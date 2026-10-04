package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/github"
	"devboard/internal/gitrepo"
)

// These tests run the real git executable on temporary repositories, with real
// bare repositories as remotes, real linked worktrees and real task and run
// records. The behaviour under test is how Git's answers are read and how
// actions refuse, and a fake would hide exactly the bugs that matter.

const ghURL = "https://github.com/acme/app.git"

func isolate(t *testing.T) {
	t.Helper()
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitIn(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	writeFile(t, dir, name, content)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// gcFixture is a project whose repository has a bare "remote" (reached under a
// GitHub-looking URL through insteadOf, so GitHub detection and real pushes both
// work), a worktree directory, and a GitControl on the real Git CLI.
type gcFixture struct {
	*fixture
	gc      *GitControl
	git     *gitrepo.CLI
	repo    string
	remote  string
	wtRoot  string
	project *ProjectDetail
	wts     *Worktrees
	ctx     context.Context
}

func newGC(t *testing.T) *gcFixture {
	t.Helper()
	isolate(t)
	f := newFixture(t)
	g := &gitrepo.CLI{}
	f.projects.Git = g
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "app")
	remote := filepath.Join(base, "remote.git")
	wtRoot := filepath.Join(base, "worktrees")
	for _, d := range []string{repo, remote, wtRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "init", "-q", "-b", "main")
	commitIn(t, repo, "README.md", "hello\n", "init")
	git(t, remote, "init", "-q", "--bare", "-b", "main")
	git(t, repo, "remote", "add", "origin", ghURL)
	git(t, repo, "config", "url."+remote+".insteadOf", ghURL)
	git(t, repo, "push", "-q", "-u", "origin", "main")
	git(t, repo, "remote", "set-head", "origin", "main")

	ctx := context.Background()
	p, err := f.projects.Register(ctx, repo, "app")
	if err != nil {
		t.Fatal(err)
	}
	wts := &Worktrees{Deps: f.deps, Root: wtRoot}
	gc := &GitControl{Deps: f.deps, Git: g, Worktrees: wts}
	return &gcFixture{fixture: f, gc: gc, git: g, repo: repo, remote: remote, wtRoot: wtRoot, project: p, wts: wts, ctx: ctx}
}

// agentBranch is what a finished agent run leaves: a task, a Dev Board worktree
// with its record, a branch of the right name, a run that used it.
type agentBranch struct {
	task   *domain.Task
	run    *domain.Run
	wt     *domain.Worktree
	branch string
	path   string
}

// agent makes a Dev Board branch for a new task, with n commits on it. The run is
// left finished (completed) and the task in Review, as after a normal run.
func (f *gcFixture) agent(t *testing.T, title string, n int) *agentBranch {
	t.Helper()
	task, err := f.tasks.Create(f.ctx, f.project.ID, title, "")
	if err != nil {
		t.Fatal(err)
	}
	tail := task.ID[len(task.ID)-6:]
	a := &agentBranch{task: task, branch: "devboard/" + strings.ReplaceAll(strings.ToLower(title), " ", "-") + "-" + tail}
	a.path = filepath.Join(f.wtRoot, f.project.ID, strings.ReplaceAll(a.branch, "/", "_"))
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, f.repo, "rev-parse", "main")
	if a.wt, err = f.wts.Create(f.ctx, NewWorktree{ProjectID: f.project.ID, Path: a.path, Branch: a.branch, BaseRef: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.git.AddWorktree(f.ctx, f.repo, a.path, a.branch, head); err != nil {
		t.Fatal(err)
	}
	if a.run, err = f.runs.Create(f.ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "do it", WorktreeID: a.wt.ID}); err != nil {
		t.Fatal(err)
	}
	if a.run, err = f.runs.MarkStarted(f.ctx, a.run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		commitIn(t, a.path, "agent-"+tail+"-"+string(rune('a'+i))+".txt", "work\n", title+" commit "+string(rune('a'+i)))
	}
	a.finish(t, f)
	return a
}

// finish ends the run (completed) and moves the task to Review.
func (a *agentBranch) finish(t *testing.T, f *gcFixture) {
	t.Helper()
	var err error
	if a.run, err = f.runs.End(f.ctx, a.run.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	a.moveTask(t, f, domain.TaskReview)
}

func (a *agentBranch) moveTask(t *testing.T, f *gcFixture, st domain.TaskState) {
	t.Helper()
	cur, err := f.tasks.Get(f.ctx, a.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.task, err = f.tasks.Update(f.ctx, cur.ID, TaskPatch{State: &st, Version: cur.Version}); err != nil {
		t.Fatal(err)
	}
}

func (f *gcFixture) overview(t *testing.T) *domain.GitOverview {
	t.Helper()
	o, err := f.gc.Overview(f.ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func branchNamed(o *domain.GitOverview, name string) *domain.GitBranch {
	for i := range o.Branches {
		if o.Branches[i].Name == name && o.Branches[i].Scope == domain.ScopeLocal {
			return &o.Branches[i]
		}
	}
	return nil
}

func mustBranch(t *testing.T, o *domain.GitOverview, name string) *domain.GitBranch {
	t.Helper()
	b := branchNamed(o, name)
	if b == nil {
		t.Fatalf("branch %s is not in the overview", name)
	}
	return b
}

func hasBlocker(bs []domain.GitBlocker, code string) bool {
	for _, b := range bs {
		if b.Code == code {
			return true
		}
	}
	return false
}

func codes(bs []domain.GitBlocker) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Code)
	}
	return out
}

// ---- overview ----

func TestOverviewDescribesLocalAndRemoteSeparately(t *testing.T) {
	f := newGC(t)
	// Fetch so the remote side has a "last fetched" time.
	if res, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil || res.Outcome != domain.OutcomeDone {
		t.Fatalf("fetch = %+v, %v", res, err)
	}
	o := f.overview(t)

	l := o.Local
	if l.Name != "app" || l.RootPath != f.repo || l.Head.Branch != "main" || l.Head.Detached || l.Head.Commit != gitOut(t, f.repo, "rev-parse", "HEAD") || l.Head.Subject != "init" {
		t.Errorf("local = %+v", l)
	}
	if l.Target.Name != "main" || l.Target.Source != gitrepo.TargetFromOriginHead || !l.Target.LocalExists || l.Target.CheckedOut != f.repo || l.Target.Upstream.State != domain.UpstreamInSync {
		t.Errorf("target = %+v", l.Target)
	}
	if !l.WorkingTree.Clean || len(l.RecentCommits) != 1 || l.RecentCommits[0].Subject != "init" {
		t.Errorf("working tree = %+v, commits = %+v", l.WorkingTree, l.RecentCommits)
	}
	r := o.Remote
	if len(r.Remotes) != 1 || r.Remotes[0].Name != "origin" || r.Remotes[0].URL != ghURL {
		t.Errorf("remotes = %+v", r.Remotes)
	}
	if r.LastFetchedAt == nil || time.Since(*r.LastFetchedAt) > time.Minute {
		t.Errorf("last fetched = %v", r.LastFetchedAt)
	}
	if !r.GitHub.Detected || r.GitHub.Repo != "acme/app" || r.GitHub.Remote != "origin" {
		t.Errorf("github = %+v", r.GitHub)
	}
	if r.Sync == nil || r.Sync.Upstream.State != domain.UpstreamInSync || r.Sync.NotPushed.Total != 0 || r.Sync.NotPulled.Total != 0 {
		t.Errorf("sync = %+v", r.Sync)
	}
	if main := mustBranch(t, o, "main"); !main.Target || !main.Head || !main.Protected || main.VsTarget.Relation != domain.RelTarget || len(main.Attention) != 0 {
		t.Errorf("main = %+v", main)
	}
}

func TestOverviewWorkingTreeStagedUnstagedUntracked(t *testing.T) {
	f := newGC(t)
	commitIn(t, f.repo, "tracked.txt", "v1\n", "tracked")
	writeFile(t, f.repo, "tracked.txt", "v2\n")
	writeFile(t, f.repo, "staged.txt", "s\n")
	git(t, f.repo, "add", "staged.txt")
	writeFile(t, f.repo, "untracked.txt", "u\n")

	wt := f.overview(t).Local.WorkingTree
	if wt.Clean || wt.Counts.Staged != 1 || wt.Counts.Unstaged != 1 || wt.Counts.Untracked != 1 ||
		wt.Staged[0].Path != "staged.txt" || wt.Unstaged[0].Path != "tracked.txt" || wt.Untracked[0].Path != "untracked.txt" {
		t.Errorf("working tree = %+v", wt)
	}
	// The same through the working-changes endpoint, with a diff per kind.
	c, err := f.gc.WorkingTree(f.ctx, f.project.ID, "")
	if err != nil || !c.Primary || c.Tree.Counts.Total() != 3 {
		t.Fatalf("changes = %+v, %v", c, err)
	}
	for kind, path := range map[string]string{"staged": "staged.txt", "unstaged": "tracked.txt", "untracked": "untracked.txt"} {
		d, err := f.gc.WorkingDiff(f.ctx, f.project.ID, "", []string{path}, kind, 0, 0)
		if err != nil || d.Additions != 1 || d.Diff == "" {
			t.Errorf("%s diff = %+v, %v", kind, d, err)
		}
	}
}

func TestLocalAheadBehindAndDivergenceAgainstUpstreamAndTarget(t *testing.T) {
	f := newGC(t)
	// Another clone moves origin/main on by two commits.
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	commitIn(t, other, "r1.txt", "1\n", "remote 1")
	commitIn(t, other, "r2.txt", "2\n", "remote 2")
	git(t, other, "push", "-q", "origin", "main")
	if _, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	o := f.overview(t)
	if up := o.Local.Target.Upstream; up.State != domain.UpstreamBehind || up.Behind != 2 || up.Ahead != 0 {
		t.Errorf("main vs origin/main = %+v", up)
	}
	if o.Remote.Sync.NotPulled.Total != 2 || o.Remote.Sync.NotPulled.Items[0].Subject != "remote 2" || o.Remote.Sync.NotPushed.Total != 0 {
		t.Errorf("not pulled = %+v", o.Remote.Sync)
	}

	// Now a local commit as well: main diverges from origin/main.
	commitIn(t, f.repo, "l1.txt", "l\n", "local 1")
	o = f.overview(t)
	if up := o.Local.Target.Upstream; up.State != domain.UpstreamDiverged || up.Ahead != 1 || up.Behind != 2 {
		t.Errorf("diverged main = %+v", up)
	}
	if o.Remote.Sync.NotPushed.Total != 1 || o.Remote.Sync.NotPushed.Items[0].Subject != "local 1" {
		t.Errorf("not pushed = %+v", o.Remote.Sync.NotPushed)
	}

	// A Dev Board branch cut from the old main is diverged from the target, and
	// one that has not diverged is merely ahead.
	oldMain := gitOut(t, f.repo, "rev-parse", "main~1")
	a := f.agent(t, "Diverging", 1)
	git(t, a.path, "reset", "-q", "--hard", oldMain) // test setup only: rewind the branch
	commitIn(t, a.path, "d.txt", "d\n", "diverging work")
	o = f.overview(t)
	b := mustBranch(t, o, a.branch)
	if b.VsTarget.Relation != domain.RelDiverged || b.VsTarget.Ahead != 1 || b.VsTarget.Behind != 1 {
		t.Errorf("diverging branch = %+v", b.VsTarget)
	}
	// Finished work of Dev Board's own is to be reviewed even though the target moved on...
	if !hasAttention(b, domain.AttentionReview) || hasAttention(b, domain.AttentionDiverged) || !strings.Contains(b.Attention[0].Message, "moved on by 1") {
		t.Errorf("attention = %+v", b.Attention)
	}
	// ...but a diverged branch that is not Dev Board's is only flagged as diverged.
	git(t, f.repo, "branch", "users-diverged", oldMain)
	git(t, f.repo, "checkout", "-q", "users-diverged")
	commitIn(t, f.repo, "u.txt", "u\n", "user work")
	git(t, f.repo, "checkout", "-q", "main")
	if ub := mustBranch(t, f.overview(t), "users-diverged"); !hasAttention(ub, domain.AttentionDiverged) || hasAttention(ub, domain.AttentionReview) {
		t.Errorf("a user's diverged branch: %+v", ub.Attention)
	}
}

func hasAttention(b *domain.GitBranch, kind string) bool {
	for _, a := range b.Attention {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

func TestBranchStatesMergedAheadUpstreamAndOwnership(t *testing.T) {
	f := newGC(t)
	merged := f.agent(t, "Merged work", 1)
	// Merge one branch into main directly (test setup). The others are cut afterwards,
	// from the new main, so they are ahead of it and not diverged.
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge", merged.branch)
	ahead := f.agent(t, "Ahead work", 2)
	empty := f.agent(t, "Nothing yet", 0)
	git(t, ahead.path, "push", "-q", "-u", "origin", ahead.branch)
	commitIn(t, ahead.path, "more.txt", "m\n", "unpushed follow-up")
	// A branch of the user's own, and one that only has Dev Board's name.
	git(t, f.repo, "branch", "my-experiment", "main")
	git(t, f.repo, "branch", "devboard/impostor-000000", "main")

	o := f.overview(t)

	a := mustBranch(t, o, ahead.branch)
	if a.VsTarget.Relation != domain.RelAhead || a.VsTarget.Ahead != 3 || a.Merged || a.Scope != domain.ScopeLocal {
		t.Errorf("ahead = %+v", a.VsTarget)
	}
	if a.Upstream.State != domain.UpstreamAhead || a.Upstream.Ahead != 1 || a.NotPushed != 1 || a.Upstream.Name != "origin/"+ahead.branch {
		t.Errorf("upstream = %+v notPushed=%d", a.Upstream, a.NotPushed)
	}
	if !a.DevBoard.Created || !a.DevBoard.Namespace || a.DevBoard.TaskID != ahead.task.ID || a.DevBoard.TaskTitle != "Ahead work" ||
		a.DevBoard.RunID != ahead.run.ID || a.DevBoard.RunState != domain.RunCompleted || a.DevBoard.Phase != domain.PhaseReview || a.DevBoard.ActiveRun {
		t.Errorf("ownership = %+v", a.DevBoard)
	}
	if a.Worktree == nil || !a.Worktree.Owned || a.Worktree.WorktreeID != ahead.wt.ID || a.Worktree.Path != ahead.path || a.Worktree.Primary {
		t.Errorf("worktree = %+v", a.Worktree)
	}
	if a.Worktree.Dirty == nil || a.Worktree.Dirty.Dirty() {
		t.Errorf("a clean worktree reported dirty: %+v", a.Worktree.Dirty)
	}
	if !hasAttention(a, domain.AttentionReview) || !hasAttention(a, domain.AttentionUnpushed) {
		t.Errorf("attention = %+v", a.Attention)
	}

	m := mustBranch(t, o, merged.branch)
	if !m.Merged || m.VsTarget.Relation != domain.RelMerged || m.VsTarget.Behind != 1 {
		t.Errorf("merged = %+v", m.VsTarget)
	}
	if m.Upstream.State != domain.UpstreamNone || !hasAttention(m, domain.AttentionCleanup) {
		t.Errorf("merged branch: upstream %+v attention %+v", m.Upstream, m.Attention)
	}
	e := mustBranch(t, o, empty.branch)
	if e.VsTarget.Relation != domain.RelSame || !e.Merged || e.VsTarget.Ahead != 0 {
		t.Errorf("empty = %+v", e.VsTarget)
	}

	mine := mustBranch(t, o, "my-experiment")
	if mine.DevBoard.Created || mine.DevBoard.Namespace || mine.DevBoard.Phase != domain.PhaseNone || mine.Worktree != nil {
		t.Errorf("a user's branch = %+v", mine.DevBoard)
	}
	imp := mustBranch(t, o, "devboard/impostor-000000")
	if imp.DevBoard.Created || !imp.DevBoard.Namespace {
		t.Errorf("a branch that only has the name = %+v: a name alone must not count as created by Dev Board", imp.DevBoard)
	}

	// Dev Board's branches come first, and the merged one sorts with its cleanup reason.
	var firstNonOwned int = -1
	for i, b := range o.Branches {
		if !b.DevBoard.Created && firstNonOwned < 0 {
			firstNonOwned = i
		}
		if b.DevBoard.Created && firstNonOwned >= 0 {
			t.Errorf("owned branch %s is listed after non-owned %s", b.Name, o.Branches[firstNonOwned].Name)
		}
	}
	if o.Summary.DevBoard != 3 || o.Summary.Cleanup != 1 || o.Summary.Mergeable != 1 || o.Summary.Worktrees != 3 {
		t.Errorf("summary = %+v", o.Summary)
	}
}

// A branch that never got a commit is not "merged" just because the target moved past it, and a
// branch that really was merged is.
func TestBranchWithNoCommitsOfItsOwnIsBehindNotMerged(t *testing.T) {
	f := newGC(t)
	untouched := f.agent(t, "Never committed", 0)
	merged := f.agent(t, "Really merged", 1)
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge", merged.branch)
	o := f.overview(t)

	u := mustBranch(t, o, untouched.branch)
	if u.VsTarget.Relation != domain.RelBehind || u.VsTarget.Behind != 2 || u.VsTarget.Ahead != 0 {
		t.Errorf("untouched = %+v", u.VsTarget)
	}
	if !u.Merged {
		t.Error("it can still be deleted by ancestry: nothing on it would be lost")
	}
	if !hasAttention(u, domain.AttentionCleanup) || strings.Contains(u.Attention[0].Message, "merged") {
		t.Errorf("attention = %+v: must not claim it was merged", u.Attention)
	}
	if m := mustBranch(t, o, merged.branch); m.VsTarget.Relation != domain.RelMerged {
		t.Errorf("merged = %+v", m.VsTarget)
	}
	if o.Summary.Cleanup != 1 {
		t.Errorf("only the branch that was merged is a cleanup: %+v", o.Summary)
	}
}

func TestTaskPhaseFollowsTheTaskAndItsRuns(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Phase work", 1)
	phase := func() domain.TaskPhase {
		return mustBranch(t, f.overview(t), a.branch).DevBoard.Phase
	}
	if phase() != domain.PhaseReview {
		t.Errorf("review: %s", phase())
	}
	a.moveTask(t, f, domain.TaskDone)
	if phase() != domain.PhaseCompleted {
		t.Errorf("done: %s", phase())
	}
	a.moveTask(t, f, domain.TaskBacklog)
	if phase() != domain.PhaseIdle {
		t.Errorf("backlog: %s", phase())
	}
	// A new run starts: the branch is active whatever column the card is in.
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: a.task.ID, AgentID: "fake", Prompt: "again", WorktreeID: a.wt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	b := mustBranch(t, f.overview(t), a.branch)
	if b.DevBoard.Phase != domain.PhaseActive || !b.DevBoard.ActiveRun || b.DevBoard.RunID != run.ID || b.DevBoard.RunState != domain.RunRunning {
		t.Errorf("active = %+v", b.DevBoard)
	}
}

func TestDirtyWorktreeIsShownOnItsBranch(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Dirty tree", 0)
	writeFile(t, a.path, "uncommitted.txt", "wip\n")
	writeFile(t, a.path, "README.md", "edited\n")

	o := f.overview(t)
	b := mustBranch(t, o, a.branch)
	if b.Worktree == nil || b.Worktree.Dirty == nil || b.Worktree.Dirty.Untracked != 1 || b.Worktree.Dirty.Unstaged != 1 || !hasAttention(b, domain.AttentionDirty) {
		t.Errorf("dirty branch = %+v attention=%+v", b.Worktree, b.Attention)
	}
	if o.Summary.DirtyTrees != 1 {
		t.Errorf("summary = %+v", o.Summary)
	}
	// Its working changes are reachable through the worktree, and only through its record.
	c, err := f.gc.WorkingTree(f.ctx, f.project.ID, a.wt.ID)
	if err != nil || c.Primary || !c.Owned || c.TaskID != a.task.ID || c.Tree.Counts.Untracked != 1 {
		t.Errorf("changes = %+v, %v", c, err)
	}
	if _, err := f.gc.WorkingTree(f.ctx, f.project.ID, "wt_doesnotexist"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown worktree: %v", err)
	}
	// A worktree of another project is not found, as if it did not exist.
	other := filepath.Join(t.TempDir(), "other")
	initRepo(t, other)
	op, err := f.projects.Register(f.ctx, other, "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gc.WorkingTree(f.ctx, op.ID, a.wt.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another project's worktree: %v", err)
	}
}

func TestOverviewDetachedHeadAndNoRemote(t *testing.T) {
	f := newGC(t)
	git(t, f.repo, "checkout", "-q", "--detach")
	o := f.overview(t)
	if !o.Local.Head.Detached || o.Local.Head.Branch != "" || o.Remote.Sync != nil {
		t.Errorf("head = %+v sync=%+v", o.Local.Head, o.Remote.Sync)
	}
	if !containsNote(o.Notes, "detached") {
		t.Errorf("notes = %v", o.Notes)
	}
	// main is no longer checked out anywhere, and is not protected only by being HEAD.
	if b := mustBranch(t, o, "main"); !b.Protected || o.Local.Target.CheckedOut != "" {
		t.Errorf("main = %+v target = %+v", b, o.Local.Target)
	}

	// A repository with no remote at all.
	g := newGCOn(t, false)
	o = g.overview(t)
	if len(o.Remote.Remotes) != 0 || o.Remote.LastFetchedAt != nil || o.Remote.GitHub.Detected || o.Remote.Sync == nil || o.Remote.Sync.Upstream.State != domain.UpstreamNone {
		t.Errorf("remote = %+v", o.Remote)
	}
	if !containsNote(o.Notes, "no remote") {
		t.Errorf("notes = %v", o.Notes)
	}
	res, err := g.gc.Fetch(g.ctx, g.project.ID)
	if err != nil || res.Outcome != domain.OutcomeNoop {
		t.Errorf("fetch without a remote = %+v, %v", res, err)
	}
}

func containsNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n), sub) {
			return true
		}
	}
	return false
}

// newGCOn is newGC, optionally without any remote.
func newGCOn(t *testing.T, withRemote bool) *gcFixture {
	t.Helper()
	if withRemote {
		return newGC(t)
	}
	isolate(t)
	f := newFixture(t)
	g := &gitrepo.CLI{}
	f.projects.Git = g
	base, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(base, "solo")
	wtRoot := filepath.Join(base, "worktrees")
	for _, d := range []string{repo, wtRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "init", "-q", "-b", "main")
	commitIn(t, repo, "README.md", "hello\n", "init")
	p, err := f.projects.Register(context.Background(), repo, "solo")
	if err != nil {
		t.Fatal(err)
	}
	wts := &Worktrees{Deps: f.deps, Root: wtRoot}
	return &gcFixture{fixture: f, gc: &GitControl{Deps: f.deps, Git: g, Worktrees: wts}, git: g, repo: repo, wtRoot: wtRoot, project: p, wts: wts, ctx: context.Background()}
}

func TestUpstreamGoneAndMissing(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Gone upstream", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	b := f.agent(t, "No upstream", 1)
	// The branch is deleted on the remote and the fetch prunes the tracking ref.
	git(t, f.remote, "branch", "-D", a.branch)
	if res, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil || res.Outcome != domain.OutcomeDone {
		t.Fatalf("fetch = %+v, %v", res, err)
	}
	o := f.overview(t)
	if g := mustBranch(t, o, a.branch); g.Upstream.State != domain.UpstreamGone || !hasAttention(g, domain.AttentionGone) {
		t.Errorf("gone = %+v attention %+v", g.Upstream, g.Attention)
	}
	nu := mustBranch(t, o, b.branch)
	if nu.Upstream.State != domain.UpstreamNone || nu.NotPushed != 1 || !hasAttention(nu, domain.AttentionNoRemote) {
		t.Errorf("none = %+v notPushed=%d attention=%+v", nu.Upstream, nu.NotPushed, nu.Attention)
	}
}

func TestStaleBranch(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Old work", 1)
	// Age the branch's only commit by 30 days, and move main on.
	old := time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "-C", a.path, "commit", "-q", "--amend", "--no-edit", "--date", old)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	commitIn(t, f.repo, "later.txt", "x\n", "main moved on")

	b := mustBranch(t, f.overview(t), a.branch)
	if !b.Stale || b.StaleWhy == "" || !hasAttention(b, domain.AttentionStale) {
		t.Errorf("stale = %v %q %+v", b.Stale, b.StaleWhy, b.Attention)
	}
	// A fresh branch behind main is not stale.
	fresh := f.agent(t, "Fresh work", 1)
	commitIn(t, f.repo, "later2.txt", "y\n", "main moved again")
	if mustBranch(t, f.overview(t), fresh.branch).Stale {
		t.Error("a branch committed to a moment ago is not stale")
	}
}

func TestRemoteOnlyBranchesAreListedAsRemote(t *testing.T) {
	f := newGC(t)
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	git(t, other, "checkout", "-q", "-b", "colleague-work")
	commitIn(t, other, "c.txt", "c\n", "colleague")
	git(t, other, "push", "-q", "origin", "colleague-work")
	if _, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	o := f.overview(t)
	var rb *domain.GitBranch
	for i := range o.Branches {
		if o.Branches[i].Scope == domain.ScopeRemote && o.Branches[i].Name == "origin/colleague-work" {
			rb = &o.Branches[i]
		}
	}
	if rb == nil || rb.Remote != "origin" || rb.VsTarget.Relation != domain.RelAhead || rb.LocalName != "" || rb.DevBoard.Created {
		t.Fatalf("remote branch = %+v", rb)
	}
	for _, b := range o.Branches {
		if b.Scope == domain.ScopeRemote && b.Name == "origin/main" {
			t.Error("origin/main is tracked by main and must not be listed again as remote-only")
		}
	}
	// And it can be compared, as a remote branch.
	cmp, err := f.gc.Compare(f.ctx, f.project.ID, domain.ScopeRemote, "origin/colleague-work", "", 0, 0)
	if err != nil || cmp.Ahead != 1 || cmp.FilesTotal != 1 {
		t.Errorf("compare = %+v, %v", cmp, err)
	}
}

func TestGitCommandFailureIsAnErrorNotEmptyData(t *testing.T) {
	f := newGC(t)
	bin := failingGit(t, "for-each-ref")
	f.gc.Git = &gitrepo.CLI{Binary: bin}
	_, err := f.gc.Overview(f.ctx, f.project.ID)
	if err == nil || !errors.Is(err, domain.ErrGit) || !strings.Contains(err.Error(), "simulated failure") {
		t.Fatalf("Overview with a failing git: %v", err)
	}
}

// failingGit is a git executable that fails for the named subcommands.
func failingGit(t *testing.T, failOn ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	var cases strings.Builder
	for _, s := range failOn {
		cases.WriteString("    " + s + ") echo 'fatal: simulated failure' >&2; exit 1;;\n")
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  case \"$a\" in\n" + cases.String() + "  esac\ndone\nexec " + real + " \"$@\"\n"
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A branch name that could be taken for something else, or for an option, is
// listed, flagged, and refused by every action. Measurements use commit IDs, so
// the name is never put on a command line.
func TestUnexpectedBranchNamesAreListedButNeverActedOn(t *testing.T) {
	f := newGC(t)
	tip := gitOut(t, f.repo, "rev-parse", "main")
	for _, ref := range []string{"refs/heads/-dash", "refs/heads/--upload-pack=touch-pwned", "refs/heads/HEAD", "refs/heads/ünïcode-ブランチ", "refs/heads/with.lock.ok", "refs/heads/a@b"} {
		git(t, f.repo, "update-ref", ref, tip)
	}
	o := f.overview(t)
	for _, name := range []string{"-dash", "--upload-pack=touch-pwned", "HEAD"} {
		b := branchNamed(o, name)
		if b == nil {
			t.Errorf("%q is missing from the overview", name)
			continue
		}
		if b.Unusual == "" {
			t.Errorf("%q must be flagged unusual", name)
		}
	}
	if b := branchNamed(o, "ünïcode-ブランチ"); b == nil || b.Unusual != "" {
		t.Errorf("a unicode name is fine: %+v", b)
	}
	pwned := filepath.Join(t.TempDir(), "pwned")
	for _, name := range []string{"-dash", "--upload-pack=touch-pwned", "HEAD", "-D", "a..b"} {
		sha := tip
		if res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: name, ExpectedSha: sha}); err != nil || res.OK || res.Outcome != domain.OutcomeRefused {
			t.Errorf("push %q = %+v, %v", name, res, err)
		}
		if res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, DeleteInput{Branch: name, BranchSha: sha}); err != nil || res.OK {
			t.Errorf("delete %q = %+v, %v", name, res, err)
		}
		if plan, err := f.gc.MergePlan(f.ctx, f.project.ID, MergeInput{Branch: name}); err != nil || plan.CanMerge {
			t.Errorf("merge plan %q = %+v, %v", name, plan, err)
		}
		if _, err := f.gc.Compare(f.ctx, f.project.ID, "", name, "", 0, 0); !errors.Is(err, domain.ErrInvalid) && !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("compare %q: %v", name, err)
		}
	}
	if exists(pwned) {
		t.Fatal("a branch name was executed")
	}
	// The odd branches are all still there.
	for _, name := range []string{"-dash", "--upload-pack=touch-pwned", "HEAD"} {
		if gitOut(t, f.repo, "rev-parse", "refs/heads/"+name) != tip {
			t.Errorf("branch %q was changed or deleted", name)
		}
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// ---- comparison ----

func TestCompareBranchReportsFilesCountsAndCommits(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Compare me", 0)
	commitIn(t, a.path, "added.txt", "one\ntwo\nthree\n", "add a file")
	commitIn(t, a.path, "README.md", "hello\nmore\n", "edit readme")
	// The target moves on after the branch left it.
	commitIn(t, f.repo, "target-only.txt", "t\n", "target change")

	cmp, err := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Target != "main" || cmp.Ahead != 2 || cmp.Behind != 1 || cmp.Relation != domain.RelDiverged || cmp.MergeBase == "" {
		t.Errorf("comparison = %+v", cmp)
	}
	if cmp.FilesTotal != 2 || cmp.Additions != 4 || cmp.Deletions != 0 || len(cmp.Files) != 2 {
		t.Errorf("files = %+v (+%d -%d of %d)", cmp.Files, cmp.Additions, cmp.Deletions, cmp.FilesTotal)
	}
	for _, f := range cmp.Files {
		if f.Path == "target-only.txt" {
			t.Error("a change made on the target after the branch left it is not part of the branch's diff")
		}
	}
	if cmp.Unique.Total != 2 || cmp.Unique.Items[0].Subject != "edit readme" || cmp.Missing.Total != 1 || cmp.Missing.Items[0].Subject != "target change" {
		t.Errorf("unique = %+v missing = %+v", cmp.Unique, cmp.Missing)
	}
	if !strings.Contains(cmp.Basis, "since it left main") {
		t.Errorf("basis = %q", cmp.Basis)
	}

	// Drill down: the file's diff, pinned to the commits the comparison reported.
	d, err := f.gc.FileDiff(f.ctx, f.project.ID, cmp.MergeBase, cmp.BranchSha, []string{"added.txt"}, 0, 0)
	if err != nil || d.Additions != 3 || !strings.Contains(d.Diff, "+two") || d.HasMore {
		t.Errorf("file diff = %+v, %v", d, err)
	}
	whole, err := f.gc.FileDiff(f.ctx, f.project.ID, cmp.MergeBase, cmp.BranchSha, nil, 0, 0)
	if err != nil || !strings.Contains(whole.Diff, "added.txt") || !strings.Contains(whole.Diff, "README.md") || strings.Contains(whole.Diff, "target-only") {
		t.Errorf("unified diff = %+v, %v", whole, err)
	}
	// A comparison against another target.
	git(t, f.repo, "branch", "other-base", "main~0")
	if c2, err := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "other-base", 0, 0); err != nil || c2.Target != "other-base" {
		t.Errorf("explicit target = %+v, %v", c2, err)
	}
	if _, err := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "nope", 0, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown target: %v", err)
	}
	if _, err := f.gc.Compare(f.ctx, f.project.ID, "", "no-such-branch", "", 0, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown branch: %v", err)
	}
	if _, err := f.gc.FileDiff(f.ctx, f.project.ID, "main", cmp.BranchSha, nil, 0, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a name in place of a commit ID: %v", err)
	}
	if _, err := f.gc.FileDiff(f.ctx, f.project.ID, strings.Repeat("a", 40), cmp.BranchSha, nil, 0, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown commit: %v", err)
	}
}

// Many files: the list is paged, the totals cover all of them, and a huge file's
// diff comes in windows. Nothing is rendered whole.
func TestCompareLargeBranchIsPaged(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Big change", 0)
	for i := 0; i < 230; i++ {
		writeFile(t, a.path, filepath.Join("gen", "file"+leftPad(i)+".txt"), "line\nline\n")
	}
	var big strings.Builder
	for i := 0; i < 3000; i++ {
		big.WriteString("generated line\n")
	}
	writeFile(t, a.path, "big.txt", big.String())
	git(t, a.path, "add", "-A")
	git(t, a.path, "commit", "-q", "-m", "lots")

	first, err := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if first.FilesTotal != 231 || len(first.Files) != 50 || first.Offset != 0 || first.Additions != 230*2+3000 {
		t.Errorf("first page: total=%d len=%d +%d", first.FilesTotal, len(first.Files), first.Additions)
	}
	last, _ := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "", 200, 50)
	if len(last.Files) != 31 || last.Offset != 200 {
		t.Errorf("last page len=%d", len(last.Files))
	}
	beyond, _ := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "", 5000, 50)
	if len(beyond.Files) != 0 || beyond.Files == nil {
		t.Errorf("past the end = %+v", beyond.Files)
	}
	capped, _ := f.gc.Compare(f.ctx, f.project.ID, "", a.branch, "", 0, 100000)
	if len(capped.Files) != MaxFilePage {
		t.Errorf("the page size must be capped at %d, got %d", MaxFilePage, len(capped.Files))
	}

	d, err := f.gc.FileDiff(f.ctx, f.project.ID, first.MergeBase, first.BranchSha, []string{"big.txt"}, 0, 200)
	if err != nil || d.Lines != 200 || !d.HasMore || d.Additions != 3000 {
		t.Fatalf("window = %+v, %v", d, err)
	}
	next, _ := f.gc.FileDiff(f.ctx, f.project.ID, first.MergeBase, first.BranchSha, []string{"big.txt"}, 200, 5000)
	if next.Lines > gitrepo.MaxDiffLines || !next.HasMore || next.Offset != 200 {
		t.Errorf("clamped window lines=%d more=%v", next.Lines, next.HasMore)
	}
}

func leftPad(i int) string {
	s := "000" + string(rune('0'+i/100)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
	return s[len(s)-4:]
}

func TestCommitsPaging(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "History", 5)
	p1, err := f.gc.Commits(f.ctx, f.project.ID, "", a.branch, 0, 3)
	if err != nil || len(p1.Items) != 3 || !p1.Truncated {
		t.Fatalf("p1 = %+v, %v", p1, err)
	}
	p2, _ := f.gc.Commits(f.ctx, f.project.ID, "", a.branch, 3, 3)
	if len(p2.Items) != 3 || p2.Truncated || p2.Items[2].Subject != "init" {
		t.Errorf("p2 = %+v", p2)
	}
	head, err := f.gc.Commits(f.ctx, f.project.ID, "", "", 0, 5)
	if err != nil || len(head.Items) != 1 {
		t.Errorf("HEAD history = %+v, %v", head, err)
	}
}

// ---- GitHub ----

// fakeGH is a gh stand-in: it prints list.json for `pr list`, view.json for `pr
// view`, a URL for `pr create`, and records every call.
type ghFake struct {
	dir string
	cli *github.CLI
}

func newFakeGH(t *testing.T) *ghFake {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> '` + dir + `/calls.log'
if [ "$1" = auth ] && [ -f '` + dir + `/authfail' ]; then exit 1; fi
if [ -f '` + dir + `/fail' ]; then cat '` + dir + `/fail' >&2; exit 1; fi
case "$1 $2" in
  "pr list") cat '` + dir + `/list.json';;
  "pr view") cat '` + dir + `/view.json';;
  "pr create") cat '` + dir + `/create.out';;
  "auth status") exit 0;;
esac
`
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &ghFake{dir: dir, cli: &github.CLI{Binary: path}}
	g.set(t, "list.json", "[]")
	g.set(t, "view.json", "{}")
	g.set(t, "create.out", "https://github.com/acme/app/pull/1\n")
	return g
}

func (g *ghFake) set(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (g *ghFake) failWith(t *testing.T, stderr string) { g.set(t, "fail", stderr) }

func (g *ghFake) calls(t *testing.T) string {
	b, _ := os.ReadFile(filepath.Join(g.dir, "calls.log"))
	return string(b)
}

func prJSON(number int, state, head, headSha string) string {
	return `{"number":` + string(rune('0'+number)) + `,"title":"PR title","url":"https://github.com/acme/app/pull/` + string(rune('0'+number)) + `","state":"` + state +
		`","isDraft":false,"headRefName":"` + head + `","baseRefName":"main","headRefOid":"` + headSha + `","author":{"login":"me"},"reviewDecision":"","mergeable":"MERGEABLE","statusCheckRollup":[],"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","mergedAt":null,"closedAt":null,"isCrossRepository":false}`
}

func TestPullRequestsAreAskedSeparatelyAndJoinedToTasks(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Has a PR", 1)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	gh.set(t, "list.json", "["+prJSON(4, "OPEN", a.branch, "abc")+","+prJSON(3, "MERGED", "somebody-else", "def")+"]")

	st, err := f.gc.PullRequests(f.ctx, f.project.ID)
	if err != nil || !st.Available || st.Repo != "acme/app" || len(st.PullRequests) != 2 {
		t.Fatalf("state = %+v, %v", st, err)
	}
	pr := st.PullRequests[0]
	if pr.TaskID != a.task.ID || pr.TaskTitle != "Has a PR" || pr.RunID != a.run.ID || pr.State != "open" {
		t.Errorf("pr = %+v", pr)
	}
	if other := st.PullRequests[1]; other.TaskID != "" || other.State != "merged" {
		t.Errorf("a pull request from a branch Dev Board does not know is unassociated: %+v", other)
	}
	if !strings.Contains(gh.calls(t), "-R acme/app") {
		t.Errorf("gh calls: %s", gh.calls(t))
	}
	// The local overview did not need GitHub: it works the same with it failing.
	gh.failWith(t, "error connecting to api.github.com")
	if o := f.overview(t); len(o.Branches) == 0 {
		t.Error("the local overview must not depend on GitHub")
	}
}

func TestGitHubUnavailableReasons(t *testing.T) {
	f := newGC(t)
	ask := func() *domain.GitHubState {
		st, err := f.gc.PullRequests(f.ctx, f.project.ID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Available || len(st.PullRequests) != 0 || st.PullRequests == nil {
			t.Fatalf("unavailable state must be empty and say why: %+v", st)
		}
		return st
	}

	f.gc.GitHub = nil
	if st := ask(); st.Reason != domain.GHMissing {
		t.Errorf("no client: %+v", st)
	}
	f.gc.GitHub = &github.CLI{Binary: filepath.Join(t.TempDir(), "no-gh")}
	if st := ask(); st.Reason != domain.GHMissing || !strings.Contains(st.Message, "not installed") {
		t.Errorf("gh missing: %+v", st)
	}
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	gh.failWith(t, "To get started with GitHub CLI, please run:  gh auth login")
	if st := ask(); st.Reason != domain.GHUnauthenticated {
		t.Errorf("signed out: %+v", st)
	}
	gh.failWith(t, "error connecting to api.github.com")
	if st := ask(); st.Reason != domain.GHError {
		t.Errorf("offline: %+v", st)
	}

	// Not a GitHub remote.
	git(t, f.repo, "remote", "set-url", "origin", "git@gitlab.example.com:acme/app.git")
	if err := os.Remove(filepath.Join(gh.dir, "fail")); err != nil {
		t.Fatal(err)
	}
	gh.set(t, "authfail", "x")
	if st := ask(); st.Reason != domain.GHNotGitHub {
		t.Errorf("gitlab: %+v", st)
	}
	// No remote at all.
	g := newGCOn(t, false)
	g.gc.GitHub = gh.cli
	st, err := g.gc.PullRequests(g.ctx, g.project.ID)
	if err != nil || st.Available || st.Reason != domain.GHNoRemote {
		t.Errorf("no remote: %+v, %v", st, err)
	}
}

// execGit runs git in dir and returns its trimmed output, for assertions that
// must not fail the test when git exits non-zero.
func execGit(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func execGitErr(dir string, args ...string) error {
	_, err := execGit(dir, args...)
	return err
}

// A repository with no commits yet still has an overview: no target, no branches, and a note.
func TestOverviewOfARepositoryWithNoCommits(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	real := &gitrepo.CLI{}
	f.projects.Git = real
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, repo, "new.txt", "x\n")
	p, err := f.projects.Register(context.Background(), repo, "empty")
	if err != nil {
		t.Fatal(err)
	}
	gc := &GitControl{Deps: f.deps, Git: real, Worktrees: &Worktrees{Deps: f.deps, Root: filepath.Join(repo, "..", "wt")}}
	o, err := gc.Overview(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Local.Head.Unborn || o.Local.Target.Name != "" || len(o.Branches) != 0 || o.Local.WorkingTree.Counts.Untracked != 1 || !containsNote(o.Notes, "no target") {
		t.Errorf("overview = head %+v target %+v branches %d notes %v", o.Local.Head, o.Local.Target, len(o.Branches), o.Notes)
	}
	plan, err := gc.MergePlan(context.Background(), p.ID, MergeInput{Branch: "x"})
	if err != nil || plan.CanMerge {
		t.Errorf("merge plan = %+v, %v", plan, err)
	}
}
