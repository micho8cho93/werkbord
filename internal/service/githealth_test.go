package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
)

// These tests run the health system over real temporary repositories: real
// branches, real worktrees, real merges and conflicts, a real bare remote. What
// is under test is that Git's answers are read correctly and turned into the right
// findings, and, as much, that nothing is reported that is not true.

type hFix struct {
	*gcFixture
	h   *GitHealth
	clk *testClock
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newHealth(t *testing.T) *hFix {
	t.Helper()
	f := newGC(t)
	clk := &testClock{t: time.Now().UTC()}
	f.gc.Now = clk.now
	h := &GitHealth{Deps: f.deps, Control: f.gc}
	h.Now = clk.now
	// A clone that has fetched at least once, as a repository in use has.
	git(t, f.repo, "fetch", "-q", "origin")
	return &hFix{gcFixture: f, h: h, clk: clk}
}

func (f *hFix) refresh(t *testing.T) *domain.HealthReport {
	t.Helper()
	r, err := f.h.Refresh(f.ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func find(r *domain.HealthReport, typ domain.HealthFindingType, branch string) *domain.HealthFinding {
	for i := range r.Findings {
		if r.Findings[i].Type == typ && (branch == "" || r.Findings[i].Subject.Branch == branch) {
			return &r.Findings[i]
		}
	}
	return nil
}

func typesOf(r *domain.HealthReport) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, string(f.Type)+"("+string(f.Severity)+") "+f.Subject.Branch)
	}
	return out
}

func (f *hFix) expectHealthy(t *testing.T, r *domain.HealthReport) {
	t.Helper()
	if r.Summary.State != domain.HealthHealthy || r.Summary.NeedsAttention != 0 {
		t.Fatalf("expected healthy, got %q: %v", r.Summary.Headline, typesOf(r))
	}
}

// pushBranch publishes a branch, so that its work is not "only on this computer".
func (f *hFix) pushBranch(t *testing.T, branch string) {
	t.Helper()
	git(t, f.repo, "push", "-q", "-u", "origin", branch)
}

// pushed is an agent branch whose work has been pushed, which is the ordinary state of a finished agent.
func (f *hFix) pushed(t *testing.T, title string, n int) *agentBranch {
	t.Helper()
	a := f.agent(t, title, n)
	f.pushBranch(t, a.branch)
	return a
}

// working puts an agent back to work on the branch: a run with a live session.
func (f *hFix) working(t *testing.T, a *agentBranch) {
	t.Helper()
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: a.task.ID, AgentID: "fake", Prompt: "more", WorktreeID: a.wt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	a.moveTask(t, f.gcFixture, domain.TaskDoing)
}

func (f *hFix) mainFile(t *testing.T, name, content string) {
	t.Helper()
	commitIn(t, f.repo, name, content, "main: "+name)
	git(t, f.repo, "push", "-q", "origin", "main")
}

// ---- the baseline ----

func TestAQuietRepositoryIsHealthy(t *testing.T) {
	f := newHealth(t)
	r := f.refresh(t)
	f.expectHealthy(t, r)
	if r.Summary.Headline != "Healthy" || len(r.Findings) != 0 || r.Check == nil || r.Check.Error != "" {
		t.Fatalf("report = %+v", r)
	}
}

func TestFinishedAndPushedWorkWaitingForReviewIsNotAProblem(t *testing.T) {
	f := newHealth(t)
	f.pushed(t, "add login", 2)
	f.pushed(t, "add search", 1)
	r := f.refresh(t)
	f.expectHealthy(t, r)
}

func TestAnAgentThatIsWorkingIsNotAProblemWhateverItsStateLooksLike(t *testing.T) {
	f := newHealth(t)
	a := f.agent(t, "busy", 2) // never pushed
	f.working(t, a)
	writeFile(t, a.path, "wip.txt", "wip")
	for i := 0; i < 8; i++ {
		writeFile(t, a.path, "scratch/"+string(rune('a'+i))+".txt", "x")
	}
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "edited")
	f.clk.advance(3 * 24 * time.Hour)
	r := f.refresh(t)
	for _, fd := range r.Findings {
		if fd.Subject.Branch == a.branch {
			t.Fatalf("a branch with a live session produced %s: %s", fd.Type, fd.Title)
		}
	}
}

// ---- uncommitted work, and a finding's life ----

func TestUncommittedWorkIsFoundThenResolvedWhenCommitted(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	f.expectHealthy(t, f.refresh(t))

	file := "agent-" + a.task.ID[len(a.task.ID)-6:] + "-a.txt"
	writeFile(t, a.path, file, "changed\n")
	r := f.refresh(t)
	fd := find(r, domain.FindUncommittedWork, a.branch)
	if fd == nil || fd.Severity != domain.HealthAttention || fd.State != domain.HealthOpen || fd.Subject.TaskID != a.task.ID || fd.Subject.WorktreeID != a.wt.ID {
		t.Fatalf("expected uncommitted work on the agent's worktree, got %v", typesOf(r))
	}
	if r.Summary.State != "attention" || r.Summary.Headline != "1 item needs attention" {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if fd.Action.Kind != domain.ActReviewChanges || !fd.Action.CanPerform || fd.Action.WorktreeID != a.wt.ID {
		t.Fatalf("the next step is to review the changes: %+v", fd.Action)
	}
	first := fd.DetectedAt

	// An hour later it is still the same finding, detected when it first was.
	f.clk.advance(time.Hour)
	fd = find(f.refresh(t), domain.FindUncommittedWork, a.branch)
	if fd == nil || !fd.DetectedAt.Equal(first) {
		t.Fatalf("a finding keeps the time it was first detected: %+v", fd)
	}

	git(t, a.path, "add", "-A")
	git(t, a.path, "commit", "-q", "-m", "finish")
	f.pushBranch(t, a.branch)
	r = f.refresh(t)
	f.expectHealthy(t, r)
	if len(r.Resolved) != 1 || r.Resolved[0].Type != domain.FindUncommittedWork || r.Resolved[0].ResolvedAt == nil {
		t.Fatalf("a fix is visible, not silent: %+v", r.Resolved)
	}
}

func TestUncommittedWorkLeftForADayBecomesARisk(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	if fd := find(f.refresh(t), domain.FindUncommittedWork, a.branch); fd == nil || fd.Severity != domain.HealthAttention {
		t.Fatalf("first it needs attention: %+v", fd)
	}
	f.clk.advance(30 * time.Hour)
	fd := find(f.refresh(t), domain.FindUncommittedWork, a.branch)
	if fd == nil || fd.Severity != domain.HealthRisk {
		t.Fatalf("a day later it is at risk: %+v", fd)
	}
}

func TestAFewScratchFilesAreNotReported(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, a.path, "notes-"+n+".txt", "x")
	}
	f.expectHealthy(t, f.refresh(t))
	for _, n := range []string{"d", "e"} {
		writeFile(t, a.path, "notes-"+n+".txt", "x")
	}
	if find(f.refresh(t), domain.FindUncommittedWork, a.branch) == nil {
		t.Fatal("five untracked files are significant work")
	}
}

func TestAnEditInTheUsersOwnCheckoutIsOnlyInformation(t *testing.T) {
	f := newHealth(t)
	writeFile(t, f.repo, "README.md", "edited by me\n")
	r := f.refresh(t)
	f.expectHealthy(t, r)
	if fd := find(r, domain.FindUncommittedWork, ""); fd == nil || fd.Severity != domain.HealthInfo {
		t.Fatalf("housekeeping expected: %v", typesOf(r))
	}
	// But once there is work to merge, it stands in the way.
	f.pushed(t, "feature", 1)
	if fd := find(f.refresh(t), domain.FindUncommittedWork, ""); fd == nil || fd.Severity != domain.HealthAttention {
		t.Fatal("uncommitted changes in the checkout block a merge that is ready")
	}
}

// ---- unsynced work ----

func TestUnpushedWorkIsFoundThenResolvedByPushing(t *testing.T) {
	f := newHealth(t)
	a := f.agent(t, "feature", 2)
	r := f.refresh(t)
	fd := find(r, domain.FindBranchNotPushed, a.branch)
	if fd == nil || fd.Severity != domain.HealthAttention || fd.Action.Kind != domain.ActPushBranch || !fd.Action.CanPerform {
		t.Fatalf("a branch never pushed: %v", typesOf(r))
	}
	f.pushBranch(t, a.branch)
	f.expectHealthy(t, f.refresh(t))

	commitIn(t, a.path, "more.txt", "more\n", "another commit")
	fd = find(f.refresh(t), domain.FindUnpushedCommits, a.branch)
	if fd == nil || !strings.Contains(fd.Title, "1 commit not pushed") {
		t.Fatalf("a new commit after the push: %+v", fd)
	}
}

func TestAMergeMadeHereIsNotOnTheRemoteUntilPushed(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge feature", a.branch)
	r := f.refresh(t)
	if fd := find(r, domain.FindUnpushedCommits, "main"); fd == nil || fd.Severity != domain.HealthAttention {
		t.Fatalf("main is ahead of origin/main: %v", typesOf(r))
	}
	if fd := find(r, domain.FindMergedBranch, a.branch); fd == nil || fd.Severity != domain.HealthInfo {
		t.Fatalf("the merged branch is housekeeping: %v", typesOf(r))
	}
	git(t, f.repo, "push", "-q", "origin", "main")
	r = f.refresh(t)
	if find(r, domain.FindUnpushedCommits, "main") != nil {
		t.Fatal("pushed: it must resolve")
	}
}

func TestADivergedBranchIsARiskAndPushIsNotOffered(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	other := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	git(t, other, "checkout", "-q", a.branch)
	commitIn(t, other, "theirs.txt", "x\n", "someone else's commit")
	git(t, other, "push", "-q", "origin", a.branch)
	commitIn(t, a.path, "mine.txt", "x\n", "my commit")
	git(t, f.repo, "fetch", "-q", "origin")

	r := f.refresh(t)
	fd := find(r, domain.FindUpstreamDiverged, a.branch)
	if fd == nil || fd.Severity != domain.HealthRisk || fd.Action.CanPerform || fd.Action.Reason == "" || fd.Action.Kind != domain.ActSyncBranch {
		t.Fatalf("diverged: %v / %+v", typesOf(r), fd)
	}
}

func TestTheRemoteBeingAheadIsReportedForTheTarget(t *testing.T) {
	f := newHealth(t)
	other := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	commitIn(t, other, "upstream.txt", "x\n", "upstream work")
	git(t, other, "push", "-q", "origin", "main")
	git(t, f.repo, "fetch", "-q", "origin")
	r := f.refresh(t)
	if fd := find(r, domain.FindRemoteAhead, "main"); fd == nil || fd.Severity != domain.HealthAttention || fd.Action.CanPerform {
		t.Fatalf("main is behind origin: %v", typesOf(r))
	}
}

func TestAnOldFetchOnlyMattersWhenThereIsAgentWork(t *testing.T) {
	f := newHealth(t)
	f.clk.advance(30 * 24 * time.Hour)
	// Never fetched since the clone: nothing to be wrong about yet.
	f.expectHealthy(t, f.refresh(t))
	f.pushed(t, "feature", 1)
	git(t, f.repo, "fetch", "-q", "origin")
	f.clk.advance(-30 * 24 * time.Hour)
	if find(f.refresh(t), domain.FindRemoteStateStale, "") != nil {
		t.Fatal("a fresh fetch must not be reported")
	}
	f.clk.advance(10 * 24 * time.Hour)
	if fd := find(f.refresh(t), domain.FindRemoteStateStale, ""); fd == nil || fd.Severity != domain.HealthInfo || !fd.Action.CanPerform {
		t.Fatal("a ten day old fetch is worth a line of housekeeping")
	}
}

// ---- branch hygiene and orchestration ----

func TestADoneTaskWhoseBranchIsNotMergedIsARisk(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 2)
	a.moveTask(t, f.gcFixture, domain.TaskDone)
	r := f.refresh(t)
	fd := find(r, domain.FindTaskDoneUnmerged, a.branch)
	if fd == nil || fd.Severity != domain.HealthRisk || fd.Basis != domain.BasisDeterministic || fd.Subject.TaskID != a.task.ID {
		t.Fatalf("a Done task with an unmerged branch: %v", typesOf(r))
	}
	if fd.Action.Kind != domain.ActMergeBranch || !fd.Action.CanPerform {
		t.Fatalf("with a clean checkout the merge can be offered: %+v", fd.Action)
	}

	// Once it is merged, the risk is gone and only housekeeping is left.
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge", a.branch)
	git(t, f.repo, "push", "-q", "origin", "main")
	r = f.refresh(t)
	if find(r, domain.FindTaskDoneUnmerged, a.branch) != nil {
		t.Fatalf("merged: %v", typesOf(r))
	}
	f.expectHealthy(t, r)
}

// A squash or rebase merge puts the content on the target under other commits, so by
// history the branch is unmerged. Git's own merge, run in memory, shows it adds nothing.
func TestASquashMergedBranchIsRecognisedByItsContent(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 2)
	a.moveTask(t, f.gcFixture, domain.TaskDone)
	git(t, f.repo, "merge", "-q", "--squash", a.branch)
	git(t, f.repo, "commit", "-q", "-m", "squash feature")
	git(t, f.repo, "push", "-q", "origin", "main")

	r := f.refresh(t)
	if find(r, domain.FindTaskDoneUnmerged, a.branch) != nil {
		t.Fatalf("a squash-merged Done task is not unmerged work: %v", typesOf(r))
	}
	fd := find(r, domain.FindContentOnTarget, a.branch)
	if fd == nil || fd.Severity != domain.HealthInfo || fd.Basis != domain.BasisDeterministic {
		t.Fatalf("expected the content to be recognised: %v", typesOf(r))
	}
	f.expectHealthy(t, r)
}

func TestFinishedWorkSittingUnmergedForADayIsNudged(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	f.clk.advance(5 * time.Hour)
	f.expectHealthy(t, f.refresh(t))
	f.clk.advance(30 * time.Hour)
	fd := find(f.refresh(t), domain.FindFinishedUnmerged, a.branch)
	if fd == nil || fd.Basis != domain.BasisHeuristic || fd.Severity != domain.HealthAttention {
		t.Fatalf("finished work waiting more than a day: %+v", fd)
	}
}

func TestAnAbandonedBranchIsFoundButOnlyAsAGuess(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	a.moveTask(t, f.gcFixture, domain.TaskBacklog)
	f.clk.advance(5 * 24 * time.Hour)
	fd := find(f.refresh(t), domain.FindAbandonedBranch, a.branch)
	if fd == nil || fd.Basis != domain.BasisHeuristic || strings.Contains(strings.ToLower(fd.Explanation), "is abandoned") {
		t.Fatalf("a quiet branch whose task went back to the backlog: %+v", fd)
	}
}

func TestABranchBehindTheTargetByManyCommitsIsFlagged(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	for i := 0; i < 26; i++ {
		commitIn(t, f.repo, "m"+string(rune('a'+i%26))+".txt", "x\n", "main moves")
	}
	git(t, f.repo, "push", "-q", "origin", "main")
	fd := find(f.refresh(t), domain.FindBranchFarBehind, a.branch)
	if fd == nil || fd.Severity != domain.HealthAttention {
		t.Fatalf("26 commits behind: %+v", fd)
	}
}

// ---- agents stepping on each other ----

func sharedBase(t *testing.T, f *hFix) {
	t.Helper()
	body := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	f.mainFile(t, "shared.txt", body)
}

func TestGitProvesTwoBranchesConflict(t *testing.T) {
	f := newHealth(t)
	sharedBase(t, f)
	a := f.agent(t, "alpha", 0)
	b := f.agent(t, "beta", 0)
	commitIn(t, a.path, "shared.txt", "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "alpha edits line 1")
	commitIn(t, b.path, "shared.txt", "uno\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "beta edits line 1")
	f.pushBranch(t, a.branch)
	f.pushBranch(t, b.branch)

	r := f.refresh(t)
	fd := find(r, domain.FindBranchConflict, "")
	if fd == nil || fd.Severity != domain.HealthRisk || fd.Basis != domain.BasisDeterministic || len(fd.Subject.Related) != 1 {
		t.Fatalf("Git can prove this conflict: %v", typesOf(r))
	}
	if !strings.Contains(strings.Join(evidenceValues(fd), " "), "shared.txt") {
		t.Fatalf("the evidence names the file: %+v", fd.Evidence)
	}
	if find(r, domain.FindBranchOverlap, "") != nil {
		t.Fatal("a proven conflict must not also be reported as a guess")
	}
	// The simulation changed nothing.
	if st := gitOut(t, f.repo, "status", "--porcelain"); st != "" {
		t.Fatalf("checking for conflicts dirtied the checkout: %q", st)
	}
}

func evidenceValues(f *domain.HealthFinding) []string {
	var out []string
	for _, e := range f.Evidence {
		out = append(out, e.Value)
	}
	return out
}

func TestTwoBranchesEditingDifferentPartsOfOneFileAreNotAProblem(t *testing.T) {
	f := newHealth(t)
	sharedBase(t, f)
	a := f.agent(t, "alpha", 0)
	b := f.agent(t, "beta", 0)
	commitIn(t, a.path, "shared.txt", "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "alpha edits the top")
	commitIn(t, b.path, "shared.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n", "beta edits the bottom")
	f.pushBranch(t, a.branch)
	f.pushBranch(t, b.branch)
	r := f.refresh(t)
	f.expectHealthy(t, r)
}

func TestOverlapWithUncommittedWorkIsAGuessAndSaysMay(t *testing.T) {
	f := newHealth(t)
	sharedBase(t, f)
	a := f.agent(t, "alpha", 0)
	b := f.agent(t, "beta", 0)
	commitIn(t, a.path, "shared.txt", "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "alpha edits the top")
	f.pushBranch(t, a.branch)
	f.pushBranch(t, b.branch)
	f.working(t, b)
	writeFile(t, b.path, "shared.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n") // not committed yet

	r := f.refresh(t)
	fd := find(r, domain.FindBranchOverlap, "")
	if fd == nil || fd.Basis != domain.BasisHeuristic || fd.Severity != domain.HealthAttention {
		t.Fatalf("expected a heuristic overlap: %v", typesOf(r))
	}
	if strings.Contains(strings.ToLower(fd.Title), "conflict") || !strings.Contains(fd.Explanation, "may conflict") {
		t.Fatalf("an overlap is not a conflict: %q / %q", fd.Title, fd.Explanation)
	}
	if fd.Action.Kind != domain.ActCreateTask || fd.Action.TaskTitle == "" || fd.Action.TaskDescription == "" {
		t.Fatalf("the next step is a task: %+v", fd.Action)
	}
}

func TestLockFilesAndABranchBuiltOnAnotherDoNotLookLikeCollisions(t *testing.T) {
	f := newHealth(t)
	f.mainFile(t, "go.sum", "a\n")
	a := f.agent(t, "alpha", 0)
	b := f.agent(t, "beta", 0)
	commitIn(t, a.path, "go.sum", "a\nb\n", "alpha updates the lock file")
	commitIn(t, b.path, "go.sum", "a\nc\n", "beta updates the lock file")
	f.pushBranch(t, a.branch)
	f.pushBranch(t, b.branch)
	f.expectHealthy(t, f.refresh(t))

	// beta contains alpha: what they share is inherited.
	git(t, b.path, "merge", "-q", "--no-edit", "-X", "theirs", a.branch)
	commitIn(t, a.path, "shared.txt", "from alpha\n", "alpha adds a file")
	git(t, b.path, "merge", "-q", "--no-edit", a.branch)
	f.pushBranch(t, a.branch)
	git(t, f.repo, "push", "-q", "origin", b.branch)
	r := f.refresh(t)
	if find(r, domain.FindBranchOverlap, "") != nil || find(r, domain.FindBranchConflict, "") != nil {
		t.Fatalf("beta contains alpha: %v", typesOf(r))
	}
}

// ---- worktrees ----

func TestAWorktreeInDevBoardsDirectoryThatNoRecordOwnsIsFound(t *testing.T) {
	f := newHealth(t)
	f.h.Control.Worktrees = f.wts
	stray := filepath.Join(f.wtRoot, "stray")
	git(t, f.repo, "worktree", "add", "-q", "-b", "stray-branch", stray)
	r := f.refresh(t)
	fd := find(r, domain.FindOrphanedWorktree, "stray-branch")
	if fd == nil || fd.Action.CanPerform || fd.Action.Reason == "" {
		t.Fatalf("an orphaned worktree: %v", typesOf(r))
	}
	// A worktree of the user's elsewhere is none of Werkbord's business.
	elsewhere := filepath.Join(t.TempDir(), "mine")
	git(t, f.repo, "worktree", "add", "-q", "-b", "mine", elsewhere)
	if r := f.refresh(t); find(r, domain.FindOrphanedWorktree, "mine") != nil {
		t.Fatal("a worktree outside Werkbord's directory is not orphaned")
	}
}

func TestAWorktreeRecordNoRunEverUsedIsFoundAfterAGracePeriod(t *testing.T) {
	f := newHealth(t)
	path := filepath.Join(f.wtRoot, f.project.ID, "unused")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, err := f.wts.Create(f.ctx, NewWorktree{ProjectID: f.project.ID, Path: path, Branch: "devboard/unused-aaaaaa", BaseRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.git.AddWorktree(f.ctx, f.repo, path, "devboard/unused-aaaaaa", gitOut(t, f.repo, "rev-parse", "main")); err != nil {
		t.Fatal(err)
	}
	f.expectHealthy(t, f.refresh(t)) // just created: its run has not had time to start

	f.clk.advance(time.Hour)
	r := f.refresh(t)
	fd := find(r, domain.FindWorktreeNoRun, "devboard/unused-aaaaaa")
	if fd == nil || fd.Subject.WorktreeID != rec.ID || fd.Action.Kind != domain.ActCleanWorktree || !fd.Action.CanPerform || !fd.Action.Destructive {
		t.Fatalf("an unused worktree: %v / %+v", typesOf(r), fd)
	}
}

func TestAWorktreeSwitchedToAnotherBranchDoesNotMatchItsRecord(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	git(t, a.path, "checkout", "-q", "-b", "somewhere-else")
	r := f.refresh(t)
	fd := find(r, domain.FindWorktreeMismatch, a.branch)
	if fd == nil || fd.Action.Kind == domain.ActCleanWorktree || fd.Action.Kind != domain.ActAskAgent || fd.Action.Destructive {
		t.Fatalf("Werkbord must not offer to clean a worktree that no longer matches its record: %v / %+v", typesOf(r), fd)
	}
}

func TestADirectoryThatWasDeletedUnderDevBoardIsReportedAndCanBeRetired(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	if err := os.RemoveAll(a.path); err != nil {
		t.Fatal(err)
	}
	r := f.refresh(t)
	fd := find(r, domain.FindWorktreeMismatch, a.branch)
	if fd == nil || !fd.Action.CanPerform {
		t.Fatalf("a missing directory: %v / %+v", typesOf(r), fd)
	}
}

func TestACleanWorktreeOfADoneTaskIsHousekeeping(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge", a.branch)
	git(t, f.repo, "push", "-q", "origin", "main")
	a.moveTask(t, f.gcFixture, domain.TaskDone)
	r := f.refresh(t)
	fd := find(r, domain.FindMergedBranch, a.branch)
	if fd == nil || fd.Action.Kind != domain.ActCleanWorktree || !fd.Action.CanPerform {
		t.Fatalf("the worktree has to be cleaned before the branch can go: %v / %+v", typesOf(r), fd)
	}
	f.expectHealthy(t, r)
}

// ---- Git operations ----

func conflictOn(t *testing.T, dir, base string) {
	t.Helper()
	commitIn(t, dir, "clash.txt", "from "+base+"\n", "commit "+base)
}

func TestAnInterruptedMergeWithConflictsInAnAgentWorktreeIsFound(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	f.mainFile(t, "clash.txt", "main's version\n")
	commitIn(t, a.path, "clash.txt", "the agent's version\n", "agent edits it too")
	f.pushBranch(t, a.branch)
	cmd := exec.Command("git", "-C", a.path, "merge", "main")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	_ = cmd.Run() // stops on the conflict, as the agent might have

	r := f.refresh(t)
	fd := find(r, domain.FindUnresolvedConflict, a.branch)
	if fd == nil || fd.Severity != domain.HealthRisk || fd.Action.CanPerform || fd.Action.Kind != domain.ActFinishOperation {
		t.Fatalf("conflicts in a worktree: %v", typesOf(r))
	}
	if find(r, domain.FindOperationInterrupt, a.branch) != nil {
		t.Fatal("the conflict is the finding; the unfinished merge is part of it")
	}
}

func TestAnInterruptedMergeInTheUsersCheckoutIsCritical(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	commitIn(t, a.path, "clash.txt", "agent\n", "agent adds clash.txt")
	f.pushBranch(t, a.branch)
	commitIn(t, f.repo, "clash.txt", "main\n", "main adds clash.txt")
	git(t, f.repo, "push", "-q", "origin", "main")
	cmd := exec.Command("git", "-C", f.repo, "merge", a.branch)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
	_ = cmd.Run()

	r := f.refresh(t)
	fd := find(r, domain.FindUnresolvedConflict, "")
	if fd == nil || fd.Severity != domain.HealthCritical || r.Summary.State != "critical" {
		t.Fatalf("conflicts in the user's own checkout: %v", typesOf(r))
	}
	// Resolved by aborting.
	git(t, f.repo, "merge", "--abort")
	r = f.refresh(t)
	if find(r, domain.FindUnresolvedConflict, "") != nil {
		t.Fatalf("aborted: %v", typesOf(r))
	}
}

func TestANonConflictingInterruptedOperationIsAttentionOrRisk(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	// A merge that stopped before committing, without conflicts.
	f.mainFile(t, "other.txt", "x\n")
	git(t, a.path, "merge", "-q", "--no-commit", "--no-ff", "main")
	r := f.refresh(t)
	fd := find(r, domain.FindOperationInterrupt, a.branch)
	if fd == nil || fd.Severity != domain.HealthAttention {
		t.Fatalf("an unfinished merge: %v", typesOf(r))
	}
}

func TestADetachedHeadAndATargetThatIsNotCheckedOutBlockMerging(t *testing.T) {
	f := newHealth(t)
	f.expectHealthy(t, f.refresh(t))
	git(t, f.repo, "checkout", "-q", "--detach")
	r := f.refresh(t)
	fd := find(r, domain.FindAutomationBlocked, "")
	if fd == nil || fd.Severity != domain.HealthInfo {
		t.Fatalf("with nothing to merge a detached HEAD is information: %v", typesOf(r))
	}
	f.pushed(t, "feature", 1)
	r = f.refresh(t)
	attention := 0
	for _, fd := range r.Findings {
		if fd.Type == domain.FindAutomationBlocked && fd.Severity == domain.HealthAttention {
			attention++
		}
	}
	if attention != 2 {
		t.Fatalf("a detached HEAD and a target that is not checked out, with work waiting: %v", typesOf(r))
	}
}

func TestAStaleIndexLockIsFoundAndAFreshOneIsNot(t *testing.T) {
	f := newHealth(t)
	lock := filepath.Join(f.repo, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f.expectHealthy(t, f.refresh(t)) // Git may be working right now
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	r := f.refresh(t)
	fd := find(r, domain.FindAutomationBlocked, "")
	if fd == nil || fd.Action.CanPerform || !strings.Contains(fd.Title, "lock") {
		t.Fatalf("a stale lock: %v", typesOf(r))
	}
}

func TestAnUnreadableRepositoryIsOneFindingAndLeavesTheRestAlone(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	if find(f.refresh(t), domain.FindUncommittedWork, a.branch) == nil {
		t.Fatal("setup: the uncommitted work should be found")
	}

	moved := f.repo + ".moved"
	if err := os.Rename(f.repo, moved); err != nil {
		t.Fatal(err)
	}
	r := f.refresh(t)
	if find(r, domain.FindRepositoryUnread, "") == nil {
		t.Fatalf("the repository cannot be read: %v", typesOf(r))
	}
	if find(r, domain.FindUncommittedWork, a.branch) == nil {
		t.Fatal("what could not be checked must not be reported as fixed")
	}
	if r.Check == nil || r.Check.Error == "" {
		t.Fatalf("the check should say it failed: %+v", r.Check)
	}

	if err := os.Rename(moved, f.repo); err != nil {
		t.Fatal(err)
	}
	r = f.refresh(t)
	if find(r, domain.FindRepositoryUnread, "") != nil || r.Check.Error != "" {
		t.Fatalf("readable again: %v", typesOf(r))
	}
}

// ---- dismissing ----

func TestADismissedFindingIsHiddenUntilItGetsWorse(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	r := f.refresh(t)
	fd := find(r, domain.FindUncommittedWork, a.branch)
	if fd == nil || r.Summary.State != "attention" {
		t.Fatal("setup")
	}
	r, err := f.h.Dismiss(f.ctx, f.project.ID, fd.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.expectHealthy(t, r)
	if len(r.Dismissed) != 1 || r.Summary.Dismissed != 1 {
		t.Fatalf("it is still visible, folded away: %+v", r.Summary)
	}
	if _, err := f.h.Dismiss(f.ctx, f.project.ID, fd.ID); err == nil {
		t.Fatal("a dismissed finding cannot be dismissed twice")
	}
	f.expectHealthy(t, f.refresh(t)) // still true, no worse: stays hidden

	f.clk.advance(30 * time.Hour) // now a risk
	r = f.refresh(t)
	if fd := find(r, domain.FindUncommittedWork, a.branch); fd == nil || fd.Severity != domain.HealthRisk || fd.State != domain.HealthOpen {
		t.Fatalf("it got worse, so it is back: %v", typesOf(r))
	}
}

func TestReopeningADismissedFinding(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	fd := find(f.refresh(t), domain.FindUncommittedWork, a.branch)
	if _, err := f.h.Dismiss(f.ctx, f.project.ID, fd.ID); err != nil {
		t.Fatal(err)
	}
	r, err := f.h.Reopen(f.ctx, f.project.ID, fd.ID)
	if err != nil || find(r, domain.FindUncommittedWork, a.branch) == nil {
		t.Fatalf("reopen: %v / %v", err, typesOf(r))
	}
	if _, err := f.h.Dismiss(f.ctx, f.project.ID, "hf_unknown"); err == nil {
		t.Fatal("an unknown finding cannot be dismissed")
	}
}

// ---- cost and side effects ----

// The health system tells people what is wrong and changes nothing.
func TestRefreshingChangesNothingInTheRepository(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 2)
	b := f.agent(t, "other", 1)
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	writeFile(t, f.repo, "notes.txt", "mine\n")
	_ = b

	snapshot := func() string {
		var parts []string
		parts = append(parts, gitOut(t, f.repo, "for-each-ref"))
		parts = append(parts, gitOut(t, f.repo, "status", "--porcelain=v2", "--branch"))
		parts = append(parts, gitOut(t, a.path, "status", "--porcelain=v2"))
		parts = append(parts, gitOut(t, f.repo, "worktree", "list", "--porcelain"))
		parts = append(parts, gitOut(t, f.repo, "stash", "list"))
		return strings.Join(parts, "\n--\n")
	}
	before := snapshot()
	for i := 0; i < 3; i++ {
		f.refresh(t)
	}
	if after := snapshot(); after != before {
		t.Fatalf("a health check changed the repository:\n%s\n---\n%s", before, after)
	}
}

// Health uses Git metadata only. It must work, and fast, with a remote that cannot
// be reached, because it never goes there.
func TestRefreshNeverUsesTheNetwork(t *testing.T) {
	f := newHealth(t)
	f.pushed(t, "feature", 1)
	marker := filepath.Join(t.TempDir(), "network-was-used")
	script := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", script)
	git(t, f.repo, "config", "url.ssh://unreachable.invalid/.insteadOf", ghURL)
	git(t, f.repo, "config", "--unset-all", "url."+f.remote+".insteadOf")
	start := time.Now()
	f.refresh(t)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("recalculating health touched the network")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %s", d)
	}
}

// Reading a report is a database read. Only events, an explicit refresh, or an old
// calculation make it look at Git again.
func TestAReportIsNotRecalculatedEveryTimeItIsRead(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	r, err := f.h.Report(f.ctx, f.project.ID) // never checked: calculates
	if err != nil {
		t.Fatal(err)
	}
	f.expectHealthy(t, r)
	checked := r.Check.CheckedAt

	// Something changes in the repository, but nothing says so.
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	f.clk.advance(30 * time.Second)
	r, _ = f.h.Report(f.ctx, f.project.ID)
	if !r.Check.CheckedAt.Equal(checked) {
		t.Fatal("a fresh calculation must be read, not repeated")
	}
	f.expectHealthy(t, r)

	// An event that can change the answer makes the next read recalculate.
	f.h.setDirty(f.project.ID, true)
	r, _ = f.h.Report(f.ctx, f.project.ID)
	if find(r, domain.FindUncommittedWork, a.branch) == nil {
		t.Fatalf("after an event it looks again: %v", typesOf(r))
	}

	// ... and so does an old calculation, which is how changes made in a terminal are noticed.
	git(t, a.path, "add", "-A")
	git(t, a.path, "commit", "-q", "-m", "done")
	f.pushBranch(t, a.branch)
	f.clk.advance(5 * time.Minute)
	r, _ = f.h.Report(f.ctx, f.project.ID)
	f.expectHealthy(t, r)

	if _, err := f.h.Report(f.ctx, "proj_nope"); err == nil {
		t.Fatal("an unknown project")
	}
}

// ---- events ----

func TestAnEventIsPublishedOnlyWhenTheFindingsChange(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	sub := f.bus.Subscribe(64)
	defer sub.Close()
	count := func() int {
		n := 0
		for {
			select {
			case ev := <-sub.C:
				if ev.Type == domain.EventGitHealthChanged {
					if ev.ProjectID != f.project.ID {
						t.Fatalf("event project = %q", ev.ProjectID)
					}
					n++
				}
			default:
				return n
			}
		}
	}

	f.refresh(t)
	f.refresh(t)
	if n := count(); n != 0 {
		t.Fatalf("a quiet repository published %d health events", n)
	}
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	f.refresh(t)
	if n := count(); n != 1 {
		t.Fatalf("a finding opened: %d events", n)
	}
	f.refresh(t)
	f.refresh(t)
	if n := count(); n != 0 {
		t.Fatalf("recalculating the same thing published %d events", n)
	}
	git(t, a.path, "add", "-A")
	git(t, a.path, "commit", "-q", "-m", "done")
	f.pushBranch(t, a.branch)
	f.refresh(t)
	if n := count(); n != 1 {
		t.Fatalf("a finding resolved: %d events", n)
	}
}

// ---- watching ----

func TestTheWatcherRecalculatesAfterRelevantEventsOnly(t *testing.T) {
	f := newHealth(t)
	a := f.pushed(t, "feature", 1)
	f.h.Debounce = 40 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	f.h.Watch(ctx, f.bus)
	t.Cleanup(func() { cancel(); f.h.Wait() })

	waitFor(t, "the initial check", func() bool {
		r, err := f.h.stored(f.ctx, f.project.ID)
		return err == nil && r.Check != nil
	})
	checkedAt := func() time.Time {
		r, _ := f.h.stored(f.ctx, f.project.ID)
		return r.Check.CheckedAt
	}
	first := checkedAt()

	// Agent output and unrelated events do not cause work.
	f.bus.Publish(domain.Event{Type: domain.EventAgentOutput, ProjectID: f.project.ID}, domain.Event{Type: domain.EventGitHealthChanged, ProjectID: f.project.ID})
	time.Sleep(200 * time.Millisecond)
	if !checkedAt().Equal(first) {
		t.Fatal("agent output must not trigger a recalculation")
	}

	// A task moving does, and a burst of them costs one calculation.
	writeFile(t, a.path, "agent-"+a.task.ID[len(a.task.ID)-6:]+"-a.txt", "changed\n")
	f.clk.advance(time.Second)
	for i := 0; i < 20; i++ {
		f.bus.Publish(domain.Event{Type: domain.EventTaskUpdated, ProjectID: f.project.ID})
	}
	waitFor(t, "the recalculation", func() bool {
		r, err := f.h.stored(f.ctx, f.project.ID)
		return err == nil && find(r, domain.FindUncommittedWork, a.branch) != nil
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStoppingTheWatcherDoesNotHang(t *testing.T) {
	f := newHealth(t)
	f.h.Debounce = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	f.h.Watch(ctx, f.bus)
	f.bus.Publish(domain.Event{Type: domain.EventRunStateChanged, ProjectID: f.project.ID}) // arms a timer that will never fire
	time.Sleep(50 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() { f.h.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return")
	}
}

// ---- many agents ----

// A busy day: a dozen agents, all working in the same file. The checks stay
// bounded and the result is still a handful of findings, not a wall.
func TestManyAgentsStayBoundedAndReadable(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	f := newHealth(t)
	sharedBase(t, f)
	for i := 0; i < 12; i++ {
		a := f.agent(t, "agent"+string(rune('a'+i)), 0)
		commitIn(t, a.path, "shared.txt", strings.Replace("ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n", "ONE", "line from "+a.branch, 1), "edit")
		f.pushBranch(t, a.branch)
	}
	start := time.Now()
	r := f.refresh(t)
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("took %s", d)
	}
	conflicts := 0
	for _, fd := range r.Findings {
		if fd.Type == domain.FindBranchConflict {
			conflicts++
		}
	}
	// 10 branches are compared (the limit), so at most 45 pairs; and only conflicting pairs are reported.
	if conflicts == 0 || conflicts > 45 {
		t.Fatalf("expected between 1 and 45 conflicts, got %d", conflicts)
	}
}
