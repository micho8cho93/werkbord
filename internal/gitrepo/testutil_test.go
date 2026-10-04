package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGit gives the test its own Git identity and no user or system config,
// for commands the CLI under test runs itself (they inherit this process's
// environment): a developer's commit.gpgsign, hooks path or aliases must not
// decide whether a test passes.
func isolateGit(t *testing.T) {
	t.Helper()
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit writes a file and commits it, returning the commit.
func commit(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	write(t, dir, name, content)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// plainRepo is a repository on main with one commit, no remote.
func plainRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	run(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "README.md", "hi\n", "init")
	return dir
}

// repoWithRemote is a working repository whose origin is a real bare repository
// on disk, with main pushed and origin/HEAD set.
func repoWithRemote(t *testing.T) (repo, remote string) {
	t.Helper()
	repo = plainRepo(t)
	remote, _ = filepath.EvalSymlinks(t.TempDir())
	run(t, remote, "init", "-q", "--bare", "-b", "main")
	run(t, repo, "remote", "add", "origin", remote)
	run(t, repo, "push", "-q", "-u", "origin", "main")
	run(t, repo, "remote", "set-head", "origin", "main")
	return repo, remote
}

func sha(t *testing.T, dir, rev string) string {
	t.Helper()
	return gitOut(t, dir, "rev-parse", rev)
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

// gitFails is a stand-in git executable: it runs the real one unless the
// subcommand is in failOn, in which case it prints msg to stderr and exits 1.
func gitFails(t *testing.T, msg string, failOn ...string) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	var cases strings.Builder
	for _, f := range failOn {
		cases.WriteString("    " + f + ") echo '" + msg + "' >&2; exit 1;;\n")
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n  case \"$a\" in\n" + cases.String() + "  esac\ndone\n" +
		"exec " + real + " \"$@\"\n"
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	return cmd
}
