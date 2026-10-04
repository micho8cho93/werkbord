package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// GitHealth works out what Git state needs a person's attention and remembers
// what it found.
//
// It is built to be cheap and to stay out of the way:
//
//   - It uses Git metadata and Dev Board's own records, and nothing else. It never
//     calls a model, never touches the network, and never changes the repository
//     (the one thing it writes into Git is the unreferenced objects of an
//     in-memory merge, as the merge check does).
//   - It is not polled. It recalculates when something that can change the answer
//     happens (a run changes state, a task moves, a worktree appears or goes, any
//     Git action finishes), after a short quiet period so a burst of events costs
//     one calculation; when a person asks; and when a screen asks for a report
//     whose last calculation is a few minutes old. Changes made outside Dev Board,
//     in a terminal, are picked up by the last two.
//   - It never acts. A finding recommends a next step; the person takes it, through
//     the same guarded operations as everywhere else.
//
// The rules themselves are pure (domain.EvaluateHealth). This file is the part
// that gathers their facts and keeps the findings' life.
type GitHealth struct {
	Deps
	Control *GitControl
	// Thresholds are the numbers the rules use. The zero value means the defaults.
	Thresholds domain.HealthThresholds
	// Debounce is how long the watcher waits after the last relevant event before
	// recalculating. Defaults to 3 seconds.
	Debounce time.Duration
	// MaxAge is how old a stored calculation may be before a screen asking for it
	// causes a new one. Defaults to 2 minutes.
	MaxAge time.Duration

	mu      sync.Mutex
	dirty   map[string]bool
	locks   map[string]chan struct{}
	timers  map[string]*time.Timer
	wg      sync.WaitGroup // the watcher's goroutines
	running int            // debounced recalculations in progress
	idle    *sync.Cond     // signalled when running drops to zero; guarded by mu
	stopped bool
}

const (
	defaultHealthDebounce = 3 * time.Second
	defaultHealthMaxAge   = 2 * time.Minute
	// resolvedKeptFor is how long a resolved finding is remembered.
	resolvedKeptFor = 14 * 24 * time.Hour
	// recentlyResolved is how long a resolved finding stays on the report.
	recentlyResolved     = 24 * time.Hour
	maxResolvedShown     = 8
	maxOnTargetChecks    = 20
	maxOverlapFileSets   = 2000
	maxOverlapPairChecks = 45
)

func (h *GitHealth) thresholds() domain.HealthThresholds {
	if h.Thresholds == (domain.HealthThresholds{}) {
		return domain.DefaultHealthThresholds()
	}
	return h.Thresholds
}

func (h *GitHealth) debounce() time.Duration {
	if h.Debounce > 0 {
		return h.Debounce
	}
	return defaultHealthDebounce
}

func (h *GitHealth) maxAge() time.Duration {
	if h.MaxAge > 0 {
		return h.MaxAge
	}
	return defaultHealthMaxAge
}

// ---- reading ----

// Report is a project's health. It recalculates if the project has never been
// checked, if something that can change the answer happened since, or if the last
// calculation is older than MaxAge; otherwise it reads what was stored. Reading is
// a database read: this is what a screen can call freely.
func (h *GitHealth) Report(ctx context.Context, projectID string) (*domain.HealthReport, error) {
	var check *domain.HealthCheck
	err := h.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		c, err := tx.Health().GetCheck(ctx, projectID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		check = c
		return err
	})
	if err != nil {
		return nil, err
	}
	if check == nil || h.isDirty(projectID) || h.now().Sub(check.CheckedAt) > h.maxAge() {
		return h.Refresh(ctx, projectID)
	}
	return h.stored(ctx, projectID)
}

// stored reads the report from what was last written.
func (h *GitHealth) stored(ctx context.Context, projectID string) (*domain.HealthReport, error) {
	r := &domain.HealthReport{ProjectID: projectID, Findings: []domain.HealthFinding{}, Dismissed: []domain.HealthFinding{}, Resolved: []domain.HealthFinding{}}
	now := h.now()
	err := h.Store.View(ctx, func(tx store.Tx) error {
		all, err := tx.Health().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		for _, f := range all {
			switch f.State {
			case domain.HealthOpen:
				r.Findings = append(r.Findings, f)
			case domain.HealthDismissed:
				r.Dismissed = append(r.Dismissed, f)
			case domain.HealthResolved:
				if f.ResolvedAt != nil && now.Sub(*f.ResolvedAt) <= recentlyResolved && len(r.Resolved) < maxResolvedShown {
					r.Resolved = append(r.Resolved, f)
				}
			}
		}
		if c, err := tx.Health().GetCheck(ctx, projectID); err == nil {
			r.Check = c
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	domain.SortHealth(r.Findings)
	domain.SortHealth(r.Dismissed)
	sort.SliceStable(r.Resolved, func(i, j int) bool { return r.Resolved[i].ResolvedAt.After(*r.Resolved[j].ResolvedAt) })
	r.Summary = domain.SummarizeHealth(append(append([]domain.HealthFinding{}, r.Findings...), r.Dismissed...))
	return r, nil
}

// ---- recalculating ----

func (h *GitHealth) lock(ctx context.Context, projectID string) (func(), error) {
	h.mu.Lock()
	if h.locks == nil {
		h.locks = map[string]chan struct{}{}
	}
	l, ok := h.locks[projectID]
	if !ok {
		l = make(chan struct{}, 1)
		h.locks[projectID] = l
	}
	h.mu.Unlock()
	select {
	case l <- struct{}{}:
		return func() { <-l }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *GitHealth) isDirty(projectID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dirty[projectID]
}

func (h *GitHealth) setDirty(projectID string, v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dirty == nil {
		h.dirty = map[string]bool{}
	}
	if v {
		h.dirty[projectID] = true
	} else {
		delete(h.dirty, projectID)
	}
}

// Refresh recalculates a project's health now, whatever was stored, and stores
// the result. It reads Git and Dev Board's records, runs the rules and keeps the
// findings' lives (see domain.ReconcileHealth). It is the explicit refresh.
func (h *GitHealth) Refresh(ctx context.Context, projectID string) (*domain.HealthReport, error) {
	unlock, err := h.lock(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// Cleared before looking, so an event that arrives while Git is being read marks
	// it dirty again and is not lost.
	h.setDirty(projectID, false)
	started := time.Now()

	var current []domain.HealthFinding
	readErr := ""
	t, err := h.Control.resolve(ctx, projectID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, err
	case err != nil:
		readErr = err.Error()
	default:
		a, aerr := h.Control.associations(ctx, projectID)
		if aerr != nil {
			return nil, aerr
		}
		o, oerr := h.Control.overview(ctx, t, a)
		if oerr != nil {
			readErr = oerr.Error()
			break
		}
		current = domain.EvaluateHealth(h.input(ctx, t, a, o))
	}
	if readErr != "" {
		// The repository cannot be read. That is one finding; everything else is
		// unknown, not resolved, so it is left exactly as it was.
		current = domain.EvaluateHealth(domain.HealthInput{Now: h.now(), ProjectID: projectID, ReadError: readErr, Thresholds: h.thresholds()})
	}

	if err := h.persist(ctx, projectID, current, readErr, time.Since(started)); err != nil {
		return nil, err
	}
	return h.stored(ctx, projectID)
}

// persist stores a recalculation: the findings in their new lives and the check
// itself, in one transaction, and announces the change if there was one.
func (h *GitHealth) persist(ctx context.Context, projectID string, current []domain.HealthFinding, readErr string, took time.Duration) error {
	now := h.now()
	return h.update(ctx, func(tx store.Tx, em *emitter) error {
		existing, err := tx.Health().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		if readErr != "" {
			var only []domain.HealthFinding
			for _, f := range existing {
				if f.Type == domain.FindRepositoryUnread {
					only = append(only, f)
				}
			}
			existing = only
		}
		rec := domain.ReconcileHealth(now, existing, current)
		for i := range rec.Write {
			if err := tx.Health().Upsert(ctx, &rec.Write[i]); err != nil {
				return err
			}
		}
		if err := tx.Health().SetCheck(ctx, domain.HealthCheck{ProjectID: projectID, CheckedAt: now, DurationMS: took.Milliseconds(), Error: readErr}); err != nil {
			return err
		}
		if _, err := tx.Health().DeleteResolvedBefore(ctx, now.Add(-resolvedKeptFor)); err != nil {
			return err
		}
		if !rec.Any() {
			return nil
		}
		return h.announce(ctx, tx, em, projectID, rec)
	})
}

// announce publishes that a project's open findings changed, so the Control Center
// and any open Git screen can refresh. Nothing is published for a recalculation
// that found the same thing.
func (h *GitHealth) announce(ctx context.Context, tx store.Tx, em *emitter, projectID string, rec domain.HealthReconciliation) error {
	all, err := tx.Health().ListByProject(ctx, projectID)
	if err != nil {
		return err
	}
	ev := newEvent(domain.EventGitHealthChanged, domain.HealthChange{Summary: domain.SummarizeHealth(all), Opened: rec.Opened, Resolved: rec.Resolved, Changed: rec.Changed})
	ev.ProjectID = projectID
	return em.emit(ev)
}

// ---- dismissing ----

// Dismiss says the user knows about a finding. It stays hidden until it gets worse
// or goes away and comes back. Nothing is changed in the repository.
func (h *GitHealth) Dismiss(ctx context.Context, projectID, id string) (*domain.HealthReport, error) {
	return h.setDismissed(ctx, projectID, id, true)
}

// Reopen undoes a dismissal.
func (h *GitHealth) Reopen(ctx context.Context, projectID, id string) (*domain.HealthReport, error) {
	return h.setDismissed(ctx, projectID, id, false)
}

func (h *GitHealth) setDismissed(ctx context.Context, projectID, id string, dismiss bool) (*domain.HealthReport, error) {
	now := h.now()
	err := h.update(ctx, func(tx store.Tx, em *emitter) error {
		f, err := tx.Health().Get(ctx, projectID, id)
		if err != nil {
			return err
		}
		var next domain.HealthFinding
		var ok bool
		if dismiss {
			next, ok = domain.DismissHealth(*f, now)
		} else {
			next, ok = domain.ReopenHealth(*f, now)
		}
		if !ok {
			return fmt.Errorf("finding %s is %s, so it cannot be %s: %w", id, f.State, map[bool]string{true: "dismissed", false: "reopened"}[dismiss], domain.ErrConflict)
		}
		if err := tx.Health().Upsert(ctx, &next); err != nil {
			return err
		}
		rec := domain.HealthReconciliation{Changed: 1}
		return h.announce(ctx, tx, em, projectID, rec)
	})
	if err != nil {
		return nil, err
	}
	return h.stored(ctx, projectID)
}

// ---- gathering the facts ----

// input gathers what the rules look at. Everything here is a read of Git metadata
// or of Dev Board's records.
func (h *GitHealth) input(ctx context.Context, t *gitCtx, a *gitAssoc, o *domain.GitOverview) domain.HealthInput {
	in := domain.HealthInput{
		Now: h.now(), Thresholds: h.thresholds(), ProjectID: t.project.ID, Overview: o,
		Records: a.worktrees, RunsByWorktree: a.runsByWT, Tasks: a.tasks, OnTarget: map[string]domain.TargetContent{},
	}
	if h.Control.Worktrees != nil && h.Control.Worktrees.Root != "" {
		in.WorktreeRoot = canonPath(h.Control.Worktrees.Root)
	}
	in.IndexLock = h.indexLock(t)
	in.OnTarget = h.onTarget(ctx, t, o)
	in.Overlaps = h.overlaps(ctx, t, o)
	return in
}

// indexLock reports the modification time of the checkout's index.lock, if there is one.
func (h *GitHealth) indexLock(t *gitCtx) *time.Time {
	dir := filepath.Join(t.root, ".git")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = t.repo.CommonDir // a linked worktree registered as the project: the lock is not under .git
	}
	fi, err := os.Stat(filepath.Join(dir, "index.lock"))
	if err != nil {
		return nil
	}
	m := fi.ModTime().UTC()
	return &m
}

// ownedUnmerged are the Dev Board branches with commits the target lacks.
func ownedUnmerged(o *domain.GitOverview) []*domain.GitBranch {
	var out []*domain.GitBranch
	for i := range o.Branches {
		b := &o.Branches[i]
		if b.Scope == domain.ScopeLocal && !b.Target && b.DevBoard.Created && b.Unusual == "" &&
			b.VsTarget.Ahead > 0 && !b.Merged && gitrepo.IsCommitID(b.Sha) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CommitDate.Equal(out[j].CommitDate) {
			return out[i].CommitDate.After(out[j].CommitDate)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// onTarget asks Git, for each Dev Board branch with commits the target lacks,
// whether merging it would change the target at all. That is true of a branch that
// was squash- or rebase-merged: by history its commits are missing, but its
// content is there. Git's own in-memory merge answers it, and the answer is
// deterministic: the merged tree is the target's tree, or it is not.
func (h *GitHealth) onTarget(ctx context.Context, t *gitCtx, o *domain.GitOverview) map[string]domain.TargetContent {
	out := map[string]domain.TargetContent{}
	if !gitrepo.IsCommitID(o.Local.Target.Sha) {
		return out
	}
	branches := ownedUnmerged(o)
	if len(branches) > maxOnTargetChecks {
		branches = branches[:maxOnTargetChecks]
	}
	if len(branches) == 0 {
		return out
	}
	targetTree, err := h.Control.Git.TreeOf(ctx, t.root, o.Local.Target.Sha)
	if err != nil || targetTree == "" {
		return out
	}
	var mu sync.Mutex
	forEach(len(branches), gitWorkers, func(i int) {
		b := branches[i]
		sim, err := h.Control.Git.MergeSimulation(ctx, t.root, o.Local.Target.Sha, b.Sha)
		if err != nil || !sim.Supported || sim.Unrelated {
			return
		}
		mu.Lock()
		out[b.Name] = domain.TargetContent{Checked: true, Unchanged: !sim.Conflicts && sim.Tree != "" && sim.Tree == targetTree}
		mu.Unlock()
	})
	return out
}

// fileSet is the paths a branch changes: what it committed since it left the
// target, and what is uncommitted in its worktree. Untracked directories are
// listed collapsed by Git, so they are kept as prefixes.
type fileSet struct {
	committed   map[string]bool
	uncommitted map[string]bool
	dirs        []string
}

func (f *fileSet) any() bool { return len(f.committed)+len(f.uncommitted)+len(f.dirs) > 0 }

func (f *fileSet) inDir(p string) bool {
	for _, d := range f.dirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}

// overlapFiles lists the files two branches both change, ignoring files every
// branch tends to touch and that are regenerated anyway. It reports whether any of
// the overlap is uncommitted work.
func overlapFiles(a, b *fileSet) (files []string, uncommitted bool) {
	seen := map[string]bool{}
	add := func(p string, unc bool) {
		if noisyPath(p) {
			return
		}
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
		uncommitted = uncommitted || unc
	}
	paths := func(f *fileSet) map[string]bool {
		m := map[string]bool{}
		for p := range f.committed {
			m[p] = false
		}
		for p := range f.uncommitted {
			m[p] = true
		}
		return m
	}
	pa, pb := paths(a), paths(b)
	for p, ua := range pa {
		if ub, ok := pb[p]; ok {
			add(p, ua || ub)
		} else if b.inDir(p) {
			add(p, true)
		}
	}
	for p := range pb {
		if _, ok := pa[p]; !ok && a.inDir(p) {
			add(p, true)
		}
	}
	for _, da := range a.dirs {
		for _, db := range b.dirs {
			if strings.HasPrefix(da, db) || strings.HasPrefix(db, da) {
				add(strings.TrimSuffix(longer(da, db), "/")+"/", true)
			}
		}
	}
	sort.Strings(files)
	return files, uncommitted
}

func longer(a, b string) string {
	if len(a) >= len(b) {
		return a
	}
	return b
}

// noisyPath: lock files and generated output are changed by many branches and
// resolve mechanically; counting them would make nearly every pair of branches
// look like a collision.
func noisyPath(p string) bool {
	base := filepath.Base(p)
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock", "poetry.lock", "Gemfile.lock", "composer.lock", "uv.lock", "bun.lockb":
		return true
	}
	for _, seg := range []string{"node_modules/", "dist/", "build/", "vendor/", "__pycache__/", ".next/"} {
		if strings.HasPrefix(p, seg) || strings.Contains(p, "/"+seg) {
			return true
		}
	}
	return strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".map")
}

// fileSetOf reads what a branch changes. Failures leave the set empty: no overlap is
// claimed from what could not be read.
func (h *GitHealth) fileSetOf(ctx context.Context, t *gitCtx, o *domain.GitOverview, b *domain.GitBranch) *fileSet {
	fs := &fileSet{committed: map[string]bool{}, uncommitted: map[string]bool{}}
	if b.VsTarget.Ahead > 0 && gitrepo.IsCommitID(o.Local.Target.Sha) {
		if mb, err := h.Control.Git.MergeBase(ctx, t.root, o.Local.Target.Sha, b.Sha); err == nil && mb != "" {
			if files, truncated, err := h.Control.Git.DiffFiles(ctx, t.root, mb, b.Sha, maxOverlapFileSets); err == nil && !truncated {
				for _, f := range files {
					fs.committed[f.Path] = true
					if f.OldPath != "" {
						fs.committed[f.OldPath] = true
					}
				}
			}
		}
	}
	if w := b.Worktree; w != nil && w.Owned && !w.Missing && w.Dirty != nil && w.Dirty.Dirty() {
		if tree, err := h.Control.Git.Status(ctx, w.Path); err == nil {
			for _, list := range [][]domain.GitFileChange{tree.Staged, tree.Unstaged, tree.Untracked} {
				for _, f := range list {
					if strings.HasSuffix(f.Path, "/") {
						fs.dirs = append(fs.dirs, f.Path)
						continue
					}
					fs.uncommitted[f.Path] = true
					if f.OldPath != "" {
						fs.uncommitted[f.OldPath] = true
					}
				}
			}
		}
	}
	return fs
}

// overlaps finds pairs of in-flight Dev Board branches that change some of the
// same files. It is bounded: only the most recently active branches are compared,
// pairs are only examined when their file lists intersect, and Git's in-memory merge
// is run only for those. A pair where one branch contains the other is skipped:
// the shared files are inherited, not contested.
func (h *GitHealth) overlaps(ctx context.Context, t *gitCtx, o *domain.GitOverview) []domain.BranchOverlap {
	var cands []*domain.GitBranch
	for i := range o.Branches {
		b := &o.Branches[i]
		if b.Scope != domain.ScopeLocal || b.Target || !b.DevBoard.Created || b.Unusual != "" || !gitrepo.IsCommitID(b.Sha) {
			continue
		}
		inFlight := (b.VsTarget.Ahead > 0 && !b.Merged) || (b.Worktree != nil && b.Worktree.Dirty != nil && b.Worktree.Dirty.Dirty())
		if inFlight {
			cands = append(cands, b)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].CommitDate.Equal(cands[j].CommitDate) {
			return cands[i].CommitDate.After(cands[j].CommitDate)
		}
		return cands[i].Name < cands[j].Name
	})
	if limit := h.thresholds().MaxOverlapBranches; limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	if len(cands) < 2 {
		return nil
	}
	sets := make([]*fileSet, len(cands))
	forEach(len(cands), gitWorkers, func(i int) { sets[i] = h.fileSetOf(ctx, t, o, cands[i]) })

	var out []domain.BranchOverlap
	checked := 0
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if !sets[i].any() || !sets[j].any() || checked >= maxOverlapPairChecks {
				continue
			}
			files, unc := overlapFiles(sets[i], sets[j])
			if len(files) == 0 {
				continue
			}
			checked++
			a, b := cands[i], cands[j]
			if inherits, err := h.related(ctx, t, a, b); err != nil || inherits {
				continue
			}
			ov := domain.BranchOverlap{A: a.Name, B: b.Name, Files: files, Uncommitted: unc}
			if len(sets[i].committed) > 0 && len(sets[j].committed) > 0 && a.VsTarget.Ahead > 0 && b.VsTarget.Ahead > 0 {
				if sim, err := h.Control.Git.MergeSimulation(ctx, t.root, a.Sha, b.Sha); err == nil && sim.Supported && !sim.Unrelated {
					ov.Simulated, ov.Conflicts, ov.ConflictFiles = true, sim.Conflicts, sim.Files
				}
			}
			out = append(out, ov)
		}
	}
	return out
}

// related reports whether one branch contains the other's work, in which case their
// shared files are inherited, not a collision. A branch with no commits of its own is
// the target's ancestor trivially, and contains nothing: only a branch that has
// commits counts.
func (h *GitHealth) related(ctx context.Context, t *gitCtx, a, b *domain.GitBranch) (bool, error) {
	if a.VsTarget.Ahead > 0 {
		if ok, err := h.Control.Git.IsAncestor(ctx, t.root, a.Sha, b.Sha); err != nil || ok {
			return ok, err
		}
	}
	if b.VsTarget.Ahead > 0 {
		return h.Control.Git.IsAncestor(ctx, t.root, b.Sha, a.Sha)
	}
	return false, nil
}

// ---- watching ----

// relevantToHealth are the events after which what health says may have changed.
// Agent output is not among them, and neither is health's own event.
var relevantToHealth = map[domain.EventType]bool{
	domain.EventProjectRegistered: true, domain.EventProjectInspected: true,
	domain.EventTaskUpdated: true, domain.EventRunStateChanged: true,
	domain.EventWorktreeCreated: true, domain.EventWorktreeRemoving: true, domain.EventWorktreeRemoved: true,
	domain.EventGitFetched: true, domain.EventGitPushed: true, domain.EventGitMerged: true,
	domain.EventGitBranchDeleted: true, domain.EventGitTreeCleaned: true, domain.EventGitPRCreated: true,
}

// Watch recalculates health when something that can change it happens, after a
// quiet period, until ctx is cancelled. It also checks every project once at
// start. It is the only thing that recalculates unasked, and it is driven by
// events, never by a timer.
func (h *GitHealth) Watch(ctx context.Context, bus events.Subscriber) {
	sub := bus.Subscribe(512)
	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		h.checkAll(ctx)
	}()
	go func() {
		defer h.wg.Done()
		h.watch(ctx, bus, sub)
	}()
}

// Wait stops the watcher's pending recalculations and blocks until it and any
// recalculation already running have finished. Call it after cancelling Watch's
// context and before closing the store.
func (h *GitHealth) Wait() {
	h.mu.Lock()
	h.stopped = true
	for _, t := range h.timers {
		t.Stop()
	}
	h.timers = nil
	if h.idle == nil {
		h.idle = sync.NewCond(&h.mu)
	}
	for h.running > 0 {
		h.idle.Wait()
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *GitHealth) watch(ctx context.Context, bus events.Subscriber, sub *events.Subscription) {
	for {
		h.drain(ctx, sub)
		sub.Close()
		if ctx.Err() != nil {
			return
		}
		// The subscription fell behind and was dropped: events were lost, so assume anything changed.
		h.log().Warn("health watcher fell behind; rechecking every project")
		sub = bus.Subscribe(512)
		h.checkAll(ctx)
	}
}

func (h *GitHealth) drain(ctx context.Context, sub *events.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if ev.ProjectID != "" && relevantToHealth[ev.Type] {
				h.touch(ctx, ev.ProjectID)
			}
		}
	}
}

// touch notes that a project's Git state may have changed and schedules one
// recalculation after the quiet period, however many events arrive meanwhile.
func (h *GitHealth) touch(ctx context.Context, projectID string) {
	h.setDirty(projectID, true)
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil || h.stopped {
		return
	}
	if h.timers == nil {
		h.timers = map[string]*time.Timer{}
	}
	if t, ok := h.timers[projectID]; ok {
		t.Reset(h.debounce())
		return
	}
	h.timers[projectID] = time.AfterFunc(h.debounce(), func() { h.recalculate(ctx, projectID) })
}

// recalculate is what a quiet period ends in.
func (h *GitHealth) recalculate(ctx context.Context, projectID string) {
	h.mu.Lock()
	delete(h.timers, projectID)
	if h.stopped || ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	h.running++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.running--
		if h.running == 0 && h.idle != nil {
			h.idle.Broadcast()
		}
		h.mu.Unlock()
	}()
	if _, err := h.Refresh(ctx, projectID); err != nil && ctx.Err() == nil {
		h.log().Debug("health recalculation failed", "project", projectID, "err", err)
	}
}

// checkAll works out every project's health, one after another.
func (h *GitHealth) checkAll(ctx context.Context) {
	var ids []string
	if err := h.Store.View(ctx, func(tx store.Tx) error {
		ps, err := tx.Projects().List(ctx)
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
		return err
	}); err != nil {
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if _, err := h.Refresh(ctx, id); err != nil && ctx.Err() == nil {
			h.log().Debug("health check failed", "project", id, "err", err)
		}
	}
}
