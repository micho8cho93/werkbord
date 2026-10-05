package service

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
)

// branchExists is whether a local branch exists, for assertions about deletion.
func (f *gcFixture) branchExists(t *testing.T, name string) bool {
	t.Helper()
	_, err := execGit(f.repo, "rev-parse", "--verify", "-q", "refs/heads/"+name)
	return err == nil
}

// mergeAndClean merges an agent branch into main (test setup) and removes its worktree.
func (f *gcFixture) mergeAway(t *testing.T, a *agentBranch) {
	t.Helper()
	git(t, f.repo, "merge", "-q", "--no-ff", "-m", "merge", a.branch)
}

func (f *gcFixture) deleteInput(t *testing.T, branch string) DeleteInput {
	return DeleteInput{Branch: branch, BranchSha: f.sha(t, f.repo, branch)}
}

// ---- deleting branches ----

func TestDeleteBranchSucceedsForAMergedBranchDevBoardCreated(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Merged and done", 2)
	f.mergeAway(t, a)
	in := f.deleteInput(t, a.branch)

	// Still checked out in its worktree: refused, pointing at the worktree.
	plan, err := f.gc.DeletePlan(f.ctx, f.project.ID, in)
	if err != nil || plan.CanDelete || !hasBlocker(plan.Blockers, domain.BlockCheckedOut) || !plan.Merged || plan.MergedVia != "ancestry" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	res, _ := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if res.OK || !f.branchExists(t, a.branch) {
		t.Fatalf("deleted a branch that is checked out: %+v", res)
	}

	// Clean the worktree first (the branch stays), then delete.
	clean, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID})
	if err != nil || !clean.OK {
		t.Fatalf("clean = %+v, %v", clean, err)
	}
	if !f.branchExists(t, a.branch) {
		t.Fatal("removing the worktree deleted the branch")
	}
	res, err = f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if f.branchExists(t, a.branch) {
		t.Error("the branch is still there")
	}
	if !strings.Contains(res.Local.Note, "git branch "+a.branch+" "+in.BranchSha) {
		t.Errorf("the result must say how to bring it back: %q", res.Local.Note)
	}
	if gitOut(t, f.repo, "cat-file", "-t", in.BranchSha) != "commit" {
		t.Error("the commit itself must remain")
	}
	if res.Remote != nil {
		t.Errorf("a local deletion has no remote effect: %+v", res.Remote)
	}
	evs := f.gitEvents(t)
	if len(evs) != 2 || evs[1].Type != domain.EventGitBranchDeleted || evs[1].TaskID != a.task.ID {
		t.Errorf("events = %+v", evs)
	}
}

func TestDeleteBranchNeverDeletesWhatDevBoardDidNotCreate(t *testing.T) {
	f := newGC(t)
	// A merged branch of the user's own, and one that only borrows Werkbord's name.
	git(t, f.repo, "branch", "users-merged-branch", "main")
	git(t, f.repo, "branch", "devboard/impostor-111111", "main")
	// And a real one, whose worktree record belongs to ANOTHER project.
	for _, name := range []string{"users-merged-branch", "devboard/impostor-111111"} {
		in := f.deleteInput(t, name)
		plan, err := f.gc.DeletePlan(f.ctx, f.project.ID, in)
		if err != nil || plan.CanDelete || !hasBlocker(plan.Blockers, domain.BlockNotOwned) {
			t.Errorf("%s: plan = %+v, %v", name, plan, err)
		}
		res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
		if err != nil || res.OK || res.Outcome != domain.OutcomeRefused || !f.branchExists(t, name) {
			t.Errorf("%s: delete = %+v, %v (exists=%v)", name, res, err, f.branchExists(t, name))
		}
	}
	// Even if every other condition holds, a missing record means "not ours".
	if len(f.gitEvents(t)) != 0 {
		t.Error("refused deletions must leave no event")
	}
}

func TestDeleteBranchRefusesProtectedBranches(t *testing.T) {
	f := newGC(t)
	for _, name := range []string{"main", "master", "develop", "release/1.0", "hotfix/x"} {
		if name != "main" {
			git(t, f.repo, "branch", name, "main")
		}
		res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, f.deleteInput(t, name))
		if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockProtected) {
			t.Errorf("%s: %+v, %v", name, res, err)
		}
		if !f.branchExists(t, name) {
			t.Errorf("%s was deleted", name)
		}
	}
	// A branch checked out in the project's own checkout is protected, whoever made it.
	a := f.agent(t, "Checked out here", 1)
	f.mergeAway(t, a)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	git(t, f.repo, "checkout", "-q", a.branch)
	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, f.deleteInput(t, a.branch))
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockProtected) || !hasBlocker(res.Blockers, domain.BlockCheckedOut) || !f.branchExists(t, a.branch) {
		t.Errorf("deleting the checked-out branch: %+v, %v", res, err)
	}
}

func TestDeleteBranchRefusesUnmergedWork(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Unmerged work", 3)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	in := f.deleteInput(t, a.branch)
	plan, err := f.gc.DeletePlan(f.ctx, f.project.ID, in)
	if err != nil || plan.CanDelete || plan.Merged || !hasBlocker(plan.Blockers, domain.BlockNotMerged) {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if !strings.Contains(plan.Blockers[0].Message, "3 commits on") || !strings.Contains(plan.Blockers[0].Message, "are not in main") {
		t.Errorf("message = %q", plan.Blockers[0].Message)
	}
	res, _ := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if res.OK || !f.branchExists(t, a.branch) {
		t.Fatalf("an unmerged branch was deleted: %+v", res)
	}
	// Pushed to the remote is still not "merged".
	git(t, f.repo, "push", "-q", "origin", a.branch)
	if res, _ := f.gc.DeleteBranch(f.ctx, f.project.ID, in); res.OK || !f.branchExists(t, a.branch) {
		t.Errorf("a pushed but unmerged branch was deleted: %+v", res)
	}
}

func TestDeleteBranchRefusesABranchThatMovedSinceInspection(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Moved after look", 1)
	f.mergeAway(t, a)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	seen := f.deleteInput(t, a.branch)
	// After the user looked, the branch gets a new commit (from anywhere).
	tmp := filepath.Join(t.TempDir(), "tmp-wt")
	git(t, f.repo, "worktree", "add", "-q", tmp, a.branch)
	newer := commitIn(t, tmp, "late.txt", "x\n", "late commit")
	git(t, f.repo, "worktree", "remove", "--force", tmp)

	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, seen)
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockBranchMoved) {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if f.sha(t, f.repo, a.branch) != newer {
		t.Fatal("the branch was deleted or changed")
	}
	if res, _ := f.gc.DeleteBranch(f.ctx, f.project.ID, DeleteInput{Branch: a.branch}); res.OK || !hasBlocker(res.Blockers, domain.BlockInvalidInput) {
		t.Errorf("a deletion that does not say what was reviewed: %+v", res)
	}
}

func TestDeleteBranchRefusesWhileARunHasASession(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Session open", 1)
	f.mergeAway(t, a)
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: a.task.ID, AgentID: "fake", Prompt: "again", WorktreeID: a.wt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	plan, _ := f.gc.DeletePlan(f.ctx, f.project.ID, f.deleteInput(t, a.branch))
	if plan.CanDelete || !hasBlocker(plan.Blockers, domain.BlockRunActive) {
		t.Errorf("plan = %v", codes(plan.Blockers))
	}
}

func TestDeleteBranchOnAPullRequestMergedByGitHub(t *testing.T) {
	f := newGC(t)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	a := f.agent(t, "Squash merged", 2)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	tip := f.sha(t, f.repo, a.branch)
	in := f.deleteInput(t, a.branch)

	// A squash merge leaves the branch's commits out of main: by ancestry it is unmerged.
	gh.set(t, "list.json", "["+prJSON(2, "MERGED", a.branch, tip)+"]")
	plan, err := f.gc.DeletePlan(f.ctx, f.project.ID, in)
	if err != nil || !plan.CanDelete || !plan.Merged || plan.MergedVia != "pull_request" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	// A merged pull request of a different tip is not evidence about this one.
	gh.set(t, "list.json", "["+prJSON(2, "MERGED", a.branch, strings.Repeat("a", 40))+"]")
	if plan, _ := f.gc.DeletePlan(f.ctx, f.project.ID, in); plan.CanDelete || !hasBlocker(plan.Blockers, domain.BlockNotMerged) {
		t.Errorf("a pull request merged at another commit: %+v", plan.Blockers)
	}
	// Nor is an open one.
	gh.set(t, "list.json", "["+prJSON(2, "OPEN", a.branch, tip)+"]")
	if plan, _ := f.gc.DeletePlan(f.ctx, f.project.ID, in); plan.CanDelete {
		t.Error("an open pull request is not a merge")
	}
	// GitHub unreachable: no evidence, so no deletion, and the plan says GitHub could not be asked.
	gh.failWith(t, "error connecting to api.github.com")
	plan, _ = f.gc.DeletePlan(f.ctx, f.project.ID, in)
	if plan.CanDelete || !strings.Contains(strings.Join(plan.Warnings, " "), "GitHub could not be asked") {
		t.Errorf("offline plan = %+v", plan)
	}
	_ = os.Remove(filepath.Join(gh.dir, "fail"))
	gh.set(t, "list.json", "["+prJSON(2, "MERGED", a.branch, tip)+"]")
	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if err != nil || !res.OK || f.branchExists(t, a.branch) {
		t.Fatalf("delete = %+v, %v", res, err)
	}
}

func TestDeleteRemoteBranchToo(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Remote cleanup", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	f.mergeAway(t, a)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	in := f.deleteInput(t, a.branch)
	in.DeleteRemote = true

	plan, _ := f.gc.DeletePlan(f.ctx, f.project.ID, in)
	if !plan.CanDelete || !plan.CanDeleteRemote || !plan.RemoteExist || plan.RemoteRef != "origin/"+a.branch {
		t.Fatalf("plan = %+v", plan)
	}
	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if err != nil || !res.OK || res.Remote == nil || !res.Remote.Verified {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if f.branchExists(t, a.branch) || f.remoteSha(t, a.branch) != "" {
		t.Error("the branch is still there, locally or on the remote")
	}
}

// The remote branch has a commit nobody has seen: it is neither deleted nor is
// anything else, because the user asked for both.
func TestDeleteRemoteBranchRefusesCommitsItHasNotSeen(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Remote has more", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	f.mergeAway(t, a)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	// A colleague adds a commit to the remote branch; a fetch brings the tracking ref along.
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", "-b", a.branch, f.remote, other)
	theirs := commitIn(t, other, "theirs.txt", "t\n", "work on the remote branch")
	git(t, other, "push", "-q", "origin", a.branch)
	if _, err := f.gc.Fetch(f.ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}
	in := f.deleteInput(t, a.branch)
	in.DeleteRemote = true
	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockRemoteUnmerged) {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if !f.branchExists(t, a.branch) || f.remoteSha(t, a.branch) != theirs {
		t.Fatal("something was deleted although the request could not be honoured in full")
	}
	// Deleting only the local branch is still possible: the remote copy stays.
	in.DeleteRemote = false
	if res, _ := f.gc.DeleteBranch(f.ctx, f.project.ID, in); !res.OK || f.remoteSha(t, a.branch) != theirs {
		t.Errorf("local-only deletion = %+v", res)
	}
}

// The remote branch moved after the plan was made but before it was asked: the
// local branch goes (it was safe), the remote one is NOT deleted, and the result says so.
func TestDeleteRemoteBranchWhoseTipMovedIsKept(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Raced remote", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	f.mergeAway(t, a)
	if res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	in := f.deleteInput(t, a.branch)
	in.DeleteRemote = true
	// The remote branch moves, and nobody has fetched.
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", "-b", a.branch, f.remote, other)
	theirs := commitIn(t, other, "raced.txt", "r\n", "pushed after the plan")
	git(t, other, "push", "-q", "origin", a.branch)

	res, err := f.gc.DeleteBranch(f.ctx, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Remote == nil || res.Remote.Verified || !strings.Contains(res.Message, "NOT") {
		t.Fatalf("result = %+v", res)
	}
	if f.remoteSha(t, a.branch) != theirs {
		t.Fatal("the remote branch was deleted although it had moved")
	}
	if f.branchExists(t, a.branch) {
		t.Error("the local deletion should have gone ahead, and been reported as such")
	}
	if res.Local == nil {
		t.Error("the local effect must still be reported")
	}
}

// ---- cleaning worktrees ----

func TestCleanWorktreeRemovesCleanDirectoryAndKeepsTheBranch(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Clean me", 2)
	plan, err := f.gc.CleanPlan(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID, HeadSha: f.sha(t, a.path, "HEAD")})
	if err != nil || !plan.CanClean || plan.Dirty == nil || plan.Dirty.Dirty() || len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "keeps 2 commits") {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID, HeadSha: f.sha(t, a.path, "HEAD")})
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	if exists(a.path) || !f.branchExists(t, a.branch) {
		t.Errorf("directory exists=%v branch exists=%v", exists(a.path), f.branchExists(t, a.branch))
	}
	wt, _ := f.wts.Get(f.ctx, a.wt.ID)
	if wt.State != domain.WorktreeRemoved {
		t.Errorf("record = %+v", wt)
	}
	if strings.Contains(gitOut(t, f.repo, "worktree", "list", "--porcelain"), a.path) {
		t.Error("git still lists the worktree")
	}
	// Doing it again: already removed.
	if again, _ := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); again.OK || !hasBlocker(again.Blockers, domain.BlockWorktreeUnknown) {
		t.Errorf("second clean = %+v", again)
	}
	evs := f.gitEvents(t)
	if len(evs) != 1 || evs[0].Type != domain.EventGitTreeCleaned || evs[0].TaskID != a.task.ID {
		t.Errorf("events = %+v", evs)
	}
}

// Uncommitted work is never destroyed, whatever form it takes.
func TestCleanWorktreeRefusesUncommittedWork(t *testing.T) {
	f := newGC(t)
	for name, dirty := range map[string]func(dir string){
		"untracked": func(dir string) { writeFile(t, dir, "scratch.txt", "precious\n") },
		"modified":  func(dir string) { writeFile(t, dir, "README.md", "precious edit\n") },
		"staged": func(dir string) {
			writeFile(t, dir, "new.txt", "precious\n")
			git(t, dir, "add", "new.txt")
		},
		"ignored-but-untracked-dir": func(dir string) { writeFile(t, dir, "deep/er/file.txt", "precious\n") },
		"mid-merge": func(dir string) {
			commitIn(t, dir, "README.md", "one\n", "one")
			git(t, dir, "checkout", "-q", "-b", "side", "HEAD~1")
			commitIn(t, dir, "README.md", "two\n", "two")
			git(t, dir, "checkout", "-q", "-")
			_ = execGitErr(dir, "merge", "side")
		},
	} {
		a := f.agent(t, "Dirty "+name, 0)
		dirty(a.path)
		before := gitOut(t, a.path, "status", "--porcelain")
		in := CleanInput{WorktreeID: a.wt.ID}
		plan, err := f.gc.CleanPlan(f.ctx, f.project.ID, in)
		if err != nil || plan.CanClean {
			t.Errorf("%s: plan = %+v, %v", name, plan, err)
			continue
		}
		res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, in)
		if err != nil || res.OK || res.Outcome != domain.OutcomeRefused {
			t.Errorf("%s: clean = %+v, %v", name, res, err)
		}
		if !exists(a.path) || gitOut(t, a.path, "status", "--porcelain") != before {
			t.Errorf("%s: the worktree was changed", name)
		}
		if wt, _ := f.wts.Get(f.ctx, a.wt.ID); wt.State != domain.WorktreeActive || wt.Removing() {
			t.Errorf("%s: the record changed although nothing was removed: %+v", name, wt)
		}
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("refused cleanups must leave no event")
	}
}

func TestCleanWorktreeRefusesWhileARunUsesIt(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "In use", 1)
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: a.task.ID, AgentID: "fake", Prompt: "again", WorktreeID: a.wt.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	res, _ := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID})
	if res.OK || !hasBlocker(res.Blockers, domain.BlockRunActive) || !exists(a.path) {
		t.Fatalf("clean = %+v", res)
	}
}

func TestCleanWorktreeOwnershipAndIdentityChecks(t *testing.T) {
	f := newGC(t)
	clean := func(in CleanInput) *domain.GitCleanPlan {
		t.Helper()
		plan, err := f.gc.CleanPlan(f.ctx, f.project.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}

	// Unknown, and another project's, worktree: not found.
	if _, err := f.gc.CleanPlan(f.ctx, f.project.ID, CleanInput{WorktreeID: "wt_nope"}); err == nil {
		t.Error("an unknown worktree must be an error")
	}
	other := filepath.Join(t.TempDir(), "other")
	initRepo(t, other)
	op, err := f.projects.Register(f.ctx, other, "other")
	if err != nil {
		t.Fatal(err)
	}
	a := f.agent(t, "Mine", 1)
	if _, err := f.gc.CleanPlan(f.ctx, op.ID, CleanInput{WorktreeID: a.wt.ID}); err == nil {
		t.Error("a project must not be able to clean another project's worktree")
	}

	// It moved since it was looked at.
	seen := f.sha(t, a.path, "HEAD")
	commitIn(t, a.path, "later.txt", "l\n", "later")
	if p := clean(CleanInput{WorktreeID: a.wt.ID, HeadSha: seen}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeChanged) {
		t.Errorf("moved head: %v", codes(p.Blockers))
	}

	// It was switched to another branch outside Werkbord.
	b := f.agent(t, "Switched", 0)
	git(t, b.path, "checkout", "-q", "-b", "someone-elses-branch")
	if p := clean(CleanInput{WorktreeID: b.wt.ID}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeChanged) {
		t.Errorf("switched branch: %v", codes(p.Blockers))
	}

	// Locked.
	c := f.agent(t, "Locked", 0)
	git(t, f.repo, "worktree", "lock", "--reason", "do not touch", c.path)
	if p := clean(CleanInput{WorktreeID: c.wt.ID}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeLocked) {
		t.Errorf("locked: %v", codes(p.Blockers))
	}
	if res, _ := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: c.wt.ID}); res.OK || !exists(c.path) {
		t.Error("a locked worktree was removed")
	}

	// A directory Git does not know as a worktree is never deleted on the strength of a record.
	d := f.agent(t, "Forgotten by git", 0)
	if err := os.RemoveAll(filepath.Join(f.repo, ".git", "worktrees", filepath.Base(d.path))); err != nil {
		t.Fatal(err)
	}
	if p := clean(CleanInput{WorktreeID: d.wt.ID}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeUnknown) {
		t.Errorf("unknown to git: %v", codes(p.Blockers))
	}
	if res, _ := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: d.wt.ID}); res.OK || !exists(d.path) {
		t.Error("a directory that git does not list was deleted")
	}

	// Outside Werkbord's worktree directory.
	e := f.agent(t, "Outside", 0)
	f.gc.Worktrees = &Worktrees{Deps: f.deps, Root: filepath.Join(f.wtRoot, "elsewhere")}
	if p := clean(CleanInput{WorktreeID: e.wt.ID}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeOutside) {
		t.Errorf("outside the root: %v", codes(p.Blockers))
	}
	f.gc.Worktrees = &Worktrees{Deps: f.deps}
	if p := clean(CleanInput{WorktreeID: e.wt.ID}); p.CanClean || !hasBlocker(p.Blockers, domain.BlockWorktreeOutside) {
		t.Errorf("no root configured must fail closed: %v", codes(p.Blockers))
	}
	f.gc.Worktrees = f.wts
}

func TestCleanWorktreeWhoseDirectoryIsAlreadyGoneOnlyForgetsIt(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Vanished", 1)
	if err := os.RemoveAll(a.path); err != nil {
		t.Fatal(err)
	}
	plan, err := f.gc.CleanPlan(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID})
	if err != nil || !plan.CanClean || !plan.Missing {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	// It is flagged on the overview too.
	if b := mustBranch(t, f.overview(t), a.branch); b.Worktree == nil || !b.Worktree.Missing || !hasAttention(b, domain.AttentionMissing) {
		t.Errorf("branch = %+v", b.Worktree)
	}
	res, err := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID})
	if err != nil || !res.OK {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	if wt, _ := f.wts.Get(f.ctx, a.wt.ID); wt.State != domain.WorktreeRemoved || !f.branchExists(t, a.branch) {
		t.Errorf("record = %+v, branch exists = %v", wt, f.branchExists(t, a.branch))
	}
}

// A symlink in the path must never let a deletion follow it out of the worktree directory.
func TestCleanWorktreeRefusesAPathThroughASymlink(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Symlinked", 0)
	real := a.path + "-real"
	if err := os.Rename(a.path, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, a.path); err != nil {
		t.Fatal(err)
	}
	plan, err := f.gc.CleanPlan(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID})
	if err != nil || plan.CanClean {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if res, _ := f.gc.CleanWorktree(f.ctx, f.project.ID, CleanInput{WorktreeID: a.wt.ID}); res.OK || !exists(real) {
		t.Errorf("clean = %+v", res)
	}
}

// ---- pull requests ----

func TestCreatePullRequestOnlyFromAPushedBranchAndReadsItBack(t *testing.T) {
	f := newGC(t)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	a := f.agent(t, "Propose this", 2)
	tip := f.sha(t, f.repo, a.branch)
	in := PRInput{Branch: a.branch, ExpectedSha: tip, Title: "Propose this", Body: "Because."}

	// Not on the remote yet: refused. Nothing is pushed for the user.
	res, err := f.gc.CreatePullRequest(f.ctx, f.project.ID, in)
	if err != nil || res.OK || !hasBlocker(res.Blockers, domain.BlockNotPushed) {
		t.Fatalf("unpushed: %+v, %v", res, err)
	}
	if f.remoteSha(t, a.branch) != "" || strings.Contains(gh.calls(t), "pr create") {
		t.Fatal("a pull request was attempted, or the branch pushed, for an unpushed branch")
	}
	// Pushed, then more committed locally: the pull request would show the older state.
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	newer := commitIn(t, a.path, "later.txt", "l\n", "unpushed follow-up")
	res, _ = f.gc.CreatePullRequest(f.ctx, f.project.ID, PRInput{Branch: a.branch, ExpectedSha: newer, Title: "t"})
	if res.OK || !hasBlocker(res.Blockers, domain.BlockRemoteDiffers) {
		t.Fatalf("remote differs: %+v", res)
	}
	git(t, a.path, "push", "-q", "origin", a.branch)

	// The pull request that GitHub reports is what is returned, confirmed.
	view := prJSON(5, "OPEN", a.branch, newer)
	gh.set(t, "view.json", view)
	gh.set(t, "create.out", "https://github.com/acme/app/pull/5\n")
	res, err = f.gc.CreatePullRequest(f.ctx, f.project.ID, PRInput{Branch: a.branch, ExpectedSha: newer, Title: "Propose this", Body: "Because.", Draft: true})
	if err != nil || !res.OK || res.Outcome != domain.OutcomeDone || res.PullRequest == nil || res.PullRequest.Number != 5 || res.Remote == nil || !res.Remote.Verified {
		t.Fatalf("create = %+v, %v", res, err)
	}
	calls := gh.calls(t)
	if !strings.Contains(calls, "pr create -R acme/app --head "+a.branch+" --base main --title Propose this --body Because. --draft") {
		t.Errorf("gh calls:\n%s", calls)
	}
	evs := f.gitEvents(t)
	if len(evs) != 1 || evs[0].Type != domain.EventGitPRCreated || evs[0].TaskID != a.task.ID {
		t.Errorf("events = %+v", evs)
	}
}

func TestCreatePullRequestRefusals(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "Refusals", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	tip := f.sha(t, f.repo, a.branch)
	good := PRInput{Branch: a.branch, ExpectedSha: tip, Title: "t"}
	try := func(in PRInput, want string) *domain.GitActionResult {
		t.Helper()
		res, err := f.gc.CreatePullRequest(f.ctx, f.project.ID, in)
		if err != nil || res.OK || !hasBlocker(res.Blockers, want) {
			t.Errorf("want %s: %+v, %v", want, res, err)
		}
		return res
	}

	f.gc.GitHub = nil
	try(good, domain.BlockNoGitHub)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli

	for name, in := range map[string]PRInput{
		"no title":    {Branch: a.branch, ExpectedSha: tip},
		"blank title": {Branch: a.branch, ExpectedSha: tip, Title: "   "},
		"long title":  {Branch: a.branch, ExpectedSha: tip, Title: strings.Repeat("x", 300)},
		"huge body":   {Branch: a.branch, ExpectedSha: tip, Title: "t", Body: strings.Repeat("x", 70000)},
		"no commit":   {Branch: a.branch, Title: "t"},
	} {
		_ = name
		try(in, domain.BlockInvalidInput)
	}
	try(PRInput{Branch: a.branch, ExpectedSha: strings.Repeat("b", 40), Title: "t"}, domain.BlockBranchMoved)
	try(PRInput{Branch: "main", ExpectedSha: f.sha(t, f.repo, "main"), Title: "t"}, domain.BlockIsTarget)
	try(PRInput{Branch: "a..b", ExpectedSha: tip, Title: "t"}, domain.BlockInvalidBranch)

	// Nothing to propose.
	empty := f.agent(t, "Empty", 0)
	git(t, empty.path, "push", "-q", "-u", "origin", empty.branch)
	try(PRInput{Branch: empty.branch, ExpectedSha: f.sha(t, f.repo, empty.branch), Title: "t"}, domain.BlockAlreadyMerged)

	// One is already open.
	gh.set(t, "list.json", "["+prJSON(8, "OPEN", a.branch, tip)+"]")
	if res := try(good, domain.BlockPRExists); res.PullRequest == nil || res.PullRequest.Number != 8 {
		t.Errorf("the existing pull request should be returned: %+v", res.PullRequest)
	}
	gh.set(t, "list.json", "[]")
	if strings.Contains(gh.calls(t), "pr create") {
		t.Error("gh pr create was called by a refused request")
	}

	// The remote is unavailable: that is said, not guessed.
	if err := os.RemoveAll(f.remote); err != nil {
		t.Fatal(err)
	}
	res, err := f.gc.CreatePullRequest(f.ctx, f.project.ID, good)
	if err != nil || res.OK || res.Outcome != domain.OutcomeUnavailable {
		t.Errorf("unavailable remote: %+v, %v", res, err)
	}
}

func TestCreatePullRequestGitHubFailuresAreNotSuccess(t *testing.T) {
	f := newGC(t)
	gh := newFakeGH(t)
	f.gc.GitHub = gh.cli
	a := f.agent(t, "Gh trouble", 1)
	git(t, a.path, "push", "-q", "-u", "origin", a.branch)
	tip := f.sha(t, f.repo, a.branch)
	in := PRInput{Branch: a.branch, ExpectedSha: tip, Title: "t"}

	gh.failWith(t, "To get started with GitHub CLI, please run:  gh auth login")
	res, err := f.gc.CreatePullRequest(f.ctx, f.project.ID, in)
	if err != nil || res.OK || res.Outcome != domain.OutcomeAuth || res.PullRequest != nil {
		t.Fatalf("signed out: %+v, %v", res, err)
	}
	gh.failWith(t, "error connecting to api.github.com")
	if res, _ := f.gc.CreatePullRequest(f.ctx, f.project.ID, in); res.OK || res.Outcome != domain.OutcomeFailed {
		t.Errorf("offline: %+v", res)
	}
	_ = os.Remove(filepath.Join(gh.dir, "fail"))

	// gh reports success but the pull request is not what was asked for: not reported as done.
	gh.set(t, "view.json", prJSON(6, "CLOSED", a.branch, tip))
	gh.set(t, "create.out", "https://github.com/acme/app/pull/6\n")
	res, _ = f.gc.CreatePullRequest(f.ctx, f.project.ID, in)
	if res.OK || res.Outcome != domain.OutcomeUnverified || res.Remote == nil || res.Remote.Verified {
		t.Errorf("unconfirmed: %+v", res)
	}
	if len(f.gitEvents(t)) != 0 {
		t.Error("an unconfirmed pull request must not be recorded as created")
	}
}

// ---- an agent never merges ----

// The agent runner is handed only gitrepo.Worktrees. Its method set is the
// guarantee that a finished run cannot merge, push, force or delete anything but
// the branch it just created: if someone adds such a method to that interface,
// this fails and the question has to be asked on purpose.
func TestRunnerCannotMerge(t *testing.T) {
	typ := reflect.TypeOf((*gitrepo.Worktrees)(nil)).Elem()
	var got []string
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	want := []string{"AddWorktree", "DeleteBranch", "RemoveWorktree"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gitrepo.Worktrees methods = %v, want exactly %v: the runner's Git interface must never gain a merge, push or force", got, want)
	}
}

// A run completing, and its task moving to Review, change nothing in Git: the
// target branch, the remote and the audit trail stay exactly as they were.
func TestFinishingARunDoesNotMergeAnything(t *testing.T) {
	f := newGC(t)
	mainBefore, remoteBefore := f.sha(t, f.repo, "main"), f.remoteSha(t, "main")
	a := f.agent(t, "Done by the agent", 3) // commits, finishes the run, moves the task to Review
	a.moveTask(t, f, domain.TaskDone)

	if f.sha(t, f.repo, "main") != mainBefore || f.remoteSha(t, "main") != remoteBefore {
		t.Fatal("main moved when an agent finished")
	}
	if err := execGitErr(f.repo, "merge-base", "--is-ancestor", a.branch, "main"); err == nil {
		t.Fatal("the agent's branch ended up in main")
	}
	if len(f.gitEvents(t)) != 0 {
		t.Errorf("git events appeared without anyone acting: %+v", f.gitEvents(t))
	}
	// Even the Git Control Center's own reads leave everything as it was.
	_ = f.overview(t)
	if _, err := f.gc.MergePlan(f.ctx, f.project.ID, mergeIn(a, f, t)); err != nil {
		t.Fatal(err)
	}
	if f.sha(t, f.repo, "main") != mainBefore || len(f.gitEvents(t)) != 0 {
		t.Fatal("reading changed something")
	}
}

// Actions on a project are serialised: two merges at once cannot interleave.
func TestActionsOnOneProjectAreSerialised(t *testing.T) {
	f := newGC(t)
	a := f.agent(t, "First", 1)
	b := f.agent(t, "Second", 1)
	ia, ib := mergeIn(a, f, t), mergeIn(b, f, t)
	done := make(chan *domain.GitActionResult, 2)
	for _, in := range []MergeInput{ia, ib} {
		go func(in MergeInput) {
			res, err := f.gc.Merge(f.ctx, f.project.ID, in)
			if err != nil {
				t.Error(err)
			}
			done <- res
		}(in)
	}
	r1, r2 := <-done, <-done
	// Whichever went second saw the target moved by the first, and was refused rather than racing.
	ok := 0
	for _, r := range []*domain.GitActionResult{r1, r2} {
		if r.OK {
			ok++
		} else if !hasBlocker(r.Blockers, domain.BlockTargetMoved) {
			t.Errorf("the loser should have been refused because the target moved: %+v", r)
		}
	}
	if ok != 1 {
		t.Fatalf("%d merges succeeded, want exactly 1 (the other refused as stale)", ok)
	}
}
