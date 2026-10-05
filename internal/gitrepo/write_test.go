package gitrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func TestPushNewBranchThenFastForward(t *testing.T) {
	repo, remote := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	run(t, repo, "checkout", "-q", "-b", "devboard/work-aaaaaa")
	tip := commit(t, repo, "w.txt", "1\n", "work")

	res, err := g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "devboard/work-aaaaaa", Sha: tip})
	if err != nil || res.Outcome != domain.OutcomeDone || !res.NewBranch {
		t.Fatalf("push = %+v, %v", res, err)
	}
	if got, found, err := g.RemoteBranch(ctx, repo, "origin", "devboard/work-aaaaaa"); err != nil || !found || got != tip {
		t.Fatalf("the remote itself says %q (found=%v, err=%v), want %s", got, found, err, tip)
	}
	if got := gitOut(t, remote, "rev-parse", "refs/heads/devboard/work-aaaaaa"); got != tip {
		t.Errorf("remote repository has %s", got)
	}
	if err := g.SetUpstream(ctx, repo, "devboard/work-aaaaaa", "origin"); err != nil {
		t.Fatal(err)
	}
	if up := gitOut(t, repo, "rev-parse", "--abbrev-ref", "devboard/work-aaaaaa@{upstream}"); up != "origin/devboard/work-aaaaaa" {
		t.Errorf("upstream = %s", up)
	}

	tip2 := commit(t, repo, "w.txt", "2\n", "more work")
	res, _ = g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "devboard/work-aaaaaa", Sha: tip2})
	if res.Outcome != domain.OutcomeDone || res.NewBranch {
		t.Errorf("fast-forward push = %+v", res)
	}
	res, _ = g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "devboard/work-aaaaaa", Sha: tip2})
	if res.Outcome != domain.OutcomeDone || !res.UpToDate {
		t.Errorf("pushing nothing new = %+v", res)
	}
}

// A remote that moved on makes a plain push fail; Werkbord never forces it.
func TestPushRejectedWhenRemoteMovedOnAndNeverForces(t *testing.T) {
	repo, remote := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()

	// Someone else pushes to main.
	other, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, other, "clone", "-q", remote, "o")
	clone := filepath.Join(other, "o")
	theirs := commit(t, clone, "theirs.txt", "t\n", "their work")
	run(t, clone, "push", "-q", "origin", "main")

	mine := commit(t, repo, "mine.txt", "m\n", "my work")
	res, err := g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "main", Sha: mine})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != domain.OutcomeRejected || res.Message == "" {
		t.Fatalf("push = %+v, want rejected", res)
	}
	if got := gitOut(t, remote, "rev-parse", "refs/heads/main"); got != theirs {
		t.Fatalf("the remote's main is %s, want their commit %s untouched", got, theirs)
	}
	if strings.Contains(strings.ToLower(res.Message), "force") && !strings.Contains(res.Message, "never forces") {
		t.Errorf("message suggests forcing: %q", res.Message)
	}
}

func TestRemoteUnavailableIsReportedAsSuch(t *testing.T) {
	repo, remote := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	tip := commit(t, repo, "x.txt", "x\n", "x")
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}

	res, err := g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "main", Sha: tip})
	if err != nil || res.Outcome != domain.OutcomeUnavailable {
		t.Fatalf("push to a vanished remote = %+v, %v", res, err)
	}
	f, err := g.Fetch(ctx, repo, "origin")
	if err != nil || f.Outcome != domain.OutcomeUnavailable {
		t.Fatalf("fetch from a vanished remote = %+v, %v", f, err)
	}
	if _, _, err := g.RemoteBranch(ctx, repo, "origin", "main"); err == nil {
		t.Error("asking a vanished remote about a branch must be an error, not 'not found'")
	}
}

func TestPushRefusesUnknownRemoteAndBadInput(t *testing.T) {
	repo, _ := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	tip := sha(t, repo, "HEAD")
	for name, req := range map[string]PushRequest{
		"url as remote":      {Remote: "https://example.com/x.git", Branch: "main", Sha: tip},
		"option as remote":   {Remote: "--receive-pack=x", Branch: "main", Sha: tip},
		"unknown remote":     {Remote: "nope", Branch: "main", Sha: tip},
		"option as branch":   {Remote: "origin", Branch: "--delete", Sha: tip},
		"bad branch":         {Remote: "origin", Branch: "a..b", Sha: tip},
		"branch name as sha": {Remote: "origin", Branch: "main", Sha: "main"},
		"empty sha":          {Remote: "origin", Branch: "main"},
	} {
		if _, err := g.Push(ctx, repo, req); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
}

// ext:: runs a command named in the URL. A hostile remote URL in a repository's
// config must not get that far, and file:// style remotes still work.
func TestExtTransportIsNotAllowed(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "pwned")
	run(t, repo, "remote", "add", "evil", "ext::sh -c 'touch "+marker+"'")
	tip := sha(t, repo, "HEAD")

	res, err := g.Push(ctx, repo, PushRequest{Remote: "evil", Branch: "main", Sha: tip})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome == domain.OutcomeDone {
		t.Error("a push over ext:: must not succeed")
	}
	if f, _ := g.Fetch(ctx, repo, "evil"); f.Outcome == domain.OutcomeDone {
		t.Error("a fetch over ext:: must not succeed")
	}
	if exists(marker) {
		t.Fatal("the ext:: transport ran a command")
	}
}

// Repository hooks are the repository's code; pushing and merging from a phone
// must not run them.
func TestHooksAreNotRun(t *testing.T) {
	repo, _ := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	// Set up first: the hooks fail on purpose and would break the setup's own git commands.
	run(t, repo, "checkout", "-q", "-b", "feat")
	tip := gitOut(t, repo, "commit-tree", "-p", "HEAD", "-m", "x", "HEAD^{tree}")
	run(t, repo, "update-ref", "refs/heads/feat", tip)
	run(t, repo, "checkout", "-q", "main")
	for _, h := range []string{"pre-push", "post-merge", "pre-merge-commit", "commit-msg", "post-commit", "post-checkout", "pre-auto-gc"} {
		p := filepath.Join(repo, ".git", "hooks", h)
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+h+" >> "+marker+"\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if res, _ := g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "feat", Sha: tip}); res.Outcome != domain.OutcomeDone {
		t.Fatalf("push = %+v (a failing pre-push hook must not matter)", res)
	}
	if res, err := g.Merge(ctx, repo, MergeRequest{Sha: tip, Message: "m", Strategy: domain.MergeCommit}); err != nil || res.Outcome != domain.OutcomeDone {
		t.Fatalf("merge = %+v, %v (a failing merge hook must not matter)", res, err)
	}
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("hooks ran: %s", b)
	}
}

func TestDeleteRemoteBranchLease(t *testing.T) {
	repo, remote := repoWithRemote(t)
	g := &CLI{}
	ctx := context.Background()
	run(t, repo, "checkout", "-q", "-b", "gone-soon")
	tip := commit(t, repo, "g.txt", "g\n", "g")
	run(t, repo, "push", "-q", "origin", "gone-soon")

	// Someone pushes another commit after we looked: the deletion must be refused.
	other, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, other, "clone", "-q", "-b", "gone-soon", remote, "o")
	clone := filepath.Join(other, "o")
	newer := commit(t, clone, "g.txt", "g2\n", "someone else's commit")
	run(t, clone, "push", "-q", "origin", "gone-soon")

	res, err := g.DeleteRemoteBranch(ctx, repo, "origin", "gone-soon", tip)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome == domain.OutcomeDone {
		t.Fatal("deleted a remote branch that had moved since it was inspected")
	}
	if got := gitOut(t, remote, "rev-parse", "refs/heads/gone-soon"); got != newer {
		t.Fatalf("the remote branch is %s, want %s", got, newer)
	}
	res, _ = g.DeleteRemoteBranch(ctx, repo, "origin", "gone-soon", newer)
	if res.Outcome != domain.OutcomeDone {
		t.Fatalf("delete at the right commit = %+v", res)
	}
	if _, found, _ := g.RemoteBranch(ctx, repo, "origin", "gone-soon"); found {
		t.Error("the remote branch is still there")
	}
}

func TestMergeCommitAndFastForward(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	before := sha(t, repo, "main")
	run(t, repo, "checkout", "-q", "-b", "feat")
	tip := commit(t, repo, "f.txt", "f\n", "feature")
	run(t, repo, "checkout", "-q", "main")

	res, err := g.Merge(ctx, repo, MergeRequest{Sha: tip, Message: "Merge branch 'feat'", Strategy: domain.MergeFastForward})
	if err != nil || res.Outcome != domain.OutcomeDone || res.After != tip || res.Before != before {
		t.Fatalf("ff merge = %+v, %v", res, err)
	}
	if ok, _ := g.IsAncestor(ctx, repo, tip, "refs/heads/main"); !ok {
		t.Error("the branch is not merged after a successful merge")
	}

	// Already merged: nothing to do, and said so.
	res, _ = g.Merge(ctx, repo, MergeRequest{Sha: tip, Strategy: domain.MergeCommit})
	if res.Outcome != domain.OutcomeNoop {
		t.Errorf("merging an ancestor = %+v, want noop", res)
	}

	// A true merge makes a merge commit with the message given.
	run(t, repo, "checkout", "-q", "-b", "feat2", "HEAD")
	tip2 := commit(t, repo, "g.txt", "g\n", "second feature")
	run(t, repo, "checkout", "-q", "main")
	commit(t, repo, "h.txt", "h\n", "main moved on")
	res, err = g.Merge(ctx, repo, MergeRequest{Sha: tip2, Message: "Merge branch 'feat2'\n\nTask: add g", Strategy: domain.MergeCommit})
	if err != nil || res.Outcome != domain.OutcomeDone || res.After == tip2 {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if n := gitOut(t, repo, "rev-list", "--parents", "-n1", "HEAD"); len(strings.Fields(n)) != 3 {
		t.Errorf("not a merge commit: %s", n)
	}
	if subj := gitOut(t, repo, "log", "-1", "--format=%s"); subj != "Merge branch 'feat2'" {
		t.Errorf("subject = %q", subj)
	}
}

func TestMergeFastForwardOnlyRefusesDivergence(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	run(t, repo, "checkout", "-q", "-b", "feat")
	tip := commit(t, repo, "f.txt", "f\n", "feature")
	run(t, repo, "checkout", "-q", "main")
	mainTip := commit(t, repo, "m.txt", "m\n", "main moved")

	res, err := g.Merge(context.Background(), repo, MergeRequest{Sha: tip, Strategy: domain.MergeFastForward})
	if err != nil || res.Outcome != domain.OutcomeFailed || !res.Undone || sha(t, repo, "main") != mainTip {
		t.Fatalf("ff-only on diverged = %+v, %v", res, err)
	}
}

// A merge that conflicts is aborted: the checkout is exactly as it was.
func TestMergeConflictIsUndone(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	commit(t, repo, "c.txt", "base\n", "base")
	run(t, repo, "checkout", "-q", "-b", "feat")
	tip := commit(t, repo, "c.txt", "feature\n", "feature edit")
	run(t, repo, "checkout", "-q", "main")
	mainTip := commit(t, repo, "c.txt", "main\n", "main edit")
	write(t, repo, "untracked.txt", "keep me\n")

	res, err := g.Merge(context.Background(), repo, MergeRequest{Sha: tip, Message: "m", Strategy: domain.MergeCommit})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != domain.OutcomeConflict || !res.Undone || len(res.Conflicts) != 1 || res.Conflicts[0] != "c.txt" {
		t.Fatalf("merge = %+v", res)
	}
	if sha(t, repo, "main") != mainTip || res.After != mainTip {
		t.Error("main moved")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "c.txt")); string(b) != "main\n" {
		t.Errorf("c.txt = %q: the working tree was left in the merge's state", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "untracked.txt")); string(b) != "keep me\n" {
		t.Error("an untracked file was destroyed")
	}
	if st := gitOut(t, repo, "status", "--porcelain"); st != "?? untracked.txt" {
		t.Errorf("status after the aborted merge:\n%s", st)
	}
	if exists(filepath.Join(repo, ".git", "MERGE_HEAD")) {
		t.Error("MERGE_HEAD was left behind")
	}
}

func TestMergeThatGitRefusesLeavesCheckoutAlone(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	run(t, repo, "checkout", "-q", "-b", "feat")
	tip := commit(t, repo, "README.md", "changed on the branch\n", "branch edit")
	run(t, repo, "checkout", "-q", "main")
	write(t, repo, "README.md", "my uncommitted work\n")
	mainTip := sha(t, repo, "main")

	res, err := g.Merge(context.Background(), repo, MergeRequest{Sha: tip, Strategy: domain.MergeCommit})
	if err != nil || res.Outcome != domain.OutcomeFailed || !res.Undone {
		t.Fatalf("merge over local changes = %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(b) != "my uncommitted work\n" || sha(t, repo, "main") != mainTip {
		t.Error("uncommitted work was lost or main moved")
	}
}

func TestDeleteBranchAtIsConditional(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	run(t, repo, "branch", "feat")
	old := sha(t, repo, "feat")
	run(t, repo, "checkout", "-q", "feat")
	newer := commit(t, repo, "x", "x", "moved")
	run(t, repo, "checkout", "-q", "main")

	// The branch moved after it was inspected: it is not deleted.
	if err := g.DeleteBranchAt(ctx, repo, "feat", old); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if sha(t, repo, "feat") != newer {
		t.Fatal("the branch was deleted or changed")
	}
	if err := g.DeleteBranchAt(ctx, repo, "feat", newer); err != nil {
		t.Fatal(err)
	}
	if out := gitOut(t, repo, "branch", "--list", "feat"); out != "" {
		t.Errorf("branch still there: %s", out)
	}
	if err := g.DeleteBranchAt(ctx, repo, "feat", newer); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("deleting a branch that is gone = %v", err)
	}
	// The commit is still reachable by its ID: deletion is recoverable.
	if gitOut(t, repo, "cat-file", "-t", newer) != "commit" {
		t.Error("the commit is gone")
	}
}

func TestDeleteBranchAtRefusesCheckedOutBranch(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	main := sha(t, repo, "main")
	if err := g.DeleteBranchAt(ctx, repo, "main", main); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting the checked-out branch: %v", err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	wt := filepath.Join(root, "wt")
	if _, err := g.AddWorktree(ctx, repo, wt, "devboard/in-use-111111", main); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteBranchAt(ctx, repo, "devboard/in-use-111111", main); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting a branch checked out in a linked worktree: %v", err)
	}
	if sha(t, repo, "devboard/in-use-111111") != main {
		t.Fatal("the branch was deleted")
	}
	for _, bad := range []string{"-D", "a..b", "x y", ""} {
		if err := g.DeleteBranchAt(ctx, repo, bad, main); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("branch %q: %v", bad, err)
		}
	}
	if err := g.DeleteBranchAt(ctx, repo, "x", "main"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a name for a sha: %v", err)
	}
}

func TestRemoveCleanWorktree(t *testing.T) {
	repo := plainRepo(t)
	g := &CLI{}
	ctx := context.Background()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	head := sha(t, repo, "HEAD")

	clean := filepath.Join(root, "clean")
	if _, err := g.AddWorktree(ctx, repo, clean, "devboard/clean-111111", head); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveCleanWorktree(ctx, repo, clean); err != nil {
		t.Fatalf("removing a clean worktree: %v", err)
	}
	if exists(clean) {
		t.Error("directory still exists")
	}
	if gitOut(t, repo, "branch", "--list", "devboard/clean-111111") == "" {
		t.Error("the branch must survive removing its worktree")
	}

	// Untracked, modified and staged work each stop the removal.
	for name, dirty := range map[string]func(dir string){
		"untracked": func(dir string) { write(t, dir, "scratch.txt", "mine\n") },
		"modified":  func(dir string) { write(t, dir, "README.md", "edited\n") },
		"staged": func(dir string) {
			write(t, dir, "new.txt", "n\n")
			run(t, dir, "add", "new.txt")
		},
	} {
		dir := filepath.Join(root, name)
		if _, err := g.AddWorktree(ctx, repo, dir, "devboard/"+name+"-222222", head); err != nil {
			t.Fatal(err)
		}
		dirty(dir)
		if err := g.RemoveCleanWorktree(ctx, repo, dir); err == nil {
			t.Errorf("%s: removed a worktree with uncommitted work", name)
		}
		if !exists(dir) {
			t.Errorf("%s: the directory is gone", name)
		}
	}

	// A worktree whose directory is already gone is only forgotten.
	gone := filepath.Join(root, "gone")
	if _, err := g.AddWorktree(ctx, repo, gone, "devboard/gone-333333", head); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveCleanWorktree(ctx, repo, gone); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gitOut(t, repo, "worktree", "list", "--porcelain"), "gone") {
		t.Error("the vanished worktree is still registered")
	}
}

// A git that fails is reported as a failure with its message, never as success,
// and a failing read is an error rather than empty data.
func TestGitCommandFailure(t *testing.T) {
	repo, _ := repoWithRemote(t)
	ctx := context.Background()
	tip := commit(t, repo, "x.txt", "x\n", "x")

	g := &CLI{Binary: gitFails(t, "fatal: simulated failure", "push")}
	res, err := g.Push(ctx, repo, PushRequest{Remote: "origin", Branch: "main", Sha: tip})
	if err != nil || res.Outcome != domain.OutcomeFailed || !strings.Contains(res.Detail, "simulated failure") {
		t.Fatalf("push with a failing git = %+v, %v", res, err)
	}
	g = &CLI{Binary: gitFails(t, "fatal: simulated failure", "for-each-ref")}
	if _, err := g.ListRefs(ctx, repo); err == nil || !strings.Contains(err.Error(), "simulated failure") {
		t.Fatalf("a failing read must be an error, got %v", err)
	}
	g = &CLI{Binary: gitFails(t, "fatal: simulated failure", "merge")}
	run(t, repo, "checkout", "-q", "-b", "side", "HEAD~1")
	sideTip := commit(t, repo, "s.txt", "s\n", "side")
	run(t, repo, "checkout", "-q", "main")
	before := sha(t, repo, "main")
	mres, err := g.Merge(ctx, repo, MergeRequest{Sha: sideTip, Strategy: domain.MergeCommit})
	if err != nil || mres.Outcome != domain.OutcomeFailed || sha(t, repo, "main") != before {
		t.Fatalf("merge with a failing git = %+v, %v", mres, err)
	}
}

func TestRedactTextRemovesCredentials(t *testing.T) {
	in := "fatal: unable to access 'https://user:ghp_secret@github.com/a/b.git/': The requested URL returned error: 403\nremote: ssh://me@corp.com:pw@host/x"
	got := redactText(in)
	if strings.Contains(got, "ghp_secret") || strings.Contains(got, "user:") {
		t.Errorf("credentials survived: %s", got)
	}
	if !strings.Contains(got, "https://github.com/a/b.git") {
		t.Errorf("the URL was mangled: %s", got)
	}
}

func TestClassifyFailures(t *testing.T) {
	cases := map[string]string{
		"fatal: unable to access 'https://x/': Could not resolve host: x":                                                    domain.OutcomeUnavailable,
		"ssh: connect to host x port 22: Connection refused":                                                                 domain.OutcomeUnavailable,
		"fatal: 'x' does not appear to be a git repository":                                                                  domain.OutcomeUnavailable,
		"remote: Permission denied to someone.\nfatal: unable to access 'https://x/': The requested URL returned error: 403": domain.OutcomeAuth,
		"fatal: Authentication failed for 'https://x/'":                                                                      domain.OutcomeAuth,
		"fatal: could not read Username for 'https://x': terminal prompts disabled":                                          domain.OutcomeAuth,
		" ! [rejected]        a -> a (non-fast-forward)\nerror: failed to push some refs":                                    domain.OutcomeRejected,
		" ! [remote rejected] a -> a (protected branch hook declined)":                                                       domain.OutcomeRejected,
		"fatal: something nobody expected":                                                                                   domain.OutcomeFailed,
	}
	for stderr, want := range cases {
		if got := failureResult(&gitError{code: 1, stderr: stderr}, "x").Outcome; got != want {
			t.Errorf("%q → %s, want %s", stderr, got, want)
		}
	}
}
