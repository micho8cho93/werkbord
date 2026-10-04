package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/gitrepo"
)

// These tests run the real Git CLI: the bugs they cover live in how project
// identity is derived from what Git reports, which a fake would hide.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
}

func realGitFixture(t *testing.T) *fixture {
	t.Helper()
	requireGit(t)
	f := newFixture(t)
	f.projects.Git = &gitrepo.CLI{}
	return f
}

// TestRefreshRefusesToFollowADifferentRepository: if a registered directory is
// deleted and recreated empty inside another repository (a dotfiles repo in
// $HOME is the common case), Git resolves it to the OUTER repository. Refresh
// used to store that snapshot, so the project silently described another repo.
func TestRefreshRefusesToFollowADifferentRepository(t *testing.T) {
	f := realGitFixture(t)
	ctx := context.Background()
	outer := t.TempDir()
	initRepo(t, outer)
	app := filepath.Join(outer, "code", "app")
	initRepo(t, app)

	p, err := f.projects.Register(ctx, app, "app")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(app); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := f.projects.Refresh(ctx, p.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Refresh err = %v, want ErrConflict", err)
	}
	got, err := f.projects.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository.RootPath != p.RepoPath || got.Repository.HeadCommit != p.Repository.HeadCommit {
		t.Errorf("snapshot was overwritten: root %s head %.7s, want root %s head %.7s",
			got.Repository.RootPath, got.Repository.HeadCommit, p.RepoPath, p.Repository.HeadCommit)
	}
}

// TestRegisterRejectsASecondCheckoutOfARegisteredRepository: a linked worktree
// has its own top-level path but is the same repository. Identity must come
// from the shared Git directory, not the path, in either registration order.
func TestRegisterRejectsASecondCheckoutOfARegisteredRepository(t *testing.T) {
	for _, mainFirst := range []bool{true, false} {
		name := "main first"
		if !mainFirst {
			name = "linked first"
		}
		t.Run(name, func(t *testing.T) {
			f := realGitFixture(t)
			ctx := context.Background()
			main := filepath.Join(t.TempDir(), "main")
			initRepo(t, main)
			linked := filepath.Join(t.TempDir(), "linked")
			git(t, main, "worktree", "add", "-q", linked, "-b", "feature")

			first, second := main, linked
			if !mainFirst {
				first, second = linked, main
			}
			if _, err := f.projects.Register(ctx, first, "first"); err != nil {
				t.Fatal(err)
			}
			_, err := f.projects.Register(ctx, second, "second")
			if !errors.Is(err, domain.ErrDuplicate) {
				t.Fatalf("second checkout: err = %v, want ErrDuplicate", err)
			}
			if err != nil && !strings.Contains(err.Error(), "first") {
				t.Errorf("error should name the project it duplicates: %v", err)
			}
			if list, _ := f.projects.List(ctx); len(list) != 1 {
				t.Errorf("projects = %d, want 1", len(list))
			}
		})
	}
}
