package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func run(t *testing.T, dir string, args ...string) {
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

// newRepo creates a repository with one commit and an origin remote.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "init")
	run(t, dir, "remote", "add", "origin", "https://user:secret@example.com/acme/demo.git")
	return dir
}

func TestInspectRepository(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := (&CLI{}).Inspect(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(dir)
	if repo.RootPath != wantRoot {
		t.Errorf("RootPath = %q, want %q", repo.RootPath, wantRoot)
	}
	if repo.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q", repo.CurrentBranch)
	}
	if len(repo.HeadCommit) < 40 {
		t.Errorf("HeadCommit = %q", repo.HeadCommit)
	}
	if len(repo.Remotes) != 1 || repo.Remotes[0].Name != "origin" || repo.Remotes[0].URL != "https://example.com/acme/demo.git" {
		t.Errorf("Remotes = %+v (credentials must be redacted)", repo.Remotes)
	}
}

func TestInspectEmptyRepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "trunk")
	repo, err := (&CLI{}).Inspect(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if repo.HeadCommit != "" || repo.CurrentBranch != "trunk" || len(repo.Remotes) != 0 {
		t.Errorf("unexpected metadata for empty repo: %+v", repo)
	}
}

func TestInspectRejectsInvalidPaths(t *testing.T) {
	requireGit(t)
	plain := t.TempDir()
	file := filepath.Join(plain, "f.txt")
	_ = os.WriteFile(file, nil, 0o644)
	bare := t.TempDir()
	run(t, bare, "init", "-q", "--bare")

	cases := map[string]struct {
		path string
		want error
	}{
		"missing":    {filepath.Join(plain, "nope"), ErrPathNotFound},
		"file":       {file, ErrNotDirectory},
		"not a repo": {plain, ErrNotRepository},
		"bare":       {bare, ErrBare},
		"empty":      {"", nil},
	}
	for name, c := range cases {
		_, err := (&CLI{}).Inspect(context.Background(), c.path)
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://tok@github.com/a/b.git": "https://github.com/a/b.git",
		"https://u:p@github.com/a/b.git": "https://github.com/a/b.git",
		"https://github.com/a/b@c.git":   "https://github.com/a/b@c.git",
		"git@github.com:a/b.git":         "git@github.com:a/b.git",
		"ssh://git@github.com/a/b.git":   "ssh://git@github.com/a/b.git",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}
