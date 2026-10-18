package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/integration"
	"devboard/internal/store"
)

const (
	memoryScopeBatch       = 8
	memoryRecordBatch      = 100
	memoryEventBatch       = 256
	memoryComparisonBudget = 16
	memoryRefLimit         = 2048
)

// MemoryFreshness only observes. It uses GitControl's registered-repository
// checks, Git reader and authenticated GitHub metadata, never its write methods.
// There is one serial worker per controller; the checkpoint CAS also guards
// overlapping explicit reconcilers. No Git I/O runs in a database transaction.
type MemoryFreshness struct {
	Deps
	Control      *GitControl
	PollInterval time.Duration // default 30 seconds
	wg           sync.WaitGroup
}

func memoryDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func memoryRepositoryMatches(repo *domain.GitRepository, identity string) bool {
	for _, remote := range repo.Remotes {
		id, err := integration.RepositoryIdentity(remote.URL)
		if err == nil && id == identity {
			return true
		}
	}
	return false
}

func (s *MemoryFreshness) resolve(ctx context.Context, scope domain.MemoryScope) (*gitCtx, error) {
	project := scope.ProjectID
	if project == "" {
		// Repository-only partitions must resolve uniquely. Never guess from a path
		// or pick the first match. Above the bounded discovery ceiling, stay unknown.
		var repos []domain.GitRepository
		if err := s.Store.View(ctx, func(tx store.Tx) error {
			var err error
			repos, err = tx.Repositories().ListBounded(ctx, 65)
			return err
		}); err != nil {
			return nil, err
		}
		if len(repos) > 64 {
			return nil, domain.ErrConflict
		}
		for i := range repos {
			if memoryRepositoryMatches(&repos[i], scope.Repository) {
				if project != "" {
					return nil, domain.ErrConflict
				}
				project = repos[i].ProjectID
			}
		}
		if project == "" {
			return nil, domain.ErrNotFound
		}
	}
	t, err := s.Control.resolve(ctx, project)
	if err != nil {
		return nil, err
	}
	if scope.Repository != "" && !memoryRepositoryMatches(t.repo, scope.Repository) {
		return nil, domain.ErrConflict
	}
	return t, nil
}

type memoryObservation struct {
	revision domain.MemoryRepositoryRevision
	problem  string
	target   *gitCtx
}

func (s *MemoryFreshness) observe(ctx context.Context, scope domain.MemoryScope) memoryObservation {
	o := memoryObservation{revision: domain.MemoryRepositoryRevision{Refs: map[string]string{}, PullRequests: map[string]string{}}}
	t, err := s.resolve(ctx, scope)
	if err != nil {
		o.problem = "repository_unavailable_or_ambiguous"
		return o
	}
	o.target = t
	refs, err := s.Control.Git.ListRefs(ctx, t.root)
	if err != nil || len(refs) > memoryRefLimit {
		o.problem = "repository_refs_incomplete"
		return o
	}
	o.revision.Head, o.revision.Branch = t.repo.HeadCommit, t.repo.CurrentBranch
	for _, ref := range refs {
		o.revision.Refs[ref.Ref] = ref.Sha
	}
	if raw, _ := json.Marshal(o.revision); len(raw) > 256<<10 {
		o.problem = "repository_refs_too_large"
		o.revision.Refs = nil
		return o
	}
	// A second inspection rejects a HEAD/checkout race while refs were read.
	again, err := s.Control.Git.Inspect(ctx, t.root)
	if err != nil || again.HeadCommit != o.revision.Head || again.CurrentBranch != o.revision.Branch || again.CommonDir != t.repo.CommonDir || scope.Repository != "" && !memoryRepositoryMatches(again, scope.Repository) {
		o.problem = "repository_moved_during_observation"
		return o
	}
	confirm, err := s.Control.Git.ListRefs(ctx, t.root)
	if err != nil || len(confirm) != len(refs) {
		o.problem = "repository_moved_during_observation"
		return o
	}
	for _, ref := range confirm {
		if o.revision.Refs[ref.Ref] != ref.Sha {
			o.problem = "repository_moved_during_observation"
			return o
		}
	}
	ghCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	gh, err := s.Control.readPullRequests(ghCtx, t)
	if err == nil && gh.Available {
		o.revision.GitHubAvailable = true
		for _, pr := range gh.PullRequests {
			o.revision.PullRequests[pr.URL] = memoryDigest(pr)
		}
	}
	return o
}

func memoryCodeDependent(m domain.Memory) bool {
	p := m.Source
	return p.GitSHA != "" || p.Branch != "" || len(p.Paths) > 0 || p.PullRequest != "" || p.RunID != "" || m.Authority == domain.MemoryObservation && (m.Scope.Repository != "" || m.Scope.ProjectID != "")
}

type memoryDiff struct {
	unknown bool
	paths   []string
}

// evidence is cached and bounded per page. Rewrites, missing objects, shallow
// history and truncated diffs all return unknown, never an empty known diff.
func (s *MemoryFreshness) evidence(ctx context.Context, root, from, to string, cache map[string]memoryDiff) memoryDiff {
	key := from + ":" + to
	if d, ok := cache[key]; ok {
		return d
	}
	d := memoryDiff{unknown: true}
	if len(cache) >= memoryComparisonBudget {
		return d
	}
	cache[key] = d
	if from == "" || to == "" {
		return d
	}
	resolved, err := s.Control.Git.ResolveCommit(ctx, root, to)
	if err != nil || resolved != to {
		return d
	}
	if from == to {
		d.unknown = false
		cache[key] = d
		return d
	}
	ancestor, err := s.Control.Git.IsAncestor(ctx, root, from, to)
	if err != nil || !ancestor {
		return d
	}
	files, truncated, err := s.Control.Git.DiffFiles(ctx, root, from, to, 1000)
	if err != nil || truncated {
		return d
	}
	d.unknown = false
	for _, file := range files {
		d.paths = append(d.paths, file.Path)
		if file.OldPath != "" {
			d.paths = append(d.paths, file.OldPath)
		}
	}
	cache[key] = d
	return d
}
func memoryPathsOverlap(paths, changed []string) bool {
	if len(paths) == 0 {
		return len(changed) > 0
	}
	for _, p := range paths {
		for _, c := range changed {
			if c == p || strings.HasPrefix(c, p+"/") || strings.HasPrefix(p, c+"/") {
				return true
			}
		}
	}
	return false
}

// Project event payloads once per page, rather than re-decoding a potentially
// large PR/run snapshot for each memory. All maps are bounded by the event page
// or the previous (at most 40 PR) observation.
type memoryEventChanges struct {
	runs          map[string]time.Time
	branches      map[string]time.Time
	pullRequests  map[string]time.Time
	unknownBranch time.Time
	unknownPR     time.Time
}

func memoryRememberChange(changes map[string]time.Time, key string, at time.Time) {
	if at.After(changes[key]) {
		changes[key] = at
	}
}
func memoryChanges(evs []domain.Event, revision domain.MemoryRepositoryRevision) memoryEventChanges {
	changes := memoryEventChanges{runs: map[string]time.Time{}, branches: map[string]time.Time{}, pullRequests: map[string]time.Time{}}
	for _, ev := range evs {
		switch ev.Type {
		case domain.EventGitPRObserved:
			var observed domain.GitHubState
			if json.Unmarshal(ev.Payload, &observed) != nil || !observed.Available || len(observed.PullRequests) > 40 {
				if ev.CreatedAt.After(changes.unknownPR) {
					changes.unknownPR = ev.CreatedAt
				}
				continue
			}
			signatures := map[string]string{}
			for _, pr := range observed.PullRequests {
				signatures[pr.URL] = memoryDigest(pr)
			}
			for url, signature := range revision.PullRequests {
				if signatures[url] != signature {
					memoryRememberChange(changes.pullRequests, url, ev.CreatedAt)
				}
			}
		case domain.EventGitBranchDeleted:
			var action domain.GitActionResult
			if json.Unmarshal(ev.Payload, &action) != nil || action.Local == nil {
				if ev.CreatedAt.After(changes.unknownBranch) {
					changes.unknownBranch = ev.CreatedAt
				}
			} else {
				memoryRememberChange(changes.branches, action.Local.Ref, ev.CreatedAt)
			}
		case domain.EventAgentCompleted, domain.EventAgentFailed, domain.EventAgentStopped:
			if ev.RunID != "" {
				memoryRememberChange(changes.runs, ev.RunID, ev.CreatedAt)
			}
		case domain.EventRunStateChanged:
			if ev.RunID == "" {
				continue
			}
			var payload struct {
				Run *domain.Run `json:"run"`
			}
			if json.Unmarshal(ev.Payload, &payload) != nil || payload.Run == nil || payload.Run.State.Terminal() {
				memoryRememberChange(changes.runs, ev.RunID, ev.CreatedAt)
			}
		}
	}
	return changes
}
func memoryChangedAfter(change, recorded time.Time) bool {
	return !change.IsZero() && !change.Before(recorded)
}

func (s *MemoryFreshness) affected(ctx context.Context, m domain.Memory, old *domain.MemoryFreshness, o memoryObservation, changes memoryEventChanges, gap bool, cache map[string]memoryDiff) bool {
	if m.State != domain.MemoryCurrent || m.Authority == domain.MemoryDecision || m.Authority == domain.MemoryPolicy || !memoryCodeDependent(m) {
		return false
	}
	// A controller lifecycle observation asserts only a terminal process state.
	// Code movement does not change the historical fact that this run ended.
	if m.Type == "run_lifecycle" && m.Source.Type == domain.MemorySourceRun && m.Source.GitSHA == "" {
		return gap || memoryChangedAfter(changes.runs[m.Source.RunID], m.CreatedAt)
	}
	if gap || o.problem != "" || o.target == nil {
		return true
	}
	p := m.Source
	recordedAt := m.CreatedAt
	if m.VerifiedAt != nil && m.VerifiedAt.After(recordedAt) {
		recordedAt = *m.VerifiedAt
	}
	// Ordinary edits (privacy, wording, links) cannot hide newer source events.
	// Only explicit verification can move the claim's evidence watermark.
	if p.PullRequest != "" {
		now, known := o.revision.PullRequests[p.PullRequest]
		before, wasKnown := old.Revision.PullRequests[p.PullRequest]
		// A bounded PR list's omission is not evidence of closure or freshness.
		if !o.revision.GitHubAvailable || !known || !wasKnown || now != before {
			return true
		}
	}
	if p.PullRequest != "" && (memoryChangedAfter(changes.unknownPR, recordedAt) || memoryChangedAfter(changes.pullRequests[p.PullRequest], recordedAt)) {
		return true
	}
	if p.Branch != "" && (memoryChangedAfter(changes.unknownBranch, recordedAt) || memoryChangedAfter(changes.branches[p.Branch], recordedAt)) {
		return true
	}
	if p.RunID != "" && memoryChangedAfter(changes.runs[p.RunID], recordedAt) {
		return true
	}
	to, before := o.revision.Head, old.Revision.Head
	if p.Branch != "" {
		to = o.revision.Refs[p.Branch]
		before = old.Revision.Refs[p.Branch]
	}
	if p.GitSHA == "" || to == "" {
		return true
	}
	// Compare previous observation too: a reset back onto the original source
	// commit must not erase evidence that a branch was rewritten.
	if before != "" && before != to {
		transition := s.evidence(ctx, o.target.root, before, to, cache)
		if transition.unknown || memoryPathsOverlap(p.Paths, transition.paths) {
			return true
		}
	}
	d := s.evidence(ctx, o.target.root, p.GitSHA, to, cache)
	return d.unknown || memoryPathsOverlap(p.Paths, d.paths)
}

// Reconcile processes at most 100 current records and 256 durable events in an
// exact partition. more asks the worker to continue the saved traversal soon.
// Failures retain the successful revision/cursor and conservatively invalidate
// the page; they never prevent runs or Git actions from completing.
func (s *MemoryFreshness) Reconcile(ctx context.Context, scope domain.MemoryScope) (more bool, err error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	if s.Control == nil {
		return false, domain.ErrInvalid
	}
	if scope.ProjectID != "" {
		var project *domain.Project
		err := s.Store.View(ctx, func(tx store.Tx) error {
			var err error
			project, err = tx.Projects().Get(ctx, scope.ProjectID)
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			return err
		})
		if err != nil {
			return false, err
		}
		// Work projects have no Git state or agent runs. Their facts are outside
		// repository freshness, rather than failed Git observations.
		if project != nil && !project.HasRepository() {
			return false, nil
		}
	}
	state := &domain.MemoryFreshness{Scope: scope}
	if err = s.Store.View(ctx, func(tx store.Tx) error {
		saved, e := tx.Memory().Freshness(ctx, scope)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		if e == nil {
			state = saved
		}
		return e
	}); err != nil {
		return false, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	o := s.observe(readCtx, scope)
	var evs []domain.Event
	var floor, latest int64
	project := scope.ProjectID
	if o.target != nil {
		project = o.target.project.ID
	}
	err = s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		if floor, e = tx.Events().ReplayFloor(ctx); e != nil {
			return e
		}
		if latest, e = tx.Events().LatestSeq(ctx); e != nil {
			return e
		}
		evs, e = tx.Events().ListAfterProject(ctx, project, state.EventSeq, memoryEventBatch)
		return e
	})
	if err != nil {
		return false, err
	}
	seq := max(latest, state.EventSeq, floor)
	if len(evs) == memoryEventBatch {
		seq = evs[len(evs)-1].Seq
	}
	// Pruning may remove the event at the previous high-water mark. MAX(seq)
	// of retained rows must never move the successful consumer cursor backward.
	// Transcript traffic does not restart a traversal. If committed evidence
	// moves mid-scan, finish conservatively and schedule another complete pass.
	if state.After == nil {
		state.Uncertain = false
	} else if memoryDigest(state.Pending) != memoryDigest(o.revision) || state.Problem != o.problem {
		// Finish the bounded traversal even in a continuously moving repository.
		// Its remaining facts are uncertain; then start a fresh pass. Restarting
		// on every commit could indefinitely starve the tail of a large partition.
		state.Uncertain = true
	}
	state.Pending, state.PendingSeq = o.revision, seq
	var page domain.MemoryPage
	err = s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		page, e = tx.Memory().List(ctx, scope, domain.MemoryQuery{State: domain.MemoryCurrent, Limit: memoryRecordBatch, After: state.After})
		return e
	})
	if err != nil {
		return false, err
	}
	cache := map[string]memoryDiff{}
	invalid := []domain.Memory{}
	changes := memoryChanges(evs, state.Revision)
	for _, m := range page.Records {
		if s.affected(readCtx, m, state, o, changes, state.EventSeq < floor || state.Uncertain, cache) {
			invalid = append(invalid, m)
		}
	}
	at := s.now()
	state.CheckedAt, state.Problem, state.After = at, o.problem, page.Next
	unfinished := state.Uncertain
	if page.Next == nil {
		if o.problem == "" && !unfinished {
			state.Revision = o.revision
			state.EventSeq = seq
			state.SucceededAt = &at
		}
		state.Pending = domain.MemoryRepositoryRevision{}
	}
	err = s.Store.Update(ctx, func(tx store.Tx) error {
		// Save checkpoint first so competing reconcilers cannot apply stale results.
		// All changes roll back together on a memory CAS conflict or any write error.
		if e := tx.Memory().SaveFreshness(ctx, state); e != nil {
			return e
		}
		for _, m := range invalid {
			if e := tx.Memory().Invalidate(ctx, m.Scope, m.ID, m.Version, at); e != nil {
				return e
			}
		}
		return nil
	})
	return page.Next != nil || o.problem == "" && (len(evs) == memoryEventBatch || unfinished), err
}

// Watch starts one worker. Bus messages are low-latency hints; the durable log
// and periodic committed-state reconciliation are the recovery mechanism.
func (s *MemoryFreshness) Watch(ctx context.Context, bus events.Subscriber) {
	sub := bus.Subscribe(128)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { sub.Close() }()
		interval := s.PollInterval
		if interval <= 0 {
			interval = 30 * time.Second
		}
		timer := time.NewTimer(0)
		defer timer.Stop()
		var after *domain.MemoryScope
		var retry bool
		// A single bounded priority hint accelerates the affected project's scopes.
		// Normal traversal keeps its own cursor and gets every other batch, so a
		// busy project cannot starve unrelated/external repository reconciliation.
		var priorityProject string
		var priorityAfter *domain.MemoryScope
		var priorityRetry bool
		priorityTurn := true
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.C:
				if !ok {
					if !sub.Dropped() {
						return
					}
					sub.Close()
					sub = bus.Subscribe(128)
					after = nil
					priorityProject, priorityAfter = "", nil
					timer.Reset(0)
				} else if relevantToHealth[ev.Type] || ev.Type == domain.EventGitPRObserved || ev.Type == domain.EventAgentCompleted {
					if ev.ProjectID != "" {
						if priorityProject != ev.ProjectID {
							priorityAfter = nil
							priorityRetry = false
						}
						priorityProject = ev.ProjectID
					}
					timer.Reset(0)
				}
			case <-timer.C:
				var scopes []domain.MemoryScope
				priority := priorityProject != "" && priorityTurn
				priorityTurn = !priority
				err := s.Store.View(ctx, func(tx store.Tx) error {
					var e error
					if priority {
						var identities []string
						repo, e := tx.Repositories().Get(ctx, priorityProject)
						if e != nil && !errors.Is(e, domain.ErrNotFound) {
							return e
						}
						if repo != nil {
							for _, remote := range repo.Remotes {
								if id, e := integration.RepositoryIdentity(remote.URL); e == nil && len(identities) < 32 {
									identities = append(identities, id)
								}
							}
						}
						scopes, e = tx.Memory().FreshnessProjectScopes(ctx, priorityProject, identities, priorityAfter, memoryScopeBatch)
						return e
					}
					scopes, e = tx.Memory().FreshnessScopes(ctx, after, memoryScopeBatch)
					return e
				})
				if err == nil {
					for _, scope := range scopes {
						more, e := s.Reconcile(ctx, scope)
						if priority {
							priorityRetry = priorityRetry || more
						} else {
							retry = retry || more
						}
						if e != nil && ctx.Err() == nil {
							s.log().Warn("memory reconciliation deferred", "error", e)
						}
						if ctx.Err() != nil {
							return
						}
					}
				} else if ctx.Err() == nil {
					s.log().Warn("memory scope scan deferred", "error", err)
				}
				delay := interval
				if priority {
					if len(scopes) == memoryScopeBatch {
						last := scopes[len(scopes)-1]
						priorityAfter = &last
					} else if priorityRetry {
						priorityAfter = nil
						priorityRetry = false
					} else {
						priorityProject, priorityAfter = "", nil
					}
					// Always give normal reconciliation its turn after a priority batch.
					delay = 100 * time.Millisecond
				} else if len(scopes) == memoryScopeBatch {
					last := scopes[len(scopes)-1]
					after = &last
					delay = 100 * time.Millisecond
				} else {
					after = nil
					if retry || priorityProject != "" {
						delay = 100 * time.Millisecond
					}
					retry = false
				}
				timer.Reset(delay)
			}
		}
	}()
}
func (s *MemoryFreshness) Wait() { s.wg.Wait() }
