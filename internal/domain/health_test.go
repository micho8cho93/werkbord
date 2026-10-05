package domain

import (
	"strings"
	"testing"
	"time"
)

// These tests build a repository's state by hand and check what the rules say
// about it. Every rule has cases where it must fire and cases where it must stay
// quiet: a health view that cries wolf is one people learn to ignore, so the
// false-positive cases matter as much as the rest.

const (
	hRepo   = "/work/app"
	hWTRoot = "/data/worktrees"
)

var hNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// hb builds a HealthInput: a repository on main with a remote, to which agents'
// branches are added. Each agent has a branch, a worktree record, a worktree, a
// task and a run, all agreeing until a test makes them disagree.
type hb struct {
	o        *GitOverview
	root     string
	extra    []*GitBranch
	agents   []*hAgent
	extraWTs []GitWorktree
	records  []Worktree
	overlaps []BranchOverlap
	onTarget map[string]TargetContent
	lock     *time.Time
	th       HealthThresholds
	seq      int
}

func newHB() *hb {
	fetched := hNow.Add(-time.Hour)
	o := &GitOverview{
		ProjectID: "p1",
		Local: GitLocal{
			Name: "app", RootPath: hRepo,
			Head:        GitHead{Branch: "main", Commit: "aaaaaaaaaaaaaaaa"},
			Target:      GitTarget{Name: "main", Source: "origin/HEAD", Sha: "aaaaaaaaaaaaaaaa", LocalExists: true, CheckedOut: hRepo, Upstream: GitUpstream{Name: "origin/main", State: UpstreamInSync}},
			WorkingTree: GitWorkingTree{Branch: "main", Clean: true},
		},
		Remote:    GitRemoteInfo{Remotes: []GitRemote{{Name: "origin", URL: "https://example.com/app.git"}}, LastFetchedAt: &fetched},
		Worktrees: []GitWorktree{{Path: hRepo, Branch: "main", Primary: true, Dirty: &GitChangeCounts{}}},
	}
	return &hb{o: o, root: hWTRoot, onTarget: map[string]TargetContent{}, th: DefaultHealthThresholds()}
}

func (h *hb) primary() *GitWorktree { return &h.o.Worktrees[0] }

// target returns the target branch's entry, adding it on first use.
func (h *hb) targetBranch() *GitBranch {
	for _, b := range h.extra {
		if b.Name == "main" {
			return b
		}
	}
	b := &GitBranch{
		Name: "main", Ref: "refs/heads/main", Scope: ScopeLocal, Sha: "aaaaaaaaaaaaaaaa", Head: true, Target: true, Protected: true,
		Upstream: GitUpstream{Name: "origin/main", State: UpstreamInSync}, VsTarget: GitVsTarget{Relation: RelTarget},
		DevBoard: GitBranchOwnership{Phase: PhaseNone}, Attention: []GitAttention{},
	}
	h.extra = append(h.extra, b)
	return b
}

// hAgent is an agent's branch with everything Werkbord records about it.
type hAgent struct {
	b    *GitBranch
	w    *GitWorktree
	rec  *Worktree
	run  *Run
	task *Task
	// noBranch: the repository has no such branch. notInGit: Git does not list its worktree.
	noBranch, noGit bool
}

func (a *hAgent) withoutBranch() *hAgent { a.noBranch = true; return a }
func (a *hAgent) notInGit() *hAgent      { a.noGit = true; return a }

// agent adds a finished agent branch: two commits ahead of the target, pushed and in
// sync, its run completed an hour ago, its task in Review, its worktree clean.
func (h *hb) agent(name string) *hAgent {
	h.seq++
	id := string(rune('a' + h.seq))
	wtID, taskID, runID := "wt_"+id, "tk_"+id, "run_"+id
	path := hWTRoot + "/" + strings.ReplaceAll(name, "/", "_")
	ended := hNow.Add(-time.Hour)
	a := &hAgent{
		b: &GitBranch{
			Name: name, Ref: "refs/heads/" + name, Scope: ScopeLocal, Sha: "bbbbbbbbbbbbbbbb", Subject: "work", CommitDate: hNow.Add(-time.Hour),
			Upstream: GitUpstream{Name: "origin/" + name, State: UpstreamInSync}, Attention: []GitAttention{},
			Worktree: &GitBranchWorktree{Path: path, Owned: true, WorktreeID: wtID, Dirty: &GitChangeCounts{}},
		},
		w:    &GitWorktree{Path: path, Head: "bbbbbbbbbbbbbbbb", Branch: name, Owned: true, WorktreeID: wtID, Dirty: &GitChangeCounts{}, TaskID: taskID, RunID: runID, RunState: RunCompleted},
		rec:  &Worktree{ID: wtID, ProjectID: "p1", Path: path, Branch: name, BaseRef: "main", State: WorktreeActive, CreatedAt: hNow.Add(-2 * time.Hour), UpdatedAt: hNow.Add(-2 * time.Hour)},
		run:  &Run{ID: runID, TaskID: taskID, ProjectID: "p1", AgentID: "claude", State: RunCompleted, WorktreeID: wtID, CreatedAt: hNow.Add(-2 * time.Hour), UpdatedAt: ended, EndedAt: &ended},
		task: &Task{ID: taskID, ProjectID: "p1", Title: "Task " + id, State: TaskReview},
	}
	a.counts(2, 0)
	h.agents = append(h.agents, a)
	return a
}

// counts sets how far the branch is ahead of and behind the target, and what that makes it.
func (a *hAgent) counts(ahead, behind int) *hAgent {
	a.b.VsTarget = GitVsTarget{Ahead: ahead, Behind: behind, Relation: ClassifyRelation(ahead, behind)}
	a.b.Merged = a.b.VsTarget.FullyMerged()
	return a
}

func (a *hAgent) dirty(c GitChangeCounts) *hAgent {
	a.w.Dirty, a.b.Worktree.Dirty = &c, &c
	return a
}

func (a *hAgent) upstream(s UpstreamState, ahead, behind int) *hAgent {
	a.b.Upstream = GitUpstream{Name: "origin/" + a.b.Name, State: s, Ahead: ahead, Behind: behind}
	if ahead > 0 {
		a.b.NotPushed = ahead
	}
	return a
}

// noUpstream makes it a branch that was never pushed.
func (a *hAgent) noUpstream(notPushed int) *hAgent {
	a.b.Upstream = GitUpstream{State: UpstreamNone}
	a.b.NotPushed = notPushed
	return a
}

func (a *hAgent) taskState(s TaskState) *hAgent { a.task.State = s; return a }

// quietFor makes the branch's last commit and its run's end this long ago.
func (a *hAgent) quietFor(d time.Duration) *hAgent {
	t := hNow.Add(-d)
	a.b.CommitDate = t
	a.run.UpdatedAt = t
	if a.run.EndedAt != nil {
		a.run.EndedAt = &t
	}
	return a
}

func (a *hAgent) runState(s RunState) *hAgent {
	a.run.State, a.w.RunState = s, s
	if s.Terminal() {
		e := a.run.UpdatedAt
		a.run.EndedAt = &e
	} else {
		a.run.EndedAt = nil
	}
	return a
}

func (a *hAgent) noWorktree() *hAgent { a.b.Worktree = nil; return a }

// neverAdvanced: it was cut from the target and never committed to.
func (a *hAgent) neverAdvanced() *hAgent {
	a.b.VsTarget.Relation = RelBehind
	return a
}

// input assembles everything.
func (h *hb) input() HealthInput {
	branches := []*GitBranch{}
	if len(h.extra) == 0 || h.extra[0].Name != "main" {
		branches = append(branches, h.targetBranch())
	}
	branches = append(branches, h.extra...)
	records := append([]Worktree{}, h.records...)
	runs := map[string][]Run{}
	tasks := map[string]Task{}
	wts := append([]GitWorktree{}, h.o.Worktrees...)
	for _, a := range h.agents {
		own := &a.b.DevBoard
		*own = GitBranchOwnership{Created: true, Namespace: true, WorktreeIDs: []string{a.rec.ID}, TaskID: a.task.ID, TaskTitle: a.task.Title,
			TaskState: a.task.State, RunID: a.run.ID, RunState: a.run.State, AgentID: a.run.AgentID, ActiveRun: a.run.State.Active()}
		switch {
		case own.ActiveRun || a.task.State == TaskDoing:
			own.Phase = PhaseActive
		case a.task.State == TaskReview:
			own.Phase = PhaseReview
		case a.task.State == TaskDone:
			own.Phase = PhaseCompleted
		default:
			own.Phase = PhaseIdle
		}
		a.w.ActiveRun = own.ActiveRun
		if !a.noBranch {
			branches = append(branches, a.b)
		}
		records = append(records, *a.rec)
		runs[a.rec.ID] = append(runs[a.rec.ID], *a.run)
		tasks[a.task.ID] = *a.task
		if !a.noGit {
			wts = append(wts, *a.w)
		}
	}
	o := *h.o
	o.Worktrees = append(wts, h.extraWTs...)
	o.Branches = nil
	for _, b := range branches {
		o.Branches = append(o.Branches, *b)
	}
	return HealthInput{
		Now: hNow, Thresholds: h.th, ProjectID: "p1", Overview: &o, WorktreeRoot: h.root,
		Records: records, RunsByWorktree: runs, Tasks: tasks, Overlaps: h.overlaps, OnTarget: h.onTarget, IndexLock: h.lock,
	}
}

func (h *hb) findings() []HealthFinding { return EvaluateHealth(h.input()) }

// ---- the table ----

type hCase struct {
	name  string
	setup func(h *hb)
	// want lists the finding types expected, each with its severity; none means silence.
	want []hWant
}

type hWant struct {
	t   HealthFindingType
	sev HealthSeverity
}

func w(t HealthFindingType, sev HealthSeverity) hWant { return hWant{t, sev} }

var hCases = []hCase{
	// ---- the quiet baseline: none of these may produce anything ----
	{name: "a repository with nothing going on", setup: func(h *hb) {}},
	{name: "an agent finished an hour ago and its work is pushed and waiting for review", setup: func(h *hb) { h.agent("devboard/feature") }},
	{name: "an agent is working", setup: func(h *hb) {
		a := h.agent("devboard/feature").runState(RunRunning).taskState(TaskDoing).dirty(GitChangeCounts{Staged: 3, Unstaged: 4, Untracked: 20}).noUpstream(5)
		a.counts(5, 40)
	}},
	{name: "an agent waiting on a question is still working", setup: func(h *hb) {
		h.agent("devboard/feature").runState(RunWaitingForUser).taskState(TaskDoing).dirty(GitChangeCounts{Unstaged: 2})
	}},

	// ---- uncommitted work ----
	{name: "an agent left modified files in its worktree", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Unstaged: 2})
	}, want: []hWant{w(FindUncommittedWork, HealthAttention)}},
	{name: "staged files left in a worktree", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Staged: 1})
	}, want: []hWant{w(FindUncommittedWork, HealthAttention)}},
	{name: "uncommitted work left for more than a day is at risk of being forgotten", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Unstaged: 2}).quietFor(30 * time.Hour)
	}, want: []hWant{w(FindUncommittedWork, HealthRisk), w(FindFinishedUnmerged, HealthAttention)}},
	{name: "a task marked Done with its work uncommitted", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Unstaged: 2}).taskState(TaskDone).counts(0, 0)
	}, want: []hWant{w(FindUncommittedWork, HealthRisk)}},
	{name: "significant untracked work", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Untracked: 5})
	}, want: []hWant{w(FindUncommittedWork, HealthAttention)}},
	{name: "a scratch file or two is not work", setup: func(h *hb) {
		h.agent("devboard/feature").dirty(GitChangeCounts{Untracked: 4})
	}},
	{name: "the user's checkout with edits is info, not a problem", setup: func(h *hb) {
		*h.primary().Dirty = GitChangeCounts{Unstaged: 3}
	}, want: []hWant{w(FindUncommittedWork, HealthInfo)}},
	{name: "the user's checkout with edits blocks a merge that is ready", setup: func(h *hb) {
		*h.primary().Dirty = GitChangeCounts{Unstaged: 3}
		h.agent("devboard/feature")
	}, want: []hWant{w(FindUncommittedWork, HealthAttention)}},
	{name: "a few untracked files in the user's checkout are nothing", setup: func(h *hb) {
		*h.primary().Dirty = GitChangeCounts{Untracked: 3}
	}},

	// ---- unsynced work ----
	{name: "commits not pushed on a finished branch", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamAhead, 2, 0)
	}, want: []hWant{w(FindUnpushedCommits, HealthAttention)}},
	{name: "commits not pushed on a Done task's branch", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamAhead, 2, 0).taskState(TaskDone)
	}, want: []hWant{w(FindUnpushedCommits, HealthRisk), w(FindTaskDoneUnmerged, HealthRisk)}},
	{name: "commits not pushed while the agent is still working are normal", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamAhead, 2, 0).runState(RunRunning).taskState(TaskDoing)
	}},
	{name: "the user's own branch with unpushed commits is housekeeping", setup: func(h *hb) {
		b := &GitBranch{Name: "my-feature", Scope: ScopeLocal, Head: true, Upstream: GitUpstream{Name: "origin/my-feature", State: UpstreamAhead, Ahead: 1},
			VsTarget: GitVsTarget{Ahead: 1, Relation: RelAhead}, DevBoard: GitBranchOwnership{Phase: PhaseNone}, Attention: []GitAttention{}}
		h.extra = append(h.extra, b)
	}, want: []hWant{w(FindUnpushedCommits, HealthInfo)}},
	{name: "another of the user's branches is not looked at", setup: func(h *hb) {
		h.extra = append(h.extra, &GitBranch{Name: "someone", Scope: ScopeLocal, Upstream: GitUpstream{State: UpstreamDiverged, Ahead: 1, Behind: 1},
			VsTarget: GitVsTarget{Ahead: 1, Relation: RelAhead}, DevBoard: GitBranchOwnership{Phase: PhaseNone}, Attention: []GitAttention{}})
	}},
	{name: "a merge made here that is not pushed", setup: func(h *hb) {
		b := h.targetBranch()
		b.Upstream = GitUpstream{Name: "origin/main", State: UpstreamAhead, Ahead: 1}
	}, want: []hWant{w(FindUnpushedCommits, HealthAttention)}},
	{name: "a branch that was never pushed", setup: func(h *hb) {
		h.agent("devboard/feature").noUpstream(2)
	}, want: []hWant{w(FindBranchNotPushed, HealthAttention)}},
	{name: "a Done task's branch that was never pushed", setup: func(h *hb) {
		h.agent("devboard/feature").noUpstream(2).taskState(TaskDone)
	}, want: []hWant{w(FindBranchNotPushed, HealthRisk), w(FindTaskDoneUnmerged, HealthRisk)}},
	{name: "without a remote nothing can be pushed, so nothing is reported", setup: func(h *hb) {
		h.o.Remote.Remotes = nil
		h.agent("devboard/feature").noUpstream(2)
	}},
	{name: "a branch with no commits of its own has nothing to push", setup: func(h *hb) {
		h.agent("devboard/feature").noUpstream(0).counts(0, 3).neverAdvanced()
	}},
	{name: "the target is behind its remote", setup: func(h *hb) {
		h.targetBranch().Upstream = GitUpstream{Name: "origin/main", State: UpstreamBehind, Behind: 3}
	}, want: []hWant{w(FindRemoteAhead, HealthAttention)}},
	{name: "someone pushed to an agent's branch", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamBehind, 0, 1)
	}, want: []hWant{w(FindRemoteAhead, HealthInfo)}},
	{name: "a branch that diverged from its remote", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamDiverged, 1, 1)
	}, want: []hWant{w(FindUpstreamDiverged, HealthRisk)}},
	{name: "the remote branch was deleted while the work is not in the target", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamGone, 0, 0)
		h.agents[0].b.NotPushed = 2
	}, want: []hWant{w(FindRemoteBranchDeleted, HealthAttention)}},
	{name: "the remote branch of a merged branch went with its pull request", setup: func(h *hb) {
		a := h.agent("devboard/feature").upstream(UpstreamGone, 0, 0).counts(0, 4).noWorktree()
		a.b.NotPushed = 0
	}, want: []hWant{w(FindMergedBranch, HealthInfo)}},
	{name: "the remote branch is gone but Git shows its content is already on the target", setup: func(h *hb) {
		h.agent("devboard/feature").upstream(UpstreamGone, 0, 0)
		h.agents[0].b.NotPushed = 2
		h.onTarget["devboard/feature"] = TargetContent{Checked: true, Unchanged: true}
	}, want: []hWant{w(FindContentOnTarget, HealthInfo)}},
	{name: "the repository never fetched", setup: func(h *hb) {
		h.o.Remote.LastFetchedAt = nil
		h.agent("devboard/feature")
	}, want: []hWant{w(FindRemoteStateStale, HealthInfo)}},
	{name: "the last fetch was weeks ago", setup: func(h *hb) {
		t := hNow.Add(-20 * 24 * time.Hour)
		h.o.Remote.LastFetchedAt = &t
		h.agent("devboard/feature")
	}, want: []hWant{w(FindRemoteStateStale, HealthInfo)}},
	{name: "an old fetch does not matter when there is no agent work", setup: func(h *hb) {
		t := hNow.Add(-20 * 24 * time.Hour)
		h.o.Remote.LastFetchedAt = &t
	}},
	{name: "a recent fetch is fine", setup: func(h *hb) {
		t := hNow.Add(-2 * 24 * time.Hour)
		h.o.Remote.LastFetchedAt = &t
		h.agent("devboard/feature")
	}},

	// ---- branch hygiene ----
	{name: "a merged branch is still present", setup: func(h *hb) {
		h.agent("devboard/feature").counts(0, 3).noWorktree()
	}, want: []hWant{w(FindMergedBranch, HealthInfo)}},
	{name: "a merged branch whose worktree is still there", setup: func(h *hb) {
		h.agent("devboard/feature").counts(0, 3)
	}, want: []hWant{w(FindMergedBranch, HealthInfo)}},
	{name: "the user's merged branch is theirs to tidy", setup: func(h *hb) {
		h.extra = append(h.extra, &GitBranch{Name: "old-feature", Scope: ScopeLocal, Merged: true, Upstream: GitUpstream{State: UpstreamNone},
			VsTarget: GitVsTarget{Behind: 3, Relation: RelMerged}, DevBoard: GitBranchOwnership{Phase: PhaseNone}, Attention: []GitAttention{}})
	}},
	{name: "a branch whose content is already on the target (squash merge)", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone)
		h.onTarget["devboard/feature"] = TargetContent{Checked: true, Unchanged: true}
	}, want: []hWant{w(FindContentOnTarget, HealthInfo)}},
	{name: "a branch nobody works on and no task waits for, quiet for days", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskBacklog).quietFor(5 * 24 * time.Hour)
	}, want: []hWant{w(FindAbandonedBranch, HealthAttention)}},
	{name: "a task stuck in Doing with no run for days", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDoing).runState(RunFailed).quietFor(6 * 24 * time.Hour)
	}, want: []hWant{w(FindAbandonedBranch, HealthAttention)}},
	{name: "an empty branch, untouched for days", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskBacklog).quietFor(5*24*time.Hour).counts(0, 2).neverAdvanced()
	}, want: []hWant{w(FindAbandonedBranch, HealthInfo)}},
	{name: "a branch quiet for only a day is not abandoned", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskBacklog).quietFor(20 * time.Hour)
	}},
	{name: "work waiting for review is not abandoned however long it has waited a few days", setup: func(h *hb) {
		h.agent("devboard/feature").quietFor(5 * 24 * time.Hour)
	}, want: []hWant{w(FindFinishedUnmerged, HealthAttention)}},
	{name: "a branch with a live session is never abandoned", setup: func(h *hb) {
		h.agent("devboard/feature").runState(RunWaitingForUser).taskState(TaskBacklog).quietFor(9 * 24 * time.Hour)
	}},
	{name: "a stale branch waiting for review", setup: func(h *hb) {
		a := h.agent("devboard/feature").quietFor(20*24*time.Hour).counts(2, 6)
		a.b.Stale, a.b.StaleWhy = true, "no commits for 20 days while the target moved on by 6"
	}, want: []hWant{w(FindStaleBranch, HealthAttention)}},
	{name: "a branch far behind the target", setup: func(h *hb) {
		h.agent("devboard/feature").counts(2, 30)
	}, want: []hWant{w(FindBranchFarBehind, HealthAttention)}},
	{name: "a branch very far behind the target", setup: func(h *hb) {
		h.agent("devboard/feature").counts(2, 120)
	}, want: []hWant{w(FindBranchFarBehind, HealthRisk)}},
	{name: "a branch a little behind is normal", setup: func(h *hb) {
		h.agent("devboard/feature").counts(2, 10)
	}},
	{name: "a merged branch is not reported as behind", setup: func(h *hb) {
		h.agent("devboard/feature").counts(0, 80).noWorktree()
	}, want: []hWant{w(FindMergedBranch, HealthInfo)}},

	// ---- orchestration ----
	{name: "a task is Done but its branch is not merged", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone)
	}, want: []hWant{w(FindTaskDoneUnmerged, HealthRisk)}},
	{name: "a Done task whose remote branch was deleted is less alarming", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone).upstream(UpstreamGone, 0, 0)
		h.agents[0].b.NotPushed = 2
	}, want: []hWant{w(FindTaskDoneUnmerged, HealthAttention)}},
	{name: "a Done task whose branch is merged is fine", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone).counts(0, 2).noWorktree()
	}, want: []hWant{w(FindMergedBranch, HealthInfo)}},
	{name: "finished work left unmerged for more than a day", setup: func(h *hb) {
		h.agent("devboard/feature").quietFor(30 * time.Hour)
	}, want: []hWant{w(FindFinishedUnmerged, HealthAttention)}},
	{name: "finished work from a few hours ago is just waiting for review", setup: func(h *hb) {
		h.agent("devboard/feature").quietFor(5 * time.Hour)
	}},
	{name: "an active task's branch is missing", setup: func(h *hb) {
		h.agent("devboard/feature").runState(RunRunning).taskState(TaskDoing).withoutBranch()
	}, want: []hWant{w(FindMissingBranch, HealthRisk)}},
	{name: "a finished task's missing branch is not an active-task problem", setup: func(h *hb) {
		h.agent("devboard/feature").withoutBranch()
	}},
	{name: "two branches change the same files", setup: func(h *hb) {
		h.agent("devboard/a")
		h.agent("devboard/b")
		h.overlaps = []BranchOverlap{{A: "devboard/a", B: "devboard/b", Files: []string{"server.go"}}}
	}, want: []hWant{w(FindBranchOverlap, HealthAttention)}},
	{name: "two branches change the same file but Git merges their commits cleanly", setup: func(h *hb) {
		h.agent("devboard/a")
		h.agent("devboard/b")
		h.overlaps = []BranchOverlap{{A: "devboard/a", B: "devboard/b", Files: []string{"server.go"}, Simulated: true}}
	}},
	{name: "a clean merge of committed work does not hide uncommitted overlap", setup: func(h *hb) {
		h.agent("devboard/a")
		h.agent("devboard/b")
		h.overlaps = []BranchOverlap{{A: "devboard/a", B: "devboard/b", Files: []string{"server.go"}, Simulated: true, Uncommitted: true}}
	}, want: []hWant{w(FindBranchOverlap, HealthAttention)}},
	{name: "Git proves two branches conflict", setup: func(h *hb) {
		h.agent("devboard/a")
		h.agent("devboard/b")
		h.overlaps = []BranchOverlap{{A: "devboard/a", B: "devboard/b", Files: []string{"server.go"}, Simulated: true, Conflicts: true, ConflictFiles: []string{"server.go"}}}
	}, want: []hWant{w(FindBranchConflict, HealthRisk)}},

	// ---- worktree hygiene ----
	{name: "a worktree in Werkbord's directory that no record owns", setup: func(h *hb) {
		h.extraWTs = append(h.extraWTs, GitWorktree{Path: hWTRoot + "/leftover", Branch: "devboard/old", Head: "cccccccc"})
	}, want: []hWant{w(FindOrphanedWorktree, HealthAttention)}},
	{name: "a worktree of the user's elsewhere is not Werkbord's business", setup: func(h *hb) {
		h.extraWTs = append(h.extraWTs, GitWorktree{Path: "/home/me/other-checkout", Branch: "experiment", Head: "cccccccc"})
	}},
	{name: "a worktree record that no run ever used", setup: func(h *hb) {
		h.records = append(h.records, Worktree{ID: "wt_x", ProjectID: "p1", Path: hWTRoot + "/unused", Branch: "devboard/unused", BaseRef: "main", State: WorktreeActive, CreatedAt: hNow.Add(-3 * time.Hour)})
		h.extraWTs = append(h.extraWTs, GitWorktree{Path: hWTRoot + "/unused", Branch: "devboard/unused", Head: "cccccccc", Owned: true, WorktreeID: "wt_x", Dirty: &GitChangeCounts{}})
		h.extra = append(h.extra, &GitBranch{Name: "devboard/unused", Scope: ScopeLocal, VsTarget: GitVsTarget{Relation: RelSame}, Merged: true, Attention: []GitAttention{}})
	}, want: []hWant{w(FindWorktreeNoRun, HealthAttention)}},
	{name: "a worktree record created a moment ago has not had time to get a run", setup: func(h *hb) {
		h.records = append(h.records, Worktree{ID: "wt_x", ProjectID: "p1", Path: hWTRoot + "/new", Branch: "devboard/new", BaseRef: "main", State: WorktreeActive, CreatedAt: hNow.Add(-2 * time.Minute)})
		h.extraWTs = append(h.extraWTs, GitWorktree{Path: hWTRoot + "/new", Branch: "devboard/new", Head: "cccccccc", Owned: true, WorktreeID: "wt_x", Dirty: &GitChangeCounts{}})
	}},
	{name: "a Done task keeps a clean worktree", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone).counts(0, 0)
	}, want: []hWant{w(FindWorktreeAfterDone, HealthInfo)}},
	{name: "a Done task with uncommitted work is reported as uncommitted, not as a spare worktree", setup: func(h *hb) {
		h.agent("devboard/feature").taskState(TaskDone).counts(0, 0).dirty(GitChangeCounts{Unstaged: 1})
	}, want: []hWant{w(FindUncommittedWork, HealthRisk)}},
	{name: "a task in Review may keep its worktree", setup: func(h *hb) {
		h.agent("devboard/feature").counts(0, 0)
	}},
	{name: "the record says a worktree is active but Git does not list it", setup: func(h *hb) {
		h.agent("devboard/feature").notInGit()
	}, want: []hWant{w(FindWorktreeMismatch, HealthAttention)}},
	{name: "someone switched an agent's worktree to another branch", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		a.w.Branch = "other"
	}, want: []hWant{w(FindWorktreeMismatch, HealthAttention)}},
	{name: "an agent's worktree is on a detached HEAD", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		a.w.Branch, a.w.Detached = "", true
	}, want: []hWant{w(FindWorktreeMismatch, HealthAttention)}},
	{name: "an agent's worktree directory is gone", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		a.w.Missing = true
		a.b.Worktree.Missing = true
	}, want: []hWant{w(FindWorktreeMismatch, HealthAttention)}},
	{name: "a removal that was begun and never finished", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		t := hNow.Add(-time.Hour)
		a.rec.RemovingSince = &t
	}, want: []hWant{w(FindWorktreeMismatch, HealthAttention)}},
	{name: "a removal begun a moment ago is in progress, not stuck", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		t := hNow.Add(-time.Minute)
		a.rec.RemovingSince = &t
	}},

	// ---- Git operations ----
	{name: "a merge was left unfinished in an agent's worktree", setup: func(h *hb) {
		a := h.agent("devboard/feature")
		a.w.Operation = "merge"
	}, want: []hWant{w(FindOperationInterrupt, HealthAttention)}},
	{name: "a rebase was left unfinished in an agent's worktree", setup: func(h *hb) {
		h.agent("devboard/feature").w.Operation = "rebase"
	}, want: []hWant{w(FindOperationInterrupt, HealthRisk)}},
	{name: "a merge is unfinished in the user's checkout", setup: func(h *hb) {
		h.primary().Operation = "merge"
	}, want: []hWant{w(FindOperationInterrupt, HealthRisk)}},
	{name: "a bisect is only information", setup: func(h *hb) {
		h.primary().Operation = "bisect"
	}, want: []hWant{w(FindOperationInterrupt, HealthInfo)}},
	{name: "an operation in a worktree where an agent is working is the agent's", setup: func(h *hb) {
		a := h.agent("devboard/feature").runState(RunRunning).taskState(TaskDoing)
		a.w.Operation = "rebase"
	}},
	{name: "unresolved conflicts in the user's checkout block everything", setup: func(h *hb) {
		h.primary().Dirty.Conflicted = 2
		h.primary().Operation = "merge"
	}, want: []hWant{w(FindUnresolvedConflict, HealthCritical)}},
	{name: "unresolved conflicts in an agent's worktree", setup: func(h *hb) {
		a := h.agent("devboard/feature").dirty(GitChangeCounts{Conflicted: 1})
		a.w.Operation = "merge"
	}, want: []hWant{w(FindUnresolvedConflict, HealthRisk)}},
	{name: "no target branch could be found", setup: func(h *hb) {
		h.o.Local.Target = GitTarget{Source: "none"}
	}, want: []hWant{w(FindAutomationBlocked, HealthAttention)}},
	{name: "the target exists only on the remote", setup: func(h *hb) {
		h.o.Local.Target.LocalExists = false
		h.o.Local.Target.CheckedOut = ""
	}, want: []hWant{w(FindAutomationBlocked, HealthAttention)}},
	{name: "the target is not checked out and a branch is ready to merge", setup: func(h *hb) {
		h.o.Local.Target.CheckedOut = ""
		h.agent("devboard/feature")
	}, want: []hWant{w(FindAutomationBlocked, HealthAttention)}},
	{name: "the target is not checked out but there is nothing to merge", setup: func(h *hb) {
		h.o.Local.Target.CheckedOut = ""
	}},
	{name: "a detached HEAD with nothing to merge is information", setup: func(h *hb) {
		h.o.Local.Head = GitHead{Commit: "aaaaaaaa", Detached: true}
	}, want: []hWant{w(FindAutomationBlocked, HealthInfo)}},
	{name: "a detached HEAD with work to merge needs attention", setup: func(h *hb) {
		h.o.Local.Head = GitHead{Commit: "aaaaaaaa", Detached: true}
		h.agent("devboard/feature")
	}, want: []hWant{w(FindAutomationBlocked, HealthAttention)}},
	{name: "an index.lock left behind", setup: func(h *hb) {
		t := hNow.Add(-2 * time.Hour)
		h.lock = &t
	}, want: []hWant{w(FindAutomationBlocked, HealthAttention)}},
	{name: "an index.lock that is seconds old is Git working", setup: func(h *hb) {
		t := hNow.Add(-5 * time.Second)
		h.lock = &t
	}},
}

func TestHealthRules(t *testing.T) {
	for _, tc := range hCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHB()
			tc.setup(h)
			got := h.findings()
			assertWant(t, got, tc.want)
			for _, f := range got {
				assertWellFormed(t, f)
			}
		})
	}
}

func assertWant(t *testing.T, got []HealthFinding, want []hWant) {
	t.Helper()
	rest := append([]hWant{}, want...)
	for _, f := range got {
		found := false
		for i, w := range rest {
			if w.t == f.Type && w.sev == f.Severity {
				rest = append(rest[:i], rest[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected finding %s (%s): %s", f.Type, f.Severity, f.Title)
		}
	}
	for _, w := range rest {
		t.Errorf("missing finding %s (%s)", w.t, w.sev)
	}
	if t.Failed() {
		for _, f := range got {
			t.Logf("got %s (%s): %s | %s", f.Type, f.Severity, f.Title, f.Explanation)
		}
	}
}

// assertWellFormed checks what must be true of every finding, whichever rule made it.
func assertWellFormed(t *testing.T, f HealthFinding) {
	t.Helper()
	rule, ok := HealthRuleFor(f.Type)
	if !ok {
		t.Errorf("%s has no documented rule", f.Type)
		return
	}
	switch {
	case f.ID == "" || !strings.HasPrefix(f.ID, "hf_"):
		t.Errorf("%s: id %q", f.Type, f.ID)
	case f.ProjectID != "p1":
		t.Errorf("%s: project %q", f.Type, f.ProjectID)
	case f.Category != rule.Category:
		t.Errorf("%s: category %s, rule says %s", f.Type, f.Category, rule.Category)
	case f.Basis != rule.Basis:
		t.Errorf("%s: basis %s, rule says %s", f.Type, f.Basis, rule.Basis)
	case !f.Severity.Valid():
		t.Errorf("%s: severity %q", f.Type, f.Severity)
	case strings.TrimSpace(f.Title) == "" || len(f.Title) > 120:
		t.Errorf("%s: title must be a concise sentence, got %q", f.Type, f.Title)
	case strings.TrimSpace(f.Explanation) == "":
		t.Errorf("%s: no explanation", f.Type)
	case len(f.Evidence) == 0:
		t.Errorf("%s: a finding without evidence is an opinion", f.Type)
	case f.Action.Kind == "" || f.Action.Label == "":
		t.Errorf("%s: every finding needs a next action", f.Type)
	case !f.Action.CanPerform && f.Action.Reason == "":
		t.Errorf("%s: an action Werkbord cannot perform must say why", f.Type)
	case f.State != HealthOpen || f.DetectedAt.IsZero():
		t.Errorf("%s: state %q detected %v", f.Type, f.State, f.DetectedAt)
	}
	for _, e := range f.Evidence {
		if e.Label == "" || strings.TrimSpace(e.Value) == "" {
			t.Errorf("%s: empty evidence %+v", f.Type, e)
		}
	}
	if (f.Action.Kind == ActCreateTask || f.Action.Kind == ActAskAgent) && (f.Action.TaskTitle == "" || f.Action.TaskDescription == "" || !f.Action.CanPerform || f.Action.Destructive) {
		t.Errorf("%s: a task action adds a prefilled card to the board and nothing else: %+v", f.Type, f.Action)
	}
	if f.Action.Destructive && f.Action.Kind != ActDeleteBranch && f.Action.Kind != ActCleanWorktree {
		t.Errorf("%s: only deleting a branch or cleaning a worktree is destructive, not %s", f.Type, f.Action.Kind)
	}
	// Werkbord never claims what it cannot prove. A heuristic finding is worded as a possibility.
	if f.Basis == BasisHeuristic {
		text := strings.ToLower(f.Title + " " + f.Explanation)
		for _, banned := range []string{"will conflict", "are in conflict", "do conflict", "definitely", "certainly"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s is heuristic but says %q: %s", f.Type, banned, text)
			}
		}
	}
}

// Every rule has at least one case that makes it fire, so adding a rule without a
// test fails here.
func TestEveryRuleHasATestThatFiresIt(t *testing.T) {
	fired := map[HealthFindingType]bool{}
	for _, tc := range hCases {
		for _, w := range tc.want {
			fired[w.t] = true
		}
	}
	// repository_unreadable has no overview to build; it has its own test below.
	fired[FindRepositoryUnread] = true
	for _, r := range HealthRules {
		if !fired[r.Type] {
			t.Errorf("rule %s is never fired by a test case", r.Type)
		}
	}
	// ... and every case type is a documented rule.
	for _, tc := range hCases {
		for _, w := range tc.want {
			if _, ok := HealthRuleFor(w.t); !ok {
				t.Errorf("case %q expects undocumented type %s", tc.name, w.t)
			}
		}
	}
}

// Every rule also has a case where it stays silent despite a nearby condition.
func TestFalsePositiveCasesExist(t *testing.T) {
	quiet := 0
	for _, tc := range hCases {
		if len(tc.want) == 0 {
			quiet++
		}
	}
	if quiet < 20 {
		t.Errorf("only %d cases expect silence; the false-positive cases are the point", quiet)
	}
}

func TestRuleTableIsConsistent(t *testing.T) {
	seen := map[HealthFindingType]bool{}
	for _, r := range HealthRules {
		if seen[r.Type] {
			t.Errorf("duplicate rule %s", r.Type)
		}
		seen[r.Type] = true
		if r.Signal == "" || r.Severity == "" || r.Action == "" || r.Category == "" {
			t.Errorf("rule %s is incompletely described: %+v", r.Type, r)
		}
		if r.Basis != BasisDeterministic && r.Basis != BasisHeuristic {
			t.Errorf("rule %s has basis %q", r.Type, r.Basis)
		}
	}
}

func TestUnreadableRepository(t *testing.T) {
	got := EvaluateHealth(HealthInput{Now: hNow, ProjectID: "p1", ReadError: "not a git repository"})
	if len(got) != 1 || got[0].Type != FindRepositoryUnread || got[0].Severity != HealthRisk {
		t.Fatalf("got %+v", got)
	}
	assertWellFormed(t, got[0])
}

// ---- wording and honesty ----

// "These branches may conflict" is acceptable; "these branches will conflict" is
// only acceptable when Git proved it.
func TestOverlapIsWordedAsAPossibilityUnlessGitProvedIt(t *testing.T) {
	h := newHB()
	h.agent("devboard/a")
	h.agent("devboard/b")
	h.overlaps = []BranchOverlap{{A: "devboard/a", B: "devboard/b", Files: []string{"x.go", "y.go"}}}
	f := h.findings()[0]
	text := strings.ToLower(f.Title + " " + f.Explanation)
	if f.Type != FindBranchOverlap || f.Basis != BasisHeuristic || !strings.Contains(text, "may conflict") || strings.Contains(f.Title, "conflict") {
		t.Fatalf("overlap finding = %+v", f)
	}

	h.overlaps[0].Simulated, h.overlaps[0].Conflicts, h.overlaps[0].ConflictFiles = true, true, []string{"x.go"}
	f = h.findings()[0]
	if f.Type != FindBranchConflict || f.Basis != BasisDeterministic || !strings.Contains(f.Explanation, "merge-tree") && !strings.Contains(f.Explanation, "in memory") {
		t.Fatalf("conflict finding = %+v", f)
	}
}

// A heuristic must not become a deterministic finding by accident: the two
// kinds are documented, and these are the heuristic ones.
func TestWhichSignalsAreHeuristic(t *testing.T) {
	want := map[HealthFindingType]bool{FindAbandonedBranch: true, FindFinishedUnmerged: true, FindBranchOverlap: true}
	for _, r := range HealthRules {
		if (r.Basis == BasisHeuristic) != want[r.Type] {
			t.Errorf("rule %s: heuristic = %v, expected %v", r.Type, r.Basis == BasisHeuristic, want[r.Type])
		}
	}
}

func TestActionsAreNeverExecutedAndDestructiveOnesAreFlagged(t *testing.T) {
	h := newHB()
	h.agent("devboard/feature").counts(0, 3).noWorktree()
	for _, f := range h.findings() {
		if f.Type == FindMergedBranch {
			a := f.Action
			if a.Kind != ActDeleteBranch || !a.Destructive || !a.CanPerform || a.Branch != "devboard/feature" {
				t.Fatalf("delete action = %+v", a)
			}
			return
		}
	}
	t.Fatal("no merged-branch finding")
}

func TestDeleteNeedsTheWorktreeCleanedFirst(t *testing.T) {
	h := newHB()
	h.agent("devboard/feature").counts(0, 3)
	f := h.findings()[0]
	if f.Action.Kind != ActCleanWorktree || !f.Action.CanPerform || f.Action.WorktreeID == "" {
		t.Fatalf("a merged branch that still has a worktree should say to clean it first: %+v", f.Action)
	}
	// ... and when the worktree has uncommitted work, Werkbord says it cannot.
	h = newHB()
	h.agent("devboard/feature").counts(0, 3).dirty(GitChangeCounts{Unstaged: 1})
	for _, f := range h.findings() {
		if f.Type == FindMergedBranch && f.Action.CanPerform {
			t.Fatalf("cleaning a dirty worktree must not be offered: %+v", f.Action)
		}
	}
}

func TestPushIsNotOfferedWhenItWouldBeRejected(t *testing.T) {
	h := newHB()
	h.agent("devboard/feature").upstream(UpstreamDiverged, 1, 1)
	f := h.findings()[0]
	if f.Action.Kind != ActSyncBranch || f.Action.CanPerform || f.Action.Reason == "" {
		t.Fatalf("a diverged branch needs a sync Werkbord cannot do: %+v", f.Action)
	}

	h = newHB()
	h.o.Remote.Remotes = append(h.o.Remote.Remotes, GitRemote{Name: "backup"})
	h.o.Remote.Remotes[0].Name = "mirror"
	h.agent("devboard/feature").noUpstream(2)
	f = h.findings()[0]
	if f.Action.CanPerform {
		t.Fatalf("several remotes and none called origin: push cannot be offered: %+v", f.Action)
	}
}

func TestMergeIsOnlyOfferedWhenItCouldRun(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *hb)
		can   bool
	}{
		{"clean checkout", func(h *hb) {}, true},
		{"target not checked out", func(h *hb) { h.o.Local.Target.CheckedOut = "" }, false},
		{"checkout with tracked changes", func(h *hb) { h.primary().Dirty.Unstaged = 1 }, false},
		{"checkout mid-merge", func(h *hb) { h.primary().Operation = "merge" }, false},
		{"untracked files only are fine", func(h *hb) { h.primary().Dirty.Untracked = 2 }, true},
		{"target only on the remote", func(h *hb) { h.o.Local.Target.LocalExists = false }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHB()
			h.agent("devboard/feature").taskState(TaskDone)
			tc.setup(h)
			var done *HealthFinding
			for _, f := range h.findings() {
				if f.Type == FindTaskDoneUnmerged {
					f := f
					done = &f
				}
			}
			if done == nil {
				t.Fatal("no task_done_unmerged finding")
			}
			isMerge := done.Action.Kind == ActMergeBranch
			if isMerge != tc.can || (isMerge && !done.Action.CanPerform) {
				t.Fatalf("action = %+v, want merge offered = %v", done.Action, tc.can)
			}
		})
	}
}

// ---- identity, summary, order ----

func TestFindingIDsAreStableAndSpecific(t *testing.T) {
	a := HealthFindingID("p1", FindStaleBranch, "devboard/x")
	if a != HealthFindingID("p1", FindStaleBranch, "devboard/x") {
		t.Fatal("the same finding must have the same id")
	}
	for _, other := range []string{
		HealthFindingID("p2", FindStaleBranch, "devboard/x"),
		HealthFindingID("p1", FindAbandonedBranch, "devboard/x"),
		HealthFindingID("p1", FindStaleBranch, "devboard/y"),
	} {
		if other == a {
			t.Fatal("different findings must have different ids")
		}
	}
	h := newHB()
	h.agent("devboard/feature").dirty(GitChangeCounts{Unstaged: 1})
	if h.findings()[0].ID != h.findings()[0].ID {
		t.Fatal("recomputing must not change ids")
	}
}

func TestSummary(t *testing.T) {
	mk := func(sev HealthSeverity, st HealthState) HealthFinding {
		return HealthFinding{Severity: sev, State: st}
	}
	cases := []struct {
		name     string
		in       []HealthFinding
		state    string
		headline string
		need     int
	}{
		{"nothing", nil, "healthy", "Healthy", 0},
		{"housekeeping only is still healthy", []HealthFinding{mk(HealthInfo, HealthOpen), mk(HealthInfo, HealthOpen)}, "healthy", "Healthy", 0},
		{"one item", []HealthFinding{mk(HealthAttention, HealthOpen)}, "attention", "1 item needs attention", 1},
		{"three items", []HealthFinding{mk(HealthAttention, HealthOpen), mk(HealthAttention, HealthOpen), mk(HealthAttention, HealthOpen)}, "attention", "3 items need attention", 3},
		{"attention and a risk", []HealthFinding{mk(HealthAttention, HealthOpen), mk(HealthAttention, HealthOpen), mk(HealthAttention, HealthOpen), mk(HealthRisk, HealthOpen)}, "risk", "1 risk · 3 items need attention", 4},
		{"two risks", []HealthFinding{mk(HealthRisk, HealthOpen), mk(HealthRisk, HealthOpen)}, "risk", "2 risks", 2},
		{"critical first", []HealthFinding{mk(HealthRisk, HealthOpen), mk(HealthCritical, HealthOpen)}, "critical", "1 critical · 1 risk", 2},
		{"dismissed and resolved do not count", []HealthFinding{mk(HealthRisk, HealthDismissed), mk(HealthCritical, HealthResolved)}, "healthy", "Healthy", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := SummarizeHealth(tc.in)
			if s.State != tc.state || s.Headline != tc.headline || s.NeedsAttention != tc.need {
				t.Fatalf("summary = %+v", s)
			}
		})
	}
	if s := SummarizeHealth([]HealthFinding{mk(HealthRisk, HealthDismissed)}); s.Dismissed != 1 {
		t.Fatalf("dismissed = %d", s.Dismissed)
	}
}

func TestScoreIsSecondaryAndBounded(t *testing.T) {
	var many []HealthFinding
	for i := 0; i < 20; i++ {
		many = append(many, HealthFinding{Severity: HealthCritical, State: HealthOpen})
	}
	if s := SummarizeHealth(many); s.Score != 0 {
		t.Fatalf("score = %d", s.Score)
	}
	if s := SummarizeHealth(nil); s.Score != 100 {
		t.Fatalf("score = %d", s.Score)
	}
	// The score never changes what is said: the headline does not mention it.
	if s := SummarizeHealth([]HealthFinding{{Severity: HealthRisk, State: HealthOpen}}); strings.Contains(s.Headline, "85") {
		t.Fatalf("headline = %q", s.Headline)
	}
}

func TestSortPutsTheWorstFirst(t *testing.T) {
	older, newer := hNow.Add(-time.Hour), hNow
	fs := []HealthFinding{
		{ID: "1", Severity: HealthInfo, Category: CatBranch, DetectedAt: older, Title: "a"},
		{ID: "2", Severity: HealthAttention, Category: CatBranch, DetectedAt: newer, Title: "b"},
		{ID: "3", Severity: HealthCritical, Category: CatWorktree, DetectedAt: newer, Title: "c"},
		{ID: "4", Severity: HealthAttention, Category: CatBranch, DetectedAt: older, Title: "d"},
		{ID: "5", Severity: HealthRisk, Category: CatOperation, DetectedAt: newer, Title: "e"},
	}
	SortHealth(fs)
	var order string
	for _, f := range fs {
		order += f.ID
	}
	if order != "35421" {
		t.Fatalf("order = %s", order)
	}
}

// A noisy quiet day: many agents, all finished and waiting, nothing wrong. This is the
// case the whole system is judged by.
func TestABusyDayWithNothingWrongIsHealthy(t *testing.T) {
	h := newHB()
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		h.agent("devboard/" + n)
	}
	h.agent("devboard/working").runState(RunRunning).taskState(TaskDoing).dirty(GitChangeCounts{Unstaged: 9, Untracked: 30}).noUpstream(4)
	s := SummarizeHealth(h.findings())
	if s.State != HealthHealthy || s.NeedsAttention != 0 {
		t.Fatalf("a busy day with nothing wrong reported %+v", s)
	}
}

// ---- lifecycle ----

func hf(id string, sev HealthSeverity) HealthFinding {
	return HealthFinding{ID: id, ProjectID: "p1", Type: FindStaleBranch, Category: CatBranch, Severity: sev, Basis: BasisDeterministic,
		Title: id, Explanation: "x", Evidence: []HealthEvidence{{Label: "a", Value: "b"}}, Action: HealthAction{Kind: ActInspect, Label: "x"}}
}

func stored(f HealthFinding, state HealthState, detected time.Time) HealthFinding {
	f.State, f.DetectedAt = state, detected
	switch state {
	case HealthResolved:
		t := detected.Add(time.Hour)
		f.ResolvedAt = &t
	case HealthDismissed:
		t := detected.Add(time.Hour)
		f.DismissedAt, f.DismissedSeverity = &t, f.Severity
	}
	return f
}

func TestAFindingKeepsItsDetectionTimeWhileItStaysTrue(t *testing.T) {
	first := hNow.Add(-72 * time.Hour)
	r := ReconcileHealth(hNow, []HealthFinding{stored(hf("a", HealthAttention), HealthOpen, first)}, []HealthFinding{hf("a", HealthAttention)})
	if len(r.Write) != 1 || !r.Write[0].DetectedAt.Equal(first) || !r.Write[0].UpdatedAt.Equal(hNow) || r.Any() {
		t.Fatalf("%+v", r)
	}
}

func TestANewFindingOpensNow(t *testing.T) {
	r := ReconcileHealth(hNow, nil, []HealthFinding{hf("a", HealthAttention)})
	if r.Opened != 1 || r.Write[0].State != HealthOpen || !r.Write[0].DetectedAt.Equal(hNow) {
		t.Fatalf("%+v", r)
	}
}

func TestAFindingThatStopsBeingTrueResolves(t *testing.T) {
	first := hNow.Add(-time.Hour)
	r := ReconcileHealth(hNow, []HealthFinding{stored(hf("a", HealthRisk), HealthOpen, first)}, nil)
	if r.Resolved != 1 || r.Write[0].State != HealthResolved || r.Write[0].ResolvedAt == nil || !r.Write[0].ResolvedAt.Equal(hNow) {
		t.Fatalf("%+v", r)
	}
}

func TestAResolvedFindingIsNotWrittenAgain(t *testing.T) {
	r := ReconcileHealth(hNow, []HealthFinding{stored(hf("a", HealthRisk), HealthResolved, hNow.Add(-5*time.Hour))}, nil)
	if len(r.Write) != 0 || r.Any() {
		t.Fatalf("%+v", r)
	}
}

func TestAFindingThatComesBackIsNew(t *testing.T) {
	old := hNow.Add(-5 * 24 * time.Hour)
	r := ReconcileHealth(hNow, []HealthFinding{stored(hf("a", HealthRisk), HealthResolved, old)}, []HealthFinding{hf("a", HealthRisk)})
	if r.Opened != 1 || r.Write[0].State != HealthOpen || !r.Write[0].DetectedAt.Equal(hNow) || r.Write[0].ResolvedAt != nil {
		t.Fatalf("%+v", r)
	}
}

func TestSeverityChangeIsAChange(t *testing.T) {
	first := hNow.Add(-time.Hour)
	r := ReconcileHealth(hNow, []HealthFinding{stored(hf("a", HealthAttention), HealthOpen, first)}, []HealthFinding{hf("a", HealthRisk)})
	if r.Changed != 1 || r.Write[0].Severity != HealthRisk || !r.Write[0].DetectedAt.Equal(first) {
		t.Fatalf("%+v", r)
	}
}

func TestADismissedFindingStaysDismissedUnlessItGetsWorse(t *testing.T) {
	first := hNow.Add(-48 * time.Hour)
	dismissed := stored(hf("a", HealthAttention), HealthDismissed, first)

	same := ReconcileHealth(hNow, []HealthFinding{dismissed}, []HealthFinding{hf("a", HealthAttention)})
	if same.Write[0].State != HealthDismissed || same.Any() || same.Write[0].DismissedAt == nil {
		t.Fatalf("a dismissed finding that is no worse stays dismissed: %+v", same)
	}
	better := ReconcileHealth(hNow, []HealthFinding{dismissed}, []HealthFinding{hf("a", HealthInfo)})
	if better.Write[0].State != HealthDismissed {
		t.Fatalf("a dismissed finding that eased stays dismissed: %+v", better)
	}
	worse := ReconcileHealth(hNow, []HealthFinding{dismissed}, []HealthFinding{hf("a", HealthRisk)})
	if worse.Write[0].State != HealthOpen || worse.Opened != 1 || worse.Write[0].DismissedAt != nil || !worse.Write[0].DetectedAt.Equal(first) {
		t.Fatalf("a dismissed finding that got worse comes back, keeping when it was first seen: %+v", worse)
	}
	gone := ReconcileHealth(hNow, []HealthFinding{dismissed}, nil)
	if gone.Write[0].State != HealthResolved || gone.Write[0].DismissedAt != nil || gone.Resolved != 1 {
		t.Fatalf("a dismissed finding that goes away resolves: %+v", gone)
	}
	// ... and if it returns later it is not still dismissed.
	back := ReconcileHealth(hNow.Add(time.Hour), []HealthFinding{gone.Write[0]}, []HealthFinding{hf("a", HealthAttention)})
	if back.Write[0].State != HealthOpen {
		t.Fatalf("it returned: %+v", back)
	}
}

func TestDismissAndReopen(t *testing.T) {
	f := stored(hf("a", HealthRisk), HealthOpen, hNow.Add(-time.Hour))
	d, ok := DismissHealth(f, hNow)
	if !ok || d.State != HealthDismissed || d.DismissedSeverity != HealthRisk || d.DismissedAt == nil {
		t.Fatalf("%+v", d)
	}
	if _, ok := DismissHealth(d, hNow); ok {
		t.Fatal("a dismissed finding cannot be dismissed again")
	}
	if _, ok := DismissHealth(stored(hf("a", HealthRisk), HealthResolved, hNow), hNow); ok {
		t.Fatal("a resolved finding cannot be dismissed")
	}
	o, ok := ReopenHealth(d, hNow)
	if !ok || o.State != HealthOpen || o.DismissedAt != nil || o.DismissedSeverity != "" {
		t.Fatalf("%+v", o)
	}
	if _, ok := ReopenHealth(o, hNow); ok {
		t.Fatal("an open finding cannot be reopened")
	}
}

func TestWordingAgreesWithTheCount(t *testing.T) {
	for ahead, want := range map[int]string{1: "1 commit exists here", 3: "3 commits exist here"} {
		h := newHB()
		h.agent("devboard/feature").upstream(UpstreamAhead, ahead, 0)
		var got string
		for _, f := range h.findings() {
			if f.Type == FindUnpushedCommits {
				got = f.Explanation
			}
		}
		if !strings.Contains(got, want) {
			t.Errorf("ahead %d: %q should contain %q", ahead, got, want)
		}
	}
}

// Asking an agent is a step the user takes: the finding only offers to add a card.
func TestAskingAnAgentOnlyAddsATask(t *testing.T) {
	h := newHB()
	h.agent("devboard/feature").runState(RunRunning).taskState(TaskDoing).withoutBranch()
	for _, f := range h.findings() {
		if f.Type != FindMissingBranch {
			continue
		}
		a := f.Action
		if a.Kind != ActAskAgent || !a.CanPerform || a.Destructive || !strings.Contains(a.Detail, "Nothing runs until you press Run") {
			t.Fatalf("action = %+v", a)
		}
		if !strings.Contains(a.TaskDescription, "Do not delete or reset anything") {
			t.Fatalf("the task must tell the agent not to destroy anything: %q", a.TaskDescription)
		}
		return
	}
	t.Fatal("no missing-branch finding")
}
