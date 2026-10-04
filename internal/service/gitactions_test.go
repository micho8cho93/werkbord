package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/store"
)

// gitEvents lists the git.* events recorded so far: the audit trail. An action
// that did not happen must leave none.
func (f *gcFixture) gitEvents(t *testing.T) []domain.Event {
	t.Helper()
	var out []domain.Event
	err := f.deps.Store.View(f.ctx, func(tx store.Tx) error {
		evs, err := tx.Events().ListAfter(f.ctx, 0, 10000)
		for _, e := range evs {
			if strings.HasPrefix(string(e.Type), "git.") {
				out = append(out, e)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *gcFixture) sha(t *testing.T, dir, rev string) string {
	t.Helper()
	return gitOut(t, dir, "rev-parse", rev)
}

func (f *gcFixture) remoteSha(t *testing.T, branch string) string {
	t.Helper()
	out, err := execGit(f.remote, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return out
}

// ---- fetch ----

func TestFetchUpdatesOnlyRemoteTrackingRefs(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Fetch safe", 1)
	writeFile(t, a.path, "wip.txt", "uncommitted\n")
	mainBefore, branchBefore := f.sha(t, f.repo, "main"), f.sha(t, f.repo, a.branch)

	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	remoteTip := commitIn(t, other, "r.txt", "r\n", "remote work")
	git(t, other, "push", "-q", "origin", "main")

	res, err := f.gc.Fetch(f.ctx, f.project.ID)
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone || res.Remote == nil || !res.Remote.Verified {
		t.Fatalf("fetch = %+v, %v", res, err)
	}
	if f.sha(t, f.repo, "origin/main") != remoteTip {
		t.Error("the remote-tracking branch was not updated")
	}
	if f.sha(t, f.repo, "main") != mainBefore || f.sha(t, f.repo, a.branch) != branchBefore {
		t.Error("a fetch moved one of the user's own branches")
	}
	if b, _ := os.ReadFile(filepath.Join(a.path, "wip.txt")); string(b) != "uncommitted\n" {
		t.Error("a fetch touched a worktree")
	}
	if len(f.gitEvents(t)) != 1 {
		t.Errorf("events = %+v", f.gitEvents(t))
	}
}

func TestFetchReportsAnUnavailableRemote(t *testing.T) {
	f := newGC(t)
	if err := os.RemoveAll(f.remote); err != nil {
		t.Fatal(err)
	}
	res, err := f.gc.Fetch(f.ctx, f.project.ID)
	if err != nil || res.OK || res.Outcome != domain.OutcomeUnavailable || res.Remote == nil || res.Remote.Verified {
		t.Fatalf("fetch = %+v, %v", res, err)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("a fetch that failed must leave no event")
	}
	// The local overview is unaffected by the remote being gone.
	if o := f.overview(t); o.Local.Head.Branch != "main" {
		t.Errorf("overview = %+v", o.Local.Head)
	}
}

// ---- push ----

func TestPushNewBranchIsVerifiedWithTheRemote(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Push me", 2)
	tip := f.sha(t, f.repo, a.branch)

	res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: tip})
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if res.Remote == nil || !res.Remote.Verified || res.Remote.Sha != tip || res.Remote.Remote != "origin" {
		t.Errorf("remote effect = %+v", res.Remote)
	}
	if res.Local == nil || res.Local.After != tip || !strings.Contains(res.Local.Note, "remote-tracking") {
		t.Errorf("local effect = %+v", res.Local)
	}
	if f.remoteSha(t, a.branch) != tip {
		t.Error("the remote repository does not have the branch")
	}
	if up := gitOut(t, f.repo, "rev-parse", "--abbrev-ref", a.branch+"@{upstream}"); up != "origin/"+a.branch {
		t.Errorf("upstream = %s", up)
	}
	evs := f.gitEvents(t)
	if len(evs) != 1 || evs[0].Type != domain.EventGitPushed || evs[0].TaskID != a.task.ID || evs[0].RunID != a.run.ID {
		t.Errorf("events = %+v", evs)
	}
	// Pushing again with nothing new says so.
	again, _ := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: tip})
	if !again.OK || !strings.Contains(again.Message, "Nothing to push") {
		t.Errorf("second push = %+v", again)
	}
}

// The branch changed between the screen and the tap: nothing is pushed.
func TestPushRefusesABranchThatMovedSinceInspection(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Moving target", 1)
	seen := f.sha(t, f.repo, a.branch)
	commitIn(t, a.path, "late.txt", "l\n", "committed after the user looked")

	res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: seen})
	if err != nil || res.OK || res.Outcome != domain.OutcomeRefused || !hasBlocker(res.Blockers, domain.BlockBranchMoved) {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if f.remoteSha(t, a.branch) != "" || len(f.gitEvents(t)) != 0 {
		t.Error("something was pushed or recorded")
	}
	if res, _ := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch}); res.OK || !hasBlocker(res.Blockers, domain.BlockInvalidInput) {
		t.Errorf("a push that does not say what was reviewed must be refused: %+v", res)
	}
}

func TestPushNeverForces(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Rejected push", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	// Someone else pushes to the same branch.
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", "-b", a.branch, f.remote, other)
	theirs := commitIn(t, other, "theirs.txt", "t\n", "someone else's commit")
	git(t, other, "push", "-q", "origin", a.branch)
	mine := commitIn(t, a.path, "mine.txt", "m\n", "my commit")

	res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: mine})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Outcome != domain.OutcomeRejected || res.Remote == nil || res.Remote.Verified {
		t.Fatalf("push = %+v", res)
	}
	if !strings.Contains(res.Message, "never forces") {
		t.Errorf("message = %q", res.Message)
	}
	if f.remoteSha(t, a.branch) != theirs {
		t.Fatalf("the remote branch is %s, want their commit %s: the push was forced", f.remoteSha(t, a.branch), theirs)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("a rejected push must leave no event")
	}
}

func TestPushWithoutARemoteOrWithAnAmbiguousOne(t *testing.T) {
	g := newGCOn(t, false)
	a := g.agent(t, "Nowhere to go", 1)
	res, err := g.gc.Push(g.ctx, g.project.ID, PushInput{Branch: a.branch, ExpectedSha: g.sha(t, g.repo, a.branch)})
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockNoRemote) {
		t.Fatalf("no remote: %+v, %v", res, err)
	}

	f := newGC(t)
	b := f.agent(t, "Which one", 1)
	git(t, f.repo, "remote", "rename", "origin", "first")
	git(t, f.repo, "remote", "add", "second", f.remote)
	res, err = f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: b.branch, ExpectedSha: f.sha(t, f.repo, b.branch)})
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockAmbiguousRemote) {
		t.Fatalf("ambiguous: %+v, %v", res, err)
	}
	res, _ = f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: b.branch, ExpectedSha: f.sha(t, f.repo, b.branch), Remote: "second"})
	if !res.OK || res.Remote.Remote != "second" {
		t.Errorf("explicit remote: %+v", res)
	}
	res, _ = f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: b.branch, ExpectedSha: f.sha(t, f.repo, b.branch), Remote: "https://evil.example/x.git"})
	if res.OK || !hasBlocker(res.Blockers, domain.BlockInvalidInput) {
		t.Errorf("a URL is not a remote name: %+v", res)
	}
}

func TestPushRefusesAnUpstreamOfAnotherName(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Odd upstream", 1)
	git(t, a.path, "push", "-q", "origin", a.branch+":refs/heads/other-name")
	git(t, f.repo, "fetch", "-q", "origin")
	git(t, f.repo, "branch", "--set-upstream-to=origin/other-name", a.branch)
	res, _ := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: f.sha(t, f.repo, a.branch)})
	if res.OK || !hasBlocker(res.Blockers, domain.BlockRemoteDiffers) {
		t.Errorf("push = %+v", res)
	}
}

func TestPushWhenTheRemoteIsUnavailableOrGitFails(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Offline push", 1)
	tip := f.sha(t, f.repo, a.branch)

	// A failing git: reported as failed, with its message, and nothing recorded.
	good := f.gc.Git
	f.gc.Git = &gitrepo.CLI{Binary: failingGit(t, "push")}
	res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: tip})
	if err != nil || res.OK || res.Outcome != domain.OutcomeFailed || !strings.Contains(res.Git, "simulated failure") {
		t.Fatalf("push with a failing git = %+v, %v", res, err)
	}
	f.gc.Git = good

	// The remote vanishes.
	if err := os.RemoveAll(f.remote); err != nil {
		t.Fatal(err)
	}
	res, err = f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: tip})
	if err != nil || res.OK || res.Outcome != domain.OutcomeUnavailable || res.Remote == nil || res.Remote.Verified {
		t.Fatalf("push to an unavailable remote = %+v, %v", res, err)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("failed pushes must leave no event")
	}
}

// Git says the push worked but the remote cannot be asked: that is not reported as done.
func TestPushThatCannotBeConfirmedIsNotReportedAsDone(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Unconfirmed", 1)
	f.gc.Git = &gitrepo.CLI{Binary: failingGit(t, "ls-remote")}
	res, err := f.gc.Push(f.ctx, f.project.ID, PushInput{Branch: a.branch, ExpectedSha: f.sha(t, f.repo, a.branch)})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Outcome != domain.OutcomeUnverified || res.Remote == nil || res.Remote.Verified || len(res.Warnings) == 0 {
		t.Fatalf("push = %+v", res)
	}
	if res.Local == nil {
		t.Error("the local effect (tracking ref) should still be reported")
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("an unconfirmed push must not be recorded as done")
	}
}

// ---- merge ----

func mergeIn(a *agentBranch, f *gcFixture, t *testing.T) MergeInput {
	t.Helper()
	return MergeInput{Branch: a.branch, BranchSha: f.sha(t, f.repo, a.branch), Target: "main", TargetSha: f.sha(t, f.repo, "main")}
}

func TestMergePlanAndMergeSucceedLocallyOnly(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Mergeable", 2)
	writeFile(t, a.path, "uncommitted-in-branch-worktree.txt", "wip\n")
	in := mergeIn(a, f, t)
	remoteMain := f.remoteSha(t, "main")

	plan, err := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanMerge || len(plan.Blockers) != 0 || plan.Ahead != 2 || plan.Behind != 0 || !plan.FastForwardable || plan.FilesChanged != 2 ||
		plan.Conflicts.Method != domain.CheckSimulation || plan.Conflicts.Result != domain.ConflictClean || plan.TargetWorktree != "" && false {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "uncommitted") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
	if f.sha(t, f.repo, "main") != in.TargetSha {
		t.Fatal("planning changed main")
	}

	res, err := f.gc.Merge(f.ctx, f.project.ID, in)
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if res.Local == nil || res.Local.Before != in.TargetSha || res.Local.After != f.sha(t, f.repo, "main") || res.Remote != nil {
		t.Errorf("effects: local=%+v remote=%+v (a local merge has no remote effect)", res.Local, res.Remote)
	}
	if !strings.Contains(res.Message, "Nothing was pushed") {
		t.Errorf("message = %q", res.Message)
	}
	if err := execGitErr(f.repo, "merge-base", "--is-ancestor", in.BranchSha, "main"); err != nil {
		t.Error("the branch is not merged into main")
	}
	if f.remoteSha(t, "main") != remoteMain {
		t.Error("the merge was pushed")
	}
	if n := gitOut(t, f.repo, "rev-list", "--parents", "-n1", "main"); len(strings.Fields(n)) != 3 {
		t.Errorf("not a merge commit: %s", n)
	}
	if msg := gitOut(t, f.repo, "log", "-1", "--format=%B", "main"); !strings.Contains(msg, "Merge branch '"+a.branch+"' into main") || !strings.Contains(msg, "Task: Mergeable") {
		t.Errorf("merge message = %q", msg)
	}
	// The branch, its worktree and the uncommitted file in it are untouched.
	if f.sha(t, f.repo, a.branch) != in.BranchSha {
		t.Error("the branch moved")
	}
	if b, _ := os.ReadFile(filepath.Join(a.path, "uncommitted-in-branch-worktree.txt")); string(b) != "wip\n" {
		t.Error("the branch worktree was touched")
	}
	evs := f.gitEvents(t)
	if len(evs) != 1 || evs[0].Type != domain.EventGitMerged || evs[0].TaskID != a.task.ID {
		t.Errorf("events = %+v", evs)
	}
	// Afterwards the overview agrees, and the pull-request-style "merged" is by ancestry.
	if b := mustBranch(t, f.overview(t), a.branch); !b.Merged {
		t.Errorf("branch after merge = %+v", b.VsTarget)
	}
	// Merging again is refused as already merged.
	again, _ := f.gc.Merge(f.ctx, f.project.ID, MergeInput{Branch: a.branch, BranchSha: in.BranchSha, TargetSha: f.sha(t, f.repo, "main")})
	if again.OK || !hasBlocker(again.Blockers, domain.BlockAlreadyMerged) {
		t.Errorf("second merge = %+v", again)
	}
}

func TestMergeFastForwardOnly(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Fast forward", 1)
	in := mergeIn(a, f, t)
	in.Strategy = domain.MergeFastForward
	res, err := f.gc.Merge(f.ctx, f.project.ID, in)
	if err != nil || !res.OK || f.sha(t, f.repo, "main") != in.BranchSha {
		t.Fatalf("ff merge = %+v, %v", res, err)
	}
	if n := gitOut(t, f.repo, "rev-list", "--parents", "-n1", "main"); len(strings.Fields(n)) != 2 {
		t.Errorf("a fast-forward made a merge commit: %s", n)
	}

	// Once main has moved on, ff-only cannot, and says so.
	b := f.agent(t, "Cannot fast forward", 1)
	commitIn(t, f.repo, "moved.txt", "m\n", "main moved")
	in2 := mergeIn(b, f, t)
	in2.Strategy = domain.MergeFastForward
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, in2)
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockNotFastForward) {
		t.Errorf("plan = %+v", plan)
	}
	if _, err := f.gc.MergePlan(f.ctx, f.project.ID, MergeInput{Branch: b.branch, Strategy: "squash"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown strategy: %v", err)
	}
}

func TestMergeRefusesWhenEitherSideMovedSinceInspection(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Raced", 1)
	in := mergeIn(a, f, t)
	mainBefore := f.sha(t, f.repo, "main")

	// The branch gains a commit after the user reviewed it.
	commitIn(t, a.path, "after-review.txt", "x\n", "sneaked in after review")
	res, err := f.gc.Merge(f.ctx, f.project.ID, in)
	if err != nil || res.OK || res.Outcome != domain.OutcomeRefused || !hasBlocker(res.Blockers, domain.BlockBranchMoved) {
		t.Fatalf("merge after the branch moved = %+v, %v", res, err)
	}
	if f.sha(t, f.repo, "main") != mainBefore {
		t.Fatal("main moved")
	}

	// Review again; now the target moves.
	in = mergeIn(a, f, t)
	commitIn(t, f.repo, "main-moved.txt", "m\n", "main moved after review")
	res, _ = f.gc.Merge(f.ctx, f.project.ID, in)
	if res.OK || !hasBlocker(res.Blockers, domain.BlockTargetMoved) {
		t.Fatalf("merge after the target moved = %+v", res)
	}
	// Without the commits that were reviewed there is no merge at all.
	res, _ = f.gc.Merge(f.ctx, f.project.ID, MergeInput{Branch: a.branch})
	if res.OK || !hasBlocker(res.Blockers, domain.BlockInvalidInput) {
		t.Errorf("an unreviewed merge = %+v", res)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("refused merges must leave no event")
	}
}

// What is uncommitted in the checkout being merged into is never lost or mixed with.
func TestMergeNeverTouchesUncommittedWorkInTheTargetCheckout(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Careful", 1)
	in := mergeIn(a, f, t)

	for name, dirty := range map[string]func(){
		"unstaged": func() { writeFile(t, f.repo, "README.md", "my edit\n") },
		"staged": func() {
			writeFile(t, f.repo, "staged.txt", "s\n")
			git(t, f.repo, "add", "staged.txt")
		},
	} {
		dirty()
		plan, err := f.gc.MergePlan(f.ctx, f.project.ID, in)
		if err != nil || plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockDirty) {
			t.Fatalf("%s: plan = %+v, %v", name, plan, err)
		}
		res, _ := f.gc.Merge(f.ctx, f.project.ID, in)
		if res.OK || res.Outcome != domain.OutcomeRefused {
			t.Fatalf("%s: merge = %+v", name, res)
		}
		if f.sha(t, f.repo, "main") != in.TargetSha {
			t.Fatalf("%s: main moved", name)
		}
		git(t, f.repo, "reset", "-q", "--hard") // test cleanup
		_ = os.Remove(filepath.Join(f.repo, "staged.txt"))
	}

	// Untracked files do not matter, unless the merge would bring one of the same name.
	writeFile(t, f.repo, "scratch.txt", "mine\n")
	if plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, in); !plan.CanMerge {
		t.Errorf("an unrelated untracked file must not block: %+v", plan.Blockers)
	}
	clash := f.agent(t, "Clash", 0)
	commitIn(t, clash.path, "scratch.txt", "from the agent\n", "adds scratch.txt")
	cin := mergeIn(clash, f, t)
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, cin)
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockUntrackedClash) {
		t.Fatalf("clash plan = %+v", plan)
	}
	if res, _ := f.gc.Merge(f.ctx, f.project.ID, cin); res.OK {
		t.Fatal("merged over an untracked file")
	}
	if b, _ := os.ReadFile(filepath.Join(f.repo, "scratch.txt")); string(b) != "mine\n" {
		t.Errorf("the untracked file was overwritten: %q", b)
	}
	// A directory of untracked files that the branch also writes into.
	writeFile(t, f.repo, "docs/notes.txt", "mine\n")
	dirClash := f.agent(t, "Dir clash", 0)
	commitIn(t, dirClash.path, "docs/notes.txt", "agent\n", "adds docs/notes.txt")
	if plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(dirClash, f, t)); plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockUntrackedClash) {
		t.Errorf("a clash inside an untracked directory: %+v", plan.Blockers)
	}
}

func TestMergeRefusesWhenTheTargetIsNotCheckedOutOrIsBusy(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Nowhere to merge", 1)
	in := mergeIn(a, f, t)

	git(t, f.repo, "checkout", "-q", "--detach")
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockNotCheckedOut) {
		t.Errorf("detached HEAD: %+v", plan.Blockers)
	}
	git(t, f.repo, "checkout", "-q", "main")

	// A merge left unfinished in the target checkout.
	other := f.agent(t, "Other side", 0)
	commitIn(t, other.path, "README.md", "other side\n", "conflicting edit")
	commitIn(t, f.repo, "README.md", "main side\n", "main edit")
	_ = execGitErr(f.repo, "merge", other.branch) // leaves a conflicted merge
	plan, _ = f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(a, f, t))
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockOperation) {
		t.Errorf("unfinished merge: %+v", plan.Blockers)
	}
	if res, _ := f.gc.Merge(f.ctx, f.project.ID, mergeIn(a, f, t)); res.OK {
		t.Error("merged into a checkout that is mid-merge")
	}
	if !exists(filepath.Join(f.repo, ".git", "MERGE_HEAD")) {
		t.Error("the user's unfinished merge was disturbed")
	}
}

func TestMergePlanReportsConflictsFromGitsOwnSimulation(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Conflicting", 0)
	commitIn(t, a.path, "README.md", "agent version\n", "agent edit")
	commitIn(t, f.repo, "README.md", "main version\n", "main edit")
	in := mergeIn(a, f, t)
	mainBefore := in.TargetSha

	plan, err := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockConflicts) || plan.Conflicts.Result != domain.ConflictConflicts ||
		plan.Conflicts.Method != domain.CheckSimulation || len(plan.Conflicts.Files) != 1 || plan.Conflicts.Files[0] != "README.md" {
		t.Fatalf("plan = %+v", plan)
	}
	res, err := f.gc.Merge(f.ctx, f.project.ID, in)
	if err != nil || res.OK || res.Outcome != domain.OutcomeRefused {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if f.sha(t, f.repo, "main") != mainBefore || gitOut(t, f.repo, "status", "--porcelain") != "" {
		t.Error("the refused merge left the checkout changed")
	}
}

// Without Git's simulation (before 2.38) only the overlap heuristic is possible,
// and it must be reported as a heuristic: a hint, never a verdict. If the merge
// then really conflicts, it is aborted and the checkout is as it was.
func TestWithoutSimulationConflictsAreAHeuristicAndARealConflictIsUndone(t *testing.T) {
	f := newGC(t)
	f.gc.Git = &gitrepo.CLI{Binary: unsupportedMergeTree(t)}
	a := f.agent(t, "Heuristic", 0)
	commitIn(t, a.path, "README.md", "agent version\n", "agent edit")
	commitIn(t, f.repo, "README.md", "main version\n", "main edit")
	in := mergeIn(a, f, t)

	plan, err := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Conflicts.Method != domain.CheckOverlap || plan.Conflicts.Result != domain.ConflictPossible || len(plan.Conflicts.Files) != 1 {
		t.Fatalf("conflicts = %+v", plan.Conflicts)
	}
	if hasBlocker(plan.Blockers, domain.BlockConflicts) || !plan.CanMerge || len(plan.Warnings) == 0 {
		t.Fatalf("a heuristic must warn, not block or certify: %+v", plan)
	}
	if strings.Contains(strings.ToLower(plan.Conflicts.Note), "will conflict") {
		t.Errorf("the note states the heuristic as certain: %q", plan.Conflicts.Note)
	}

	// The user merges anyway, and the merge really conflicts.
	mainBefore := f.sha(t, f.repo, "main")
	res, err := f.gc.Merge(f.ctx, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Outcome != domain.OutcomeConflict || !hasBlocker(res.Blockers, domain.BlockConflicts) {
		t.Fatalf("merge = %+v", res)
	}
	if f.sha(t, f.repo, "main") != mainBefore || gitOut(t, f.repo, "status", "--porcelain") != "" || exists(filepath.Join(f.repo, ".git", "MERGE_HEAD")) {
		t.Error("the conflicted merge was not undone")
	}
	if b, _ := os.ReadFile(filepath.Join(f.repo, "README.md")); string(b) != "main version\n" {
		t.Errorf("README.md = %q", b)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("a conflicted merge must leave no merged event")
	}

	// No overlap: unknown, not "clean".
	b := f.agent(t, "No overlap", 1)
	plan, _ = f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(b, f, t))
	if plan.Conflicts.Result != domain.ConflictUnknown || plan.Conflicts.Method != domain.CheckOverlap {
		t.Errorf("no overlap: %+v", plan.Conflicts)
	}
}

func unsupportedMergeTree(t *testing.T) string {
	t.Helper()
	path := failingGit(t, "merge-tree")
	// An old Git answers an unknown subcommand option with usage and 129.
	b, _ := os.ReadFile(path)
	b = []byte(strings.ReplaceAll(string(b), "echo 'fatal: simulated failure' >&2; exit 1", "echo 'usage: git merge-tree' >&2; exit 129"))
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMergeIsBlockedWhileTheAgentIsWorking(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Still working", 1)
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: a.task.ID, AgentID: "fake", Prompt: "more", WorktreeID: a.wt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	in := mergeIn(a, f, t)
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockRunActive) {
		t.Fatalf("running: %+v", plan.Blockers)
	}
	if res, _ := f.gc.Merge(f.ctx, f.project.ID, in); res.OK {
		t.Error("merged a branch an agent is working on")
	}
	// An idle session (finished a turn, waiting for the next message) does not move the branch: a warning.
	if _, err := f.runs.MarkIdle(f.ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	plan, _ = f.gc.MergePlan(f.ctx, f.project.ID, in)
	if !plan.CanMerge || len(plan.Warnings) == 0 || !strings.Contains(strings.Join(plan.Warnings, " "), "session") {
		t.Errorf("idle: canMerge=%v blockers=%v warnings=%v", plan.CanMerge, plan.Blockers, plan.Warnings)
	}
}

func TestMergeIntoTheWrongTargetOrABadBranchIsRefused(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Wrong target", 1)
	in := mergeIn(a, f, t)
	in.Target = "develop"
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, in)
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockTargetMismatch) {
		t.Errorf("target mismatch: %+v", plan.Blockers)
	}
	for name, want := range map[string]string{"main": domain.BlockIsTarget, "no-such-branch": domain.BlockBranchMissing, "a..b": domain.BlockInvalidBranch} {
		plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, MergeInput{Branch: name})
		if plan.CanMerge || !hasBlocker(plan.Blockers, want) {
			t.Errorf("%s: %v", name, codes(plan.Blockers))
		}
	}
	// Unrelated histories.
	git(t, f.repo, "checkout", "-q", "--orphan", "unrelated")
	git(t, f.repo, "rm", "-rfq", ".")
	commitIn(t, f.repo, "other.txt", "o\n", "orphan root")
	git(t, f.repo, "checkout", "-q", "main")
	plan, _ = f.gc.MergePlan(f.ctx, f.project.ID, MergeInput{Branch: "unrelated"})
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockUnrelated) {
		t.Errorf("unrelated: %v", codes(plan.Blockers))
	}
}

// The target must exist locally: only origin/main is not something to merge into.
func TestMergeNeedsALocalTarget(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "No local main", 1)
	git(t, f.repo, "checkout", "-q", "--detach")
	git(t, f.repo, "branch", "-D", "main")
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, MergeInput{Branch: a.branch})
	if plan.CanMerge || !hasBlocker(plan.Blockers, domain.BlockTargetMissing) {
		t.Errorf("plan = %v", codes(plan.Blockers))
	}
}

func TestAMergedTargetBehindItsRemoteWarns(t *testing.T) {
	f := newGC(t)
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", f.remote, other)
	commitIn(t, other, "r.txt", "r\n", "remote moved")
	git(t, other, "push", "-q", "origin", "main")
	if _, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	a := f.agent(t, "Behind remote", 1)
	plan, _ := f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(a, f, t))
	if !plan.CanMerge || !strings.Contains(strings.Join(plan.Warnings, " "), "behind origin/main") {
		t.Errorf("plan = canMerge %v warnings %v", plan.CanMerge, plan.Warnings)
	}
}
