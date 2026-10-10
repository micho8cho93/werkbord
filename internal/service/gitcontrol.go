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
	"devboard/internal/github"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// GitControl is the Git Control Center: what a person orchestrating many coding
// agents needs to see of the Git state those agents produced, and the few
// actions they may take on it.
//
// It owns two kinds of work, and keeps them apart.
//
// READING looks at a project's real repository through the git executable, joins
// what it finds with Werkbord's records (which branch belongs to which task and
// run, which worktrees Werkbord made) and returns it in shapes made for a phone.
// Overview reading never changes anything or touches the network. Distributed
// review comparisons refresh tracking refs first; GitHub reads, which are separate calls so that GitHub being slow or signed out
// cannot slow down or break the local overview.
//
// ACTING (push, merge, delete, clean, open a pull request, fetch) is where
// safety lives. Every action is the same four steps, in this order, under one
// per-project lock so two taps cannot interleave:
//
//  1. look again, fresh, at the repository: nothing from an earlier screen is trusted;
//  2. compare it with what the user was looking at (the commit IDs they send) and
//     refuse if anything moved;
//  3. check every condition the action depends on, and refuse with the reasons if
//     any fails: refusing is always preferred to being clever;
//  4. do exactly the one guarded operation, and report what happened, locally and
//     on the remote separately, having asked the remote when it matters.
//
// The agent runner never reaches any of this: it has no GitControl and its Git
// interface cannot merge, so a run finishing can never merge anything.
type GitControl struct {
	Deps
	Git gitrepo.Control
	// GitHub is optional. With nil (or gh missing, or signed out) everything local still works.
	GitHub github.Client
	// Worktrees is the record keeper for the worktrees Werkbord owns; cleaning one goes through it.
	Worktrees *Worktrees

	mu    sync.Mutex
	locks map[string]chan struct{}
}

// lock serialises the actions on one project. It is held for a whole action,
// including its looking again, so what was checked is still true when it is done.
// A caller that gives up while queued returns without having done anything.
func (s *GitControl) lock(ctx context.Context, projectID string) (unlock func(), err error) {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]chan struct{}{}
	}
	l, ok := s.locks[projectID]
	if !ok {
		l = make(chan struct{}, 1)
		s.locks[projectID] = l
	}
	s.mu.Unlock()
	select {
	case l <- struct{}{}:
		return func() { <-l }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// LockExecution shares repository-operation serialization with agent setup.
func (s *GitControl) LockExecution(ctx context.Context, projectID string) (func(), error) {
	return s.lock(ctx, projectID)
}

// gitCtx is a project and the repository it was just confirmed to still be.
type gitCtx struct {
	project domain.Project
	root    string
	repo    *domain.GitRepository
}

// resolve finds the project and checks, now, that its path is still the
// repository that was registered. A directory deleted and recreated inside
// another repository would otherwise make every action here act on a repository
// the user never registered.
func (s *GitControl) resolve(ctx context.Context, projectID string) (*gitCtx, error) {
	var p *domain.Project
	var stored *domain.GitRepository
	if err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		if p, err = tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		stored, err = tx.Repositories().Get(ctx, projectID)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}); err != nil {
		return nil, err
	}
	if !p.HasRepository() {
		return nil, fmt.Errorf("%w: %q is a work project with no Git repository", domain.ErrInvalid, p.Name)
	}
	repo, err := s.Git.Inspect(ctx, p.RepoPath)
	if err != nil {
		return nil, err
	}
	if repo.RootPath != p.RepoPath {
		return nil, fmt.Errorf("project %q is registered at %s, which now belongs to the repository at %s; nothing was done: %w",
			p.Name, p.RepoPath, repo.RootPath, domain.ErrConflict)
	}
	if stored != nil && stored.CommonDir != "" && repo.CommonDir != stored.CommonDir {
		return nil, fmt.Errorf("project %q now points at a different repository than the one registered; nothing was done: %w", p.Name, domain.ErrConflict)
	}
	return &gitCtx{project: *p, root: p.RepoPath, repo: repo}, nil
}

// ---- what Werkbord knows ----

// gitAssoc is Werkbord's side of a branch: the worktrees it made, the runs that
// used them and the tasks they belong to.
type gitAssoc struct {
	worktrees  []domain.Worktree
	byID       map[string]domain.Worktree
	byBranch   map[string][]domain.Worktree
	byPath     map[string]domain.Worktree // active records, by canonical path
	remoteRuns []domain.Run
	runsByWT   map[string][]domain.Run
	tasks      map[string]domain.Task
}

// canonPath resolves symlinks where it can, so a path Git printed and a path
// Werkbord recorded compare equal on systems where /var is /private/var.
func canonPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

const maxRunsForAssociation = 2000

func (s *GitControl) associations(ctx context.Context, projectID string) (*gitAssoc, error) {
	a := &gitAssoc{
		byID: map[string]domain.Worktree{}, byBranch: map[string][]domain.Worktree{}, byPath: map[string]domain.Worktree{},
		runsByWT: map[string][]domain.Run{}, tasks: map[string]domain.Task{},
	}
	var runs []domain.Run
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		if a.worktrees, err = tx.Worktrees().ListByProject(ctx, projectID); err != nil {
			return err
		}
		if runs, err = tx.Runs().ListByProject(ctx, projectID, maxRunsForAssociation); err != nil {
			return err
		}
		tasks, err := tx.Tasks().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			a.tasks[t.ID] = t
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, w := range a.worktrees {
		a.byID[w.ID] = w
		a.byBranch[w.Branch] = append(a.byBranch[w.Branch], w)
		if w.State == domain.WorktreeActive {
			a.byPath[canonPath(w.Path)] = w
		}
	}
	for _, r := range runs {
		if r.Remote && r.Branch != "" {
			a.remoteRuns = append(a.remoteRuns, r)
		}
		if r.WorktreeID != "" {
			a.runsByWT[r.WorktreeID] = append(a.runsByWT[r.WorktreeID], r)
		}
	}
	for id := range a.runsByWT {
		rs := a.runsByWT[id]
		sort.Slice(rs, func(i, j int) bool { return rs[i].CreatedAt.Before(rs[j].CreatedAt) })
	}
	return a, nil
}

// runsOn is every run that used a worktree of this branch, oldest first.
func (a *gitAssoc) runsOn(branch string) []domain.Run {
	var out []domain.Run
	for _, r := range a.remoteRuns {
		if r.Branch == branch {
			out = append(out, r)
		}
	}
	for _, w := range a.byBranch[branch] {
		out = append(out, a.runsByWT[w.ID]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ownership says what Werkbord knows about a local branch. "Created" needs both
// the devboard/ name and a record of a worktree made for exactly this branch in
// this project: a name proves nothing, because anyone can type it.
func (a *gitAssoc) ownership(branch string) domain.GitBranchOwnership {
	o := domain.GitBranchOwnership{Namespace: domain.InDevBoardNamespace(branch), Phase: domain.PhaseNone}
	wts := a.byBranch[branch]
	o.Created = o.Namespace && len(wts) > 0
	for _, w := range wts {
		o.WorktreeIDs = append(o.WorktreeIDs, w.ID)
	}
	runs := a.runsOn(branch)
	if len(runs) == 0 {
		return o
	}
	latest := runs[len(runs)-1]
	o.RunID, o.RunState, o.RunWaiting, o.AgentID = latest.ID, latest.State, latest.Waiting, latest.AgentID
	for _, r := range runs {
		if r.State.Active() {
			o.ActiveRun = true
		}
	}
	if task, ok := a.tasks[latest.TaskID]; ok {
		o.TaskID, o.TaskTitle, o.TaskState = task.ID, task.Title, task.State
		switch {
		case o.ActiveRun || task.State == domain.TaskDoing:
			o.Phase = domain.PhaseActive
		case task.State == domain.TaskReview:
			o.Phase = domain.PhaseReview
		case task.State == domain.TaskDone:
			o.Phase = domain.PhaseCompleted
		default:
			o.Phase = domain.PhaseIdle
		}
	}
	return o
}

// ---- the overview ----

const (
	maxBranchesDetailed = 300
	maxRemoteOnly       = 100
	maxWorktreeStatuses = 16
	recentCommitCount   = 15
	syncCommitCount     = 20
	gitWorkers          = 4
)

// forEach runs fn(0..n-1) with at most workers at a time. The Git engine bounds
// the processes anyway; this bounds the goroutines waiting on it.
func forEach(n, workers int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

type notes struct {
	mu   sync.Mutex
	list []string
}

func (n *notes) add(format string, args ...any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	for _, e := range n.list {
		if e == msg {
			return
		}
	}
	n.list = append(n.list, msg)
}

// Overview is the Git section of a project: local state, what is known of the
// remote (and how old that is), every branch with its associations, and the
// worktrees. Everything in it is local: it never touches the network.
func (s *GitControl) Overview(ctx context.Context, projectID string) (*domain.GitOverview, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.overview(ctx, t, a)
}

// overview is Overview for a project already resolved and associated, which is what
// the health check has in hand and needs the same facts from.
func (s *GitControl) overview(ctx context.Context, t *gitCtx, a *gitAssoc) (*domain.GitOverview, error) {
	projectID := t.project.ID
	now := s.now()
	var nt notes

	refs, err := s.Git.ListRefs(ctx, t.root)
	if err != nil {
		return nil, err
	}
	entries, err := s.Git.ListWorktrees(ctx, t.root)
	if err != nil {
		return nil, err
	}
	tinfo, err := s.Git.DetectTarget(ctx, t.root)
	if err != nil {
		return nil, err
	}
	tree, err := s.Git.Status(ctx, t.root)
	if err != nil {
		return nil, err
	}

	byRef := map[string]gitrepo.RefInfo{}
	for _, r := range refs {
		byRef[r.Ref] = r
	}
	head := domain.GitHead{Branch: tree.Branch, Commit: tree.Head, Detached: tree.Detached, Unborn: tree.Head == ""}
	if head.Commit != "" {
		if r, ok := byRef["refs/heads/"+head.Branch]; ok && !head.Detached {
			head.Subject = r.Subject
		}
	}

	target := domain.GitTarget{Name: tinfo.Name, Source: tinfo.Source, Sha: tinfo.Sha, LocalExists: tinfo.LocalExists}
	worktrees, wtByBranch := s.worktreeViews(ctx, t, a, entries, tree, &nt)
	if w, ok := wtByBranch[tinfo.Name]; ok && tinfo.Name != "" {
		target.CheckedOut = w.Path
	}
	if tinfo.LocalExists {
		target.Upstream = upstreamOf(byRef["refs/heads/"+tinfo.Name], byRef)
	}

	// Local branches first, with their measurements; then remote branches nothing local tracks.
	var locals, remotes []gitrepo.RefInfo
	tracked := map[string]bool{}
	for _, r := range refs {
		if r.Remote == "" {
			locals = append(locals, r)
			if r.UpstreamRef != "" {
				tracked[r.UpstreamRef] = true
			}
		} else {
			remotes = append(remotes, r)
		}
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].Date.After(locals[j].Date) })
	// Werkbord's own branches are measured first, so a cap never hides them.
	sort.SliceStable(locals, func(i, j int) bool {
		return domain.InDevBoardNamespace(locals[i].Name) && !domain.InDevBoardNamespace(locals[j].Name)
	})
	detailed := locals
	if len(detailed) > maxBranchesDetailed {
		detailed = detailed[:maxBranchesDetailed]
		nt.add("Showing the first %d of %d local branches; the rest are not measured.", maxBranchesDetailed, len(locals))
	}

	branches := make([]domain.GitBranch, len(detailed))
	forEach(len(detailed), gitWorkers, func(i int) {
		branches[i] = s.localBranch(ctx, t, a, detailed[i], byRef, tinfo, head.Branch, wtByBranch, now, &nt)
	})

	var remoteOnly []gitrepo.RefInfo
	for _, r := range remotes {
		if tracked[r.Ref] {
			continue
		}
		// The branch of the same name that a local branch would track under another remote, or none.
		remoteOnly = append(remoteOnly, r)
	}
	sort.Slice(remoteOnly, func(i, j int) bool { return remoteOnly[i].Date.After(remoteOnly[j].Date) })
	if len(remoteOnly) > maxRemoteOnly {
		nt.add("Showing the %d most recent of %d remote branches that no local branch tracks.", maxRemoteOnly, len(remoteOnly))
		remoteOnly = remoteOnly[:maxRemoteOnly]
	}
	remoteBranches := make([]domain.GitBranch, len(remoteOnly))
	forEach(len(remoteOnly), gitWorkers, func(i int) {
		remoteBranches[i] = s.remoteBranch(ctx, t, a, remoteOnly[i], tinfo, byRef, now, &nt)
	})

	sort.SliceStable(branches, func(i, j int) bool {
		ri, rj := domain.AttentionRank(&branches[i]), domain.AttentionRank(&branches[j])
		if ri != rj {
			return ri > rj
		}
		return branches[i].CommitDate.After(branches[j].CommitDate)
	})
	all := append(branches, remoteBranches...)

	local := domain.GitLocal{
		Name: t.project.Name, RootPath: t.root, Head: head, Target: target, WorkingTree: *tree,
		RecentCommits: []domain.GitCommit{},
	}
	if head.Commit != "" {
		page, err := s.Git.Commits(ctx, t.root, head.Commit, nil, 0, recentCommitCount, false)
		if err != nil {
			nt.add("Recent commits could not be read: %v", err)
		}
		local.RecentCommits = page.Items
	}

	remote := domain.GitRemoteInfo{Remotes: t.repo.Remotes, GitHub: detectGitHub(t.repo.Remotes)}
	if lf := s.Git.LastFetch(t.repo.CommonDir); !lf.IsZero() {
		remote.LastFetchedAt = &lf
	}
	if len(t.repo.Remotes) == 0 {
		nt.add("This repository has no remote, so nothing can be pushed and there are no pull requests.")
	} else if remote.LastFetchedAt == nil {
		nt.add("This repository has never fetched: what it knows about the remote may be out of date. Use Fetch to update it.")
	}
	if head.Detached {
		nt.add("HEAD is detached in the project's checkout, so it is on no branch.")
	}
	if head.Commit != "" && !head.Detached {
		remote.Sync = s.syncOf(ctx, t, head, byRef, &nt)
	}
	if tinfo.Name == "" {
		nt.add("No target branch could be determined, so branches are not compared with anything.")
	} else if !tinfo.LocalExists {
		nt.add("The target %s exists only on the remote here. Branches are compared with it, but nothing can be merged until it exists locally.", tinfo.Name)
	}

	sort.Strings(nt.list)
	return &domain.GitOverview{
		ProjectID: projectID, GeneratedAt: now, Local: local, Remote: remote,
		Branches: all, Worktrees: worktrees, Summary: summarize(all, worktrees), Notes: nt.list,
	}, nil
}

func summarize(branches []domain.GitBranch, wts []domain.GitWorktree) domain.GitSummary {
	var sum domain.GitSummary
	for _, b := range branches {
		if b.Scope != domain.ScopeLocal {
			continue
		}
		sum.Branches++
		if b.DevBoard.Created {
			sum.DevBoard++
			if b.VsTarget.Relation == domain.RelAhead && !b.DevBoard.ActiveRun {
				sum.Mergeable++
			}
			if b.VsTarget.Relation == domain.RelMerged && !b.DevBoard.ActiveRun {
				sum.Cleanup++
			}
		}
		worst := ""
		for _, at := range b.Attention {
			if at.Severity == domain.SeverityAction {
				worst = domain.SeverityAction
			} else if at.Severity == domain.SeverityWarn && worst == "" {
				worst = domain.SeverityWarn
			}
		}
		switch worst {
		case domain.SeverityAction:
			sum.NeedsYou++
		case domain.SeverityWarn:
			sum.Warnings++
		}
		if b.NotPushed > 0 && !b.Merged {
			sum.Unpushed++
		}
	}
	for _, w := range wts {
		if w.Primary {
			continue
		}
		sum.Worktrees++
		if w.Dirty != nil && w.Dirty.Dirty() {
			sum.DirtyTrees++
		}
	}
	return sum
}

// detectGitHub reads the remote URLs only: nothing is asked of GitHub.
func detectGitHub(remotes []domain.GitRemote) domain.GitHubRepo {
	if r, name, ok := githubRemote(remotes); ok && r.Host == "github.com" {
		return domain.GitHubRepo{Detected: true, Host: r.Host, Repo: r.String(), Remote: name}
	}
	return domain.GitHubRepo{}
}

// githubRemote picks the remote that names a GitHub-style repository: origin if
// it parses, else the first that does.
func githubRemote(remotes []domain.GitRemote) (github.Repo, string, bool) {
	var first *domain.GitRemote
	for i := range remotes {
		if _, ok := github.ParseRemote(remotes[i].URL); !ok {
			continue
		}
		if remotes[i].Name == "origin" {
			r, _ := github.ParseRemote(remotes[i].URL)
			return r, "origin", true
		}
		if first == nil {
			first = &remotes[i]
		}
	}
	if first != nil {
		r, _ := github.ParseRemote(first.URL)
		return r, first.Name, true
	}
	return github.Repo{}, "", false
}

// upstreamOf describes a local branch's upstream from what Git reports.
func upstreamOf(r gitrepo.RefInfo, byRef map[string]gitrepo.RefInfo) domain.GitUpstream {
	u := domain.GitUpstream{State: domain.UpstreamNone}
	if r.UpstreamRef == "" {
		return u
	}
	u.Name = strings.TrimPrefix(r.UpstreamRef, "refs/remotes/")
	if _, ok := byRef[r.UpstreamRef]; !ok || r.Track == "gone" {
		u.State = domain.UpstreamGone
		return u
	}
	for _, part := range strings.Split(r.Track, ",") {
		part = strings.TrimSpace(part)
		var n int
		switch {
		case strings.HasPrefix(part, "ahead "):
			fmt.Sscanf(part, "ahead %d", &n)
			u.Ahead = n
		case strings.HasPrefix(part, "behind "):
			fmt.Sscanf(part, "behind %d", &n)
			u.Behind = n
		}
	}
	u.State = domain.ClassifyUpstream(u.Ahead, u.Behind)
	return u
}

// localBranch measures one local branch and joins it with Werkbord's records.
// Commit counts use commit IDs, never branch names, so no name can be mistaken
// for anything else; a failure to measure one branch leaves that branch's
// relation unknown and says so, instead of failing the whole overview.
func (s *GitControl) localBranch(ctx context.Context, t *gitCtx, a *gitAssoc, r gitrepo.RefInfo, byRef map[string]gitrepo.RefInfo, tinfo gitrepo.TargetInfo, headBranch string, wts map[string]domain.GitBranchWorktree, now time.Time, nt *notes) domain.GitBranch {
	b := domain.GitBranch{
		Name: r.Name, Ref: r.Ref, Scope: domain.ScopeLocal, Sha: r.Sha, Subject: r.Subject, CommitDate: r.Date,
		Head: r.Name == headBranch, Target: r.Name == tinfo.Name && tinfo.LocalExists,
		Protected: domain.ProtectedBranch(r.Name, tinfo.Name, headBranch),
		Upstream:  upstreamOf(r, byRef),
		DevBoard:  a.ownership(r.Name),
		VsTarget:  domain.GitVsTarget{Relation: domain.RelUnknown},
		Attention: []domain.GitAttention{},
	}
	if err := branchNameProblem(r.Name); err != nil {
		b.Unusual = err.Error()
	}
	if w, ok := wts[r.Name]; ok {
		w := w
		b.Worktree = &w
	}
	switch {
	case b.Target:
		b.VsTarget.Relation = domain.RelTarget
	case tinfo.Sha != "" && gitrepo.IsCommitID(r.Sha):
		behind, ahead, err := s.Git.Divergence(ctx, t.root, tinfo.Sha, r.Sha)
		if err != nil {
			nt.add("Branch %s could not be compared with %s: %v", r.Name, tinfo.Name, err)
			break
		}
		b.VsTarget = domain.GitVsTarget{Ahead: ahead, Behind: behind, Relation: domain.ClassifyRelation(ahead, behind)}
		// Merged and "never had a commit of its own" look the same to ancestry. The reflog tells them apart.
		if b.VsTarget.Relation == domain.RelMerged {
			if never, err := s.Git.NeverAdvanced(ctx, t.root, r.Ref); err == nil && never {
				b.VsTarget.Relation = domain.RelBehind
			}
		}
		b.Merged = b.VsTarget.FullyMerged()
		b.Stale, b.StaleWhy = domain.StaleReason(b.VsTarget.Relation, behind, r.Date, now)
	}
	switch b.Upstream.State {
	case domain.UpstreamAhead, domain.UpstreamDiverged:
		b.NotPushed = b.Upstream.Ahead
	case domain.UpstreamNone, domain.UpstreamGone:
		if gitrepo.IsCommitID(r.Sha) {
			if n, err := s.Git.NotOnAnyRemote(ctx, t.root, r.Sha); err == nil {
				b.NotPushed = n
			}
		}
	}
	b.Attention = domain.BranchAttention(&b)
	return b
}

// remoteBranch describes a remote-tracking branch that no local branch tracks.
func (s *GitControl) remoteBranch(ctx context.Context, t *gitCtx, a *gitAssoc, r gitrepo.RefInfo, tinfo gitrepo.TargetInfo, byRef map[string]gitrepo.RefInfo, now time.Time, nt *notes) domain.GitBranch {
	b := domain.GitBranch{
		Name: r.Name, Ref: r.Ref, Scope: domain.ScopeRemote, Remote: r.Remote, Sha: r.Sha, Subject: r.Subject, CommitDate: r.Date,
		VsTarget: domain.GitVsTarget{Relation: domain.RelUnknown}, Attention: []domain.GitAttention{},
		DevBoard: domain.GitBranchOwnership{Phase: domain.PhaseNone, Namespace: domain.InDevBoardNamespace(strings.TrimPrefix(r.Name, r.Remote+"/"))},
		Upstream: domain.GitUpstream{State: domain.UpstreamNone},
	}
	if err := branchNameProblem(strings.TrimPrefix(r.Name, r.Remote+"/")); err != nil {
		b.Unusual = err.Error()
	}
	short := strings.TrimPrefix(r.Name, r.Remote+"/")
	b.DevBoard = a.ownership(short)
	if _, ok := byRef["refs/heads/"+short]; ok {
		b.LocalName = short
	}
	if tinfo.Sha != "" && gitrepo.IsCommitID(r.Sha) {
		if tinfo.Name != "" && r.Name == r.Remote+"/"+tinfo.Name {
			b.VsTarget.Relation = domain.RelTarget
			return b
		}
		behind, ahead, err := s.Git.Divergence(ctx, t.root, tinfo.Sha, r.Sha)
		if err != nil {
			nt.add("Remote branch %s could not be compared: %v", r.Name, err)
			return b
		}
		b.VsTarget = domain.GitVsTarget{Ahead: ahead, Behind: behind, Relation: domain.ClassifyRelation(ahead, behind)}
		b.Merged = b.VsTarget.FullyMerged()
		b.Stale, b.StaleWhy = domain.StaleReason(b.VsTarget.Relation, behind, r.Date, now)
	}
	return b
}

// branchNameProblem says why a branch name cannot be used in an action, if it cannot.
func branchNameProblem(name string) error {
	if name == "HEAD" {
		return fmt.Errorf("a branch named HEAD would be confused with HEAD itself")
	}
	if err := domain.ValidateRefName(name); err != nil {
		return err
	}
	return nil
}

// syncOf compares the current branch with its remote-tracking branch: what is
// unpushed and what is not pulled. It is as of the last fetch.
func (s *GitControl) syncOf(ctx context.Context, t *gitCtx, head domain.GitHead, byRef map[string]gitrepo.RefInfo, nt *notes) *domain.GitSync {
	r, ok := byRef["refs/heads/"+head.Branch]
	if !ok {
		return nil
	}
	sync := &domain.GitSync{Branch: head.Branch, Upstream: upstreamOf(r, byRef)}
	empty := domain.GitCommitPage{Items: []domain.GitCommit{}}
	sync.NotPushed, sync.NotPulled = empty, empty
	switch sync.Upstream.State {
	case domain.UpstreamNone, domain.UpstreamGone:
		sync.Basis = "This branch has no upstream, so its unpushed commits are the ones no remote branch has."
		if page, err := s.Git.CommitsNotOnRemotes(ctx, t.root, head.Commit, syncCommitCount); err == nil {
			sync.NotPushed = page
		} else {
			nt.add("Unpushed commits could not be listed: %v", err)
		}
	default:
		up := byRef[r.UpstreamRef]
		sync.Basis = "Compared with " + sync.Upstream.Name + " as of the last fetch."
		if page, err := s.Git.Commits(ctx, t.root, head.Commit, []string{up.Sha}, 0, syncCommitCount, true); err == nil {
			sync.NotPushed = page
		}
		if page, err := s.Git.Commits(ctx, t.root, up.Sha, []string{head.Commit}, 0, syncCommitCount, true); err == nil {
			sync.NotPulled = page
		}
	}
	return sync
}

// worktreeViews joins Git's list of worktrees with Werkbord's records and, for the main
// checkout and Werkbord's own worktrees, what is uncommitted in each. It also returns
// them by branch, for the branch list.
func (s *GitControl) worktreeViews(ctx context.Context, t *gitCtx, a *gitAssoc, entries []gitrepo.WorktreeEntry, mainTree *domain.GitWorkingTree, nt *notes) ([]domain.GitWorktree, map[string]domain.GitBranchWorktree) {
	out := make([]domain.GitWorktree, len(entries))
	for i, e := range entries {
		w := domain.GitWorktree{Path: e.Path, Head: e.Head, Branch: e.Branch, Detached: e.Detached, Primary: e.Primary, Locked: e.Locked, Prunable: e.Prunable}
		if _, err := os.Stat(e.Path); err != nil {
			w.Missing = true
		}
		if rec, ok := a.byPath[canonPath(e.Path)]; ok {
			w.Owned, w.WorktreeID = true, rec.ID
			runs := a.runsByWT[rec.ID]
			if len(runs) > 0 {
				latest := runs[len(runs)-1]
				w.RunID, w.RunState, w.TaskID = latest.ID, latest.State, latest.TaskID
				w.TaskTitle = a.tasks[latest.TaskID].Title
				for _, r := range runs {
					if r.State.Active() {
						w.ActiveRun = true
					}
				}
			}
		}
		out[i] = w
	}
	// Uncommitted work, for the checkout the project is in and for worktrees Werkbord made.
	var inspect []int
	for i, w := range out {
		if w.Missing {
			continue
		}
		if canonPath(w.Path) == canonPath(t.root) {
			c, op := mainTree.Counts, mainTree.Operation
			out[i].Dirty, out[i].Operation = &c, op
			continue
		}
		if w.Owned {
			inspect = append(inspect, i)
		}
	}
	if len(inspect) > maxWorktreeStatuses {
		nt.add("Only the first %d worktrees were checked for uncommitted work.", maxWorktreeStatuses)
		inspect = inspect[:maxWorktreeStatuses]
	}
	forEach(len(inspect), gitWorkers, func(k int) {
		i := inspect[k]
		tree, err := s.Git.Status(ctx, out[i].Path)
		if err != nil {
			nt.add("The worktree at %s could not be read: %v", out[i].Path, err)
			return
		}
		c := tree.Counts
		out[i].Dirty, out[i].Operation = &c, tree.Operation
	})

	byBranch := map[string]domain.GitBranchWorktree{}
	for _, w := range out {
		if w.Branch == "" {
			continue
		}
		byBranch[w.Branch] = domain.GitBranchWorktree{
			Path: w.Path, Primary: w.Primary, Owned: w.Owned, WorktreeID: w.WorktreeID, Missing: w.Missing,
			Locked: w.Locked, Dirty: w.Dirty, Operation: w.Operation,
		}
	}
	return out, byBranch
}

// ---- comparison and diffs ----

// Limits of a comparison.
const (
	DefaultFilePage   = 50
	MaxFilePage       = 200
	maxCompareFiles   = 5000
	uniqueCommitsShow = 50
	missingCommitShow = 20
)

// refFor names a branch by its full ref: scope is "local" (the default) or "remote".
func refFor(scope, name string) (string, error) {
	if err := branchNameProblem(name); err != nil && scope != domain.ScopeRemote {
		return "", fmt.Errorf("%w: branch: %v", domain.ErrInvalid, err)
	}
	if scope == domain.ScopeRemote {
		first, rest, ok := strings.Cut(name, "/")
		if !ok || domain.ValidateRefName(first) != nil || domain.ValidateRefName(rest) != nil {
			return "", fmt.Errorf("%w: %q is not a remote branch name", domain.ErrInvalid, name)
		}
		return "refs/remotes/" + name, nil
	}
	return "refs/heads/" + name, nil
}

// Compare says what a branch would bring to a target: commits both ways, and the
// files, with a page of them. The target defaults to the project's. scope names
// whether branch is a local or a remote-tracking branch.
func (s *GitControl) Compare(ctx context.Context, projectID, scope, branch, target string, offset, limit int) (*domain.GitComparison, error) {
	return s.CompareWithCommitLimit(ctx, projectID, scope, branch, target, offset, limit, uniqueCommitsShow)
}

// CompareWithCommitLimit allows metadata clients to request a bounded complete
// commit report, without increasing the work done by ordinary review screens.
func (s *GitControl) CompareWithCommitLimit(ctx context.Context, projectID, scope, branch, target string, offset, limit, commitLimit int) (*domain.GitComparison, error) {
	if commitLimit < 1 || commitLimit > 200 {
		return nil, fmt.Errorf("%w: commit limit must be 1–200", domain.ErrInvalid)
	}
	unlock, err := s.refreshReview(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = DefaultFilePage
	}
	limit = min(limit, MaxFilePage)

	ref, err := refFor(scope, branch)
	if err != nil {
		return nil, err
	}
	branchSha, err := s.Git.ResolveCommit(ctx, t.root, ref)
	if err != nil {
		return nil, err
	}
	if branchSha == "" {
		return nil, fmt.Errorf("branch %s: %w", branch, domain.ErrNotFound)
	}
	targetName, _, targetSha, err := s.compareTarget(ctx, t, target)
	if err != nil {
		return nil, err
	}

	cmp := &domain.GitComparison{Branch: branch, BranchSha: branchSha, Target: targetName, TargetSha: targetSha, Offset: offset, Limit: limit}
	behind, ahead, err := s.Git.Divergence(ctx, t.root, targetSha, branchSha)
	if err != nil {
		return nil, err
	}
	cmp.Ahead, cmp.Behind = ahead, behind
	cmp.Relation = domain.ClassifyRelation(ahead, behind)
	if branchSha == targetSha {
		cmp.Relation = domain.RelSame
	}
	if cmp.Unique, err = s.Git.Commits(ctx, t.root, branchSha, []string{targetSha}, 0, commitLimit, true); err != nil {
		return nil, err
	}
	if cmp.Missing, err = s.Git.Commits(ctx, t.root, targetSha, []string{branchSha}, 0, missingCommitShow, true); err != nil {
		return nil, err
	}

	from := targetSha
	cmp.Basis = "everything that differs between " + targetName + " and the branch"
	if cmp.MergeBase, err = s.Git.MergeBase(ctx, t.root, targetSha, branchSha); err != nil {
		return nil, err
	}
	if cmp.MergeBase != "" {
		from = cmp.MergeBase
		cmp.Basis = "the changes on the branch since it left " + targetName + " (what a pull request shows)"
	} else {
		cmp.Basis = "the two histories are unrelated, so this is everything that differs between " + targetName + " and the branch"
	}
	files, truncated, err := s.Git.DiffFiles(ctx, t.root, from, branchSha, maxCompareFiles)
	if err != nil {
		return nil, err
	}
	cmp.FilesTotal, cmp.Truncated = len(files), truncated
	for _, f := range files {
		cmp.Additions += f.Additions
		cmp.Deletions += f.Deletions
		if f.Binary {
			cmp.Binary++
		}
	}
	if offset < len(files) {
		cmp.Files = files[offset:min(offset+limit, len(files))]
	}
	if cmp.Files == nil {
		cmp.Files = []domain.GitDiffFile{}
	}
	return cmp, nil
}

// compareTarget resolves the comparison target: the project's, or a branch the caller names
// (local first, then remote-tracking).
func (s *GitControl) compareTarget(ctx context.Context, t *gitCtx, requested string) (name, ref, sha string, err error) {
	if requested == "" {
		ti, err := s.Git.DetectTarget(ctx, t.root)
		if err != nil {
			return "", "", "", err
		}
		if ti.Name == "" {
			return "", "", "", fmt.Errorf("%w: this repository has no target branch to compare with", domain.ErrNotFound)
		}
		a, err := s.associations(ctx, t.project.ID)
		if err != nil {
			return "", "", "", err
		}
		if len(a.remoteRuns) > 0 {
			ref := "refs/remotes/origin/" + ti.Name
			sha, err := s.Git.ResolveCommit(ctx, t.root, ref)
			if err != nil {
				return "", "", "", err
			}
			if sha != "" {
				return ti.Name, ref, sha, nil
			}
		}
		return ti.Name, ti.Ref, ti.Sha, nil
	}
	if err := domain.ValidateRefName(requested); err != nil {
		return "", "", "", fmt.Errorf("%w: target: %v", domain.ErrInvalid, err)
	}
	for _, r := range []string{"refs/heads/" + requested, "refs/remotes/" + requested} {
		sha, err := s.Git.ResolveCommit(ctx, t.root, r)
		if err != nil {
			return "", "", "", err
		}
		if sha != "" {
			return requested, r, sha, nil
		}
	}
	return "", "", "", fmt.Errorf("target %s: %w", requested, domain.ErrNotFound)
}

// FileDiff returns a window onto the diff of one file between two commits of
// the repository (the From and To the comparison reported), or of every file
// when paths is empty. For a renamed file pass the old path and then the new.
func (s *GitControl) FileDiff(ctx context.Context, projectID, from, to string, paths []string, offset, lines int) (*domain.GitFileDiff, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, sha := range []string{from, to} {
		if !gitrepo.IsCommitID(sha) {
			return nil, fmt.Errorf("%w: a diff is between two full commit IDs", domain.ErrInvalid)
		}
		if got, err := s.Git.ResolveCommit(ctx, t.root, sha); err != nil {
			return nil, err
		} else if got == "" {
			return nil, fmt.Errorf("commit %s: %w", sha[:12], domain.ErrNotFound)
		}
	}
	return s.Git.FileDiff(ctx, t.root, from, to, paths, gitrepo.DiffWindow{Offset: offset, Lines: lines})
}

// Commits pages through a branch's history, newest first.
func (s *GitControl) Commits(ctx context.Context, projectID, scope, branch string, skip, limit int) (*domain.GitCommitPage, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	limit = min(limit, 100)
	if skip < 0 {
		skip = 0
	}
	var rev string
	if branch == "" {
		if rev = t.repo.HeadCommit; rev == "" {
			return &domain.GitCommitPage{Items: []domain.GitCommit{}}, nil
		}
	} else {
		ref, err := refFor(scope, branch)
		if err != nil {
			return nil, err
		}
		if rev, err = s.Git.ResolveCommit(ctx, t.root, ref); err != nil {
			return nil, err
		} else if rev == "" {
			return nil, fmt.Errorf("branch %s: %w", branch, domain.ErrNotFound)
		}
	}
	page, err := s.Git.Commits(ctx, t.root, rev, nil, skip, limit, false)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// ---- working changes ----

// WorkingChanges is the uncommitted state of one checkout of a project.
type WorkingChanges struct {
	Path       string                `json:"path"`
	Primary    bool                  `json:"primary"`
	Owned      bool                  `json:"owned"`
	WorktreeID string                `json:"worktreeId,omitempty"`
	Tree       domain.GitWorkingTree `json:"tree"`
	TaskID     string                `json:"taskId,omitempty"`
	TaskTitle  string                `json:"taskTitle,omitempty"`
}

// checkoutDir finds the directory of a checkout of the project: the project's own
// (worktreeID empty) or one of the worktrees Werkbord recorded for it. A
// directory is only ever taken from a record or from the project itself, and
// only if Git agrees it is a worktree of this repository: a path never comes from
// the request.
func (s *GitControl) checkoutDir(ctx context.Context, t *gitCtx, a *gitAssoc, worktreeID string) (dir string, rec *domain.Worktree, err error) {
	if worktreeID == "" {
		return t.root, nil, nil
	}
	w, ok := a.byID[worktreeID]
	if !ok {
		return "", nil, fmt.Errorf("worktree %s: %w", worktreeID, domain.ErrNotFound)
	}
	if w.State != domain.WorktreeActive {
		return "", nil, fmt.Errorf("worktree %s was removed: %w", worktreeID, domain.ErrNotFound)
	}
	entries, err := s.Git.ListWorktrees(ctx, t.root)
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if canonPath(e.Path) == canonPath(w.Path) {
			if _, err := os.Stat(e.Path); err != nil {
				return "", nil, fmt.Errorf("worktree %s: its directory is gone: %w", worktreeID, domain.ErrNotFound)
			}
			return e.Path, &w, nil
		}
	}
	return "", nil, fmt.Errorf("worktree %s is not a worktree of this repository: %w", worktreeID, domain.ErrNotFound)
}

// WorkingTree reports what is staged, modified and untracked in a checkout.
func (s *GitControl) WorkingTree(ctx context.Context, projectID, worktreeID string) (*WorkingChanges, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	dir, rec, err := s.checkoutDir(ctx, t, a, worktreeID)
	if err != nil {
		return nil, err
	}
	tree, err := s.Git.Status(ctx, dir)
	if err != nil {
		return nil, err
	}
	out := &WorkingChanges{Path: dir, Primary: rec == nil, Tree: *tree}
	if rec != nil {
		out.Owned, out.WorktreeID = true, rec.ID
		if runs := a.runsByWT[rec.ID]; len(runs) > 0 {
			latest := runs[len(runs)-1]
			out.TaskID, out.TaskTitle = latest.TaskID, a.tasks[latest.TaskID].Title
		}
	}
	return out, nil
}

// WorkingDiff shows one uncommitted file of a checkout. kind is "staged",
// "unstaged" or "untracked"; for a renamed file pass the old path first.
func (s *GitControl) WorkingDiff(ctx context.Context, projectID, worktreeID string, paths []string, kind string, offset, lines int) (*domain.GitFileDiff, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	dir, _, err := s.checkoutDir(ctx, t, a, worktreeID)
	if err != nil {
		return nil, err
	}
	return s.Git.WorkingDiff(ctx, dir, paths, gitrepo.WorkingKind(kind), gitrepo.DiffWindow{Offset: offset, Lines: lines})
}

// ---- GitHub ----

// PullRequests asks GitHub, through the user's own GitHub CLI, for the project's pull
// requests. It is separate from everything local: whatever happens here, the
// answer is a state (available or not, and why), never an error that hides the
// local picture, and nothing is cached or stored.
func (s *GitControl) PullRequests(ctx context.Context, projectID string) (*domain.GitHubState, error) {
	t, err := s.resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st := &domain.GitHubState{PullRequests: []domain.GitHubPR{}, FetchedAt: s.now()}
	repo, _, err := s.githubRepo(ctx, t, st)
	if err != nil {
		return st, nil
	}
	prs, err := s.GitHub.PullRequests(ctx, repo, 40)
	if err != nil {
		ghFailure(st, err)
		return st, nil
	}
	a, err := s.associations(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range prs {
		if !prs[i].CrossRepo {
			own := a.ownership(prs[i].HeadBranch)
			prs[i].TaskID, prs[i].TaskTitle, prs[i].RunID = own.TaskID, own.TaskTitle, own.RunID
		}
	}
	st.Available, st.PullRequests = true, prs
	return st, nil
}

// githubRepo finds the project's GitHub repository, or fills st with why there is none.
func (s *GitControl) githubRepo(ctx context.Context, t *gitCtx, st *domain.GitHubState) (github.Repo, string, error) {
	fail := func(reason, msg string) (github.Repo, string, error) {
		st.Reason, st.Message = reason, msg
		return github.Repo{}, "", errors.New(msg)
	}
	if len(t.repo.Remotes) == 0 {
		return fail(domain.GHNoRemote, "this repository has no remote")
	}
	repo, remote, ok := githubRemote(t.repo.Remotes)
	if !ok {
		return fail(domain.GHNotGitHub, "no remote of this repository looks like a GitHub repository")
	}
	st.Host, st.Repo = repo.Host, repo.String()
	if s.GitHub == nil {
		return fail(domain.GHMissing, "GitHub integration is not enabled")
	}
	if repo.Host != "github.com" && !s.GitHub.HostKnown(ctx, repo.Host) {
		return fail(domain.GHNotGitHub, repo.Host+" is not a host the GitHub CLI is signed in to")
	}
	return repo, remote, nil
}

func ghFailure(st *domain.GitHubState, err error) {
	var ge *github.Error
	if errors.As(err, &ge) {
		st.Reason, st.Message = ge.Reason, ge.Message
		return
	}
	st.Reason, st.Message = domain.GHError, err.Error()
}
