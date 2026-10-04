package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAddAndRemoveWorktree(t *testing.T) {
	requireGit(t)
	repo := newRepo(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	wt := filepath.Join(root, "prj", "feature")
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		t.Fatal(err)
	}
	g := &CLI{}
	ctx := context.Background()
	head := gitOut(t, repo, "rev-parse", "HEAD")

	added, err := g.AddWorktree(ctx, repo, wt, "devboard/feature", head)
	if err != nil {
		t.Fatal(err)
	}
	if !added.BranchCreated || added.Head != head {
		t.Fatalf("added = %+v, want a new branch at %s", added, head)
	}
	if got := gitOut(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "devboard/feature" {
		t.Fatalf("worktree is on %q", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree has no checkout: %v", err)
	}
	// The user's own checkout is untouched.
	if got := gitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("the user's checkout moved to %q", got)
	}

	// Work in the worktree is thrown away with it, branch stays.
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory still exists: %v", err)
	}
	if got := gitOut(t, repo, "branch", "--list", "devboard/feature"); got == "" {
		t.Fatal("removing a worktree must not delete its branch")
	}
	if got := gitOut(t, repo, "worktree", "list", "--porcelain"); strings.Contains(got, "feature") {
		t.Fatalf("worktree is still registered:\n%s", got)
	}

	// An existing branch is checked out as it is, ignoring the start point.
	again, err := g.AddWorktree(ctx, repo, wt, "devboard/feature", "main")
	if err != nil || again.BranchCreated {
		t.Fatalf("re-adding = %+v, %v; want the existing branch reused", again, err)
	}
	if err := g.RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatal(err)
	}
	if err := g.DeleteBranch(ctx, repo, "devboard/feature"); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, repo, "branch", "--list", "devboard/feature"); got != "" {
		t.Fatalf("branch survived DeleteBranch: %q", got)
	}
}

func TestAddWorktreeRefusals(t *testing.T) {
	requireGit(t)
	repo := newRepo(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	g := &CLI{}
	ctx := context.Background()

	if _, err := g.AddWorktree(ctx, repo, filepath.Join(root, "a"), "-evil", "main"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a branch that looks like an option: err = %v", err)
	}
	if _, err := g.AddWorktree(ctx, repo, "relative/path", "b", "main"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a relative path: err = %v", err)
	}
	if _, err := g.AddWorktree(ctx, repo, filepath.Join(root, "a"), "b", "--upload-pack=x"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a start point that looks like an option: err = %v", err)
	}

	busy := filepath.Join(root, "busy")
	if err := os.MkdirAll(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "mine.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddWorktree(ctx, repo, busy, "busy-branch", "main"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a directory with files in it: err = %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(busy, "mine.txt")); err != nil || string(b) != "x" {
		t.Fatal("the refused directory was modified")
	}
	if got := gitOut(t, repo, "branch", "--list", "busy-branch"); got != "" {
		t.Fatalf("a refused add must not leave a branch behind: %q", got)
	}

	// The main checkout already has main checked out.
	if _, err := g.AddWorktree(ctx, repo, filepath.Join(root, "second"), "main", "main"); !errors.Is(err, ErrBranchCheckedOut) {
		t.Errorf("a branch checked out elsewhere: err = %v", err)
	}
	if _, err := g.AddWorktree(ctx, repo, filepath.Join(root, "ghost"), "ghost", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err == nil {
		t.Error("an unknown start point must fail")
	}
}

func TestRemoveWorktreeThatIsAlreadyGone(t *testing.T) {
	requireGit(t)
	repo := newRepo(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	wt := filepath.Join(root, "gone")
	g := &CLI{}
	ctx := context.Background()
	if _, err := g.AddWorktree(ctx, repo, wt, "gone-branch", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt); err != nil { // the user deleted it by hand
		t.Fatal(err)
	}
	if err := g.RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatalf("a vanished worktree should just be forgotten: %v", err)
	}
	if got := gitOut(t, repo, "worktree", "list", "--porcelain"); strings.Contains(got, "gone") {
		t.Fatalf("stale registration remains:\n%s", got)
	}
}

// Hooks belong to the repository, so a hostile or merely chatty one must not
// run just because a worktree was created.
func TestAddWorktreeDoesNotRunRepositoryHooks(t *testing.T) {
	requireGit(t)
	repo := newRepo(t)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if _, err := (&CLI{}).AddWorktree(context.Background(), repo, filepath.Join(root, "w"), "hooked", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's post-checkout hook ran")
	}
}
