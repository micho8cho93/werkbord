package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
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

		// Userinfo ends at the LAST '@' of the authority. Cutting at the first
		// one leaked the secret when the username is an email address or the
		// password contains an '@'.
		"https://me@corp.com:s3cr3t@git.corp.com/r.git": "https://git.corp.com/r.git",
		"https://user:p@ss@host/r.git":                  "https://host/r.git",
		"http://tok@host:8080/r.git":                    "http://host:8080/r.git",
		"ftp://u:p@host/r.git":                          "ftp://host/r.git",
		// Other schemes keep the user name (not a secret) but never a password.
		"ssh://user:hunter2@host/r.git":       "ssh://user@host/r.git",
		"git+ssh://me@corp.com:pw@host/r.git": "git+ssh://me@corp.com@host/r.git",
		// '@' outside the authority is just part of a path or query.
		"https://host/r.git?x=a@b":  "https://host/r.git?x=a@b",
		"https://host:8443/a/b.git": "https://host:8443/a/b.git",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// hostileRepo returns a repository that looks like a partial clone whose HEAD
// commit is missing. Reading HEAD then makes Git "lazily fetch" the commit from
// its promisor remote, and the remote's transport is a command chosen by the
// repository's author. marker is created if that command ever runs.
func hostileRepo(t *testing.T, route string) (dir, marker string) {
	t.Helper()
	dir = newRepo(t)
	marker = filepath.Join(t.TempDir(), "executed")
	payload := "sh -c 'echo ran > " + marker + "; exit 1' --"

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))

	run(t, dir, "config", "core.repositoryformatversion", "1")
	run(t, dir, "config", "extensions.partialClone", "origin")
	run(t, dir, "config", "remote.origin.promisor", "true")
	switch route {
	case "ssh":
		run(t, dir, "config", "remote.origin.url", "ssh://attacker.invalid/r.git")
		run(t, dir, "config", "core.sshCommand", payload)
	case "uploadpack":
		bare := t.TempDir()
		run(t, bare, "init", "-q", "--bare")
		run(t, dir, "config", "remote.origin.url", bare)
		run(t, dir, "config", "remote.origin.uploadpack", payload)
	default:
		t.Fatalf("unknown route %q", route)
	}
	if err := os.Remove(filepath.Join(dir, ".git", "objects", head[:2], head[2:])); err != nil {
		t.Fatal(err)
	}
	return dir, marker
}

// withoutKeys returns env minus the named variables.
func withoutKeys(env []string, keys ...string) []string {
	out := env[:0:0]
outer:
	for _, kv := range env {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// TestInspectNeverExecutesRepositoryConfig is the regression test for the
// lazy-fetch code execution bug: inspecting a repository must not run any
// command named in that repository's own configuration.
func TestInspectNeverExecutesRepositoryConfig(t *testing.T) {
	requireGit(t)
	for _, route := range []string{"ssh", "uploadpack"} {
		t.Run(route, func(t *testing.T) {
			dir, marker := hostileRepo(t, route)

			// Control: prove the trap is live on this machine's Git. Without
			// it a passing test could simply mean Git never took the path.
			ctl := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "HEAD^{commit}")
			ctl.Env = append(withoutKeys(os.Environ(), "GIT_ALLOW_PROTOCOL"), "GIT_NO_LAZY_FETCH=0", "GIT_TERMINAL_PROMPT=0")
			_ = ctl.Run()
			if _, err := os.Stat(marker); err != nil {
				t.Skipf("this Git does not lazily fetch for %s; nothing to prove", route)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}

			// The controller's own environment must not be able to re-enable
			// what Inspect turns off.
			t.Setenv("GIT_NO_LAZY_FETCH", "0")
			t.Setenv("GIT_ALLOW_PROTOCOL", "ssh:file")

			_, err := (&CLI{}).Inspect(context.Background(), dir)
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Fatalf("Inspect executed a command from the repository's config (Inspect returned %v)", err)
			}
		})
	}
}

// TestGitEnvOverridesInheritedSettings checks both layers of the defence
// independently: GIT_NO_LAZY_FETCH (Git 2.44+) and GIT_ALLOW_PROTOCOL (older
// Git). Values inherited from the controller's environment must lose.
func TestGitEnvOverridesInheritedSettings(t *testing.T) {
	cmd := exec.Command("git")
	cmd.Env = gitEnv([]string{"GIT_NO_LAZY_FETCH=0", "GIT_ALLOW_PROTOCOL=ssh:file:http", "GIT_OPTIONAL_LOCKS=1", "PATH=/usr/bin"})
	got := map[string]string{}
	for _, kv := range cmd.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	want := map[string]string{
		"GIT_NO_LAZY_FETCH":   "1",
		"GIT_ALLOW_PROTOCOL":  "none",
		"GIT_OPTIONAL_LOCKS":  "0",
		"GIT_TERMINAL_PROMPT": "0",
		"PATH":                "/usr/bin", // unrelated variables are preserved
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// TestInspectLegitimatePartialClone guards against over-hardening: a genuine
// partial clone has its commits locally and must still be inspectable.
func TestInspectLegitimatePartialClone(t *testing.T) {
	requireGit(t)
	src := newRepo(t)
	run(t, src, "config", "uploadpack.allowFilter", "true")

	parent := t.TempDir()
	clone := exec.Command("git", "-C", parent, "clone", "-q", "--no-local", "--filter=blob:none", "file://"+src, "dst")
	if out, err := clone.CombinedOutput(); err != nil {
		t.Skipf("this Git cannot make a partial clone: %v\n%s", err, out)
	}

	repo, err := (&CLI{}).Inspect(context.Background(), filepath.Join(parent, "dst"))
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.HeadCommit) < 40 || repo.CurrentBranch != "main" {
		t.Errorf("partial clone not inspected correctly: %+v", repo)
	}
}

// commitAll makes another empty commit so two repositories never share a HEAD.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestInspectIgnoresInheritedRepositoryEnv: GIT_DIR and friends select a
// repository on their own. If the controller was started from a Git hook or
// `git rebase --exec` they are set, and Inspect used to describe that other
// repository (and accept a plain directory as a project).
func TestInspectIgnoresInheritedRepositoryEnv(t *testing.T) {
	requireGit(t)
	other, mine, plain := newRepo(t), newRepo(t), t.TempDir()
	commitAll(t, other, "only in other")
	wantHead := headOf(t, mine)
	if headOf(t, other) == wantHead {
		t.Fatal("test setup: repositories must have different HEADs")
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)

	if _, err := (&CLI{}).Inspect(context.Background(), plain); !errors.Is(err, ErrNotRepository) {
		t.Errorf("plain directory: err = %v, want ErrNotRepository", err)
	}
	repo, err := (&CLI{}).Inspect(context.Background(), mine)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(mine)
	if repo.RootPath != wantRoot || repo.HeadCommit != wantHead {
		t.Errorf("Inspect(%s) = root %s head %.7s; want root %s head %.7s (the environment's repository leaked in)",
			mine, repo.RootPath, repo.HeadCommit, wantRoot, wantHead)
	}
}

// TestGitEnvDropsRepositoryLocalVariables cross-checks our list against the
// installed Git's own, so a Git release that adds a variable fails here
// instead of silently reopening the hole.
func TestGitEnvDropsRepositoryLocalVariables(t *testing.T) {
	requireGit(t)
	out, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		t.Skipf("git rev-parse --local-env-vars: %v", err)
	}
	base := []string{"PATH=/usr/bin", "GIT_CONFIG_GLOBAL=/keep/me", "GIT_EXEC_PATH=/keep/exec", "GIT_CONFIG_KEY_0=core.sshCommand", "GIT_CONFIG_VALUE_0=evil", "GIT_NAMESPACE=ns"}
	local := strings.Fields(string(out))
	for _, name := range local {
		base = append(base, name+"=x")
	}

	cmd := exec.Command("git")
	cmd.Env = gitEnv(base)
	got := map[string]string{}
	for _, kv := range cmd.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for _, name := range append(local, "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_NAMESPACE") {
		if v, ok := got[name]; ok {
			t.Errorf("%s=%q was passed to git; it selects or reconfigures a repository", name, v)
		}
	}
	// Variables that describe the user's setup rather than a repository stay.
	for k, v := range map[string]string{"PATH": "/usr/bin", "GIT_CONFIG_GLOBAL": "/keep/me", "GIT_EXEC_PATH": "/keep/exec"} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (must be preserved)", k, got[k], v)
		}
	}
}

// TestInspectRejectsMissingHeadObject: a repository whose HEAD names an object
// that is not there is damaged, not "empty". Inspect used to report it as a
// repository with no commits.
func TestInspectRejectsMissingHeadObject(t *testing.T) {
	requireGit(t)
	for _, detach := range []bool{false, true} {
		name := "branch"
		if detach {
			name = "detached"
		}
		t.Run(name, func(t *testing.T) {
			dir := newRepo(t)
			if detach {
				run(t, dir, "checkout", "-q", "--detach")
			}
			head := headOf(t, dir)
			if err := os.Remove(filepath.Join(dir, ".git", "objects", head[:2], head[2:])); err != nil {
				t.Fatal(err)
			}
			repo, err := (&CLI{}).Inspect(context.Background(), dir)
			if !errors.Is(err, ErrUninspectable) {
				t.Fatalf("err = %v (repo %+v), want ErrUninspectable", err, repo)
			}
			if !strings.Contains(err.Error(), head) {
				t.Errorf("error should name the missing object %s: %v", head, err)
			}
		})
	}
}

// TestInspectReportsCommonDir: a repository's main checkout, any subdirectory
// of it and every linked worktree must report the same CommonDir, because that
// (not RootPath) is what identifies the repository.
func TestInspectReportsCommonDir(t *testing.T) {
	requireGit(t)
	main := newRepo(t)
	sub := filepath.Join(main, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "worktree", "add", "-q", linked, "-b", "feature")
	other := newRepo(t)

	wantRealMain, _ := filepath.EvalSymlinks(main)
	want := filepath.Join(wantRealMain, ".git")
	for name, path := range map[string]string{"main": main, "subdirectory": sub, "linked worktree": linked} {
		repo, err := (&CLI{}).Inspect(context.Background(), path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if repo.CommonDir != want {
			t.Errorf("%s: CommonDir = %q, want %q", name, repo.CommonDir, want)
		}
	}
	linkedRepo, _ := (&CLI{}).Inspect(context.Background(), linked)
	mainRepo, _ := (&CLI{}).Inspect(context.Background(), main)
	if linkedRepo.RootPath == mainRepo.RootPath {
		t.Errorf("a linked worktree has its own RootPath; both are %s", mainRepo.RootPath)
	}
	otherRepo, _ := (&CLI{}).Inspect(context.Background(), other)
	if otherRepo.CommonDir == want {
		t.Errorf("an unrelated repository must not share CommonDir %s", want)
	}
}

// fakeBinary writes an executable shell script and returns its path, standing
// in for git so the process-handling code can be driven deterministically.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakegit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGitReturnsAfterTimeoutEvenIfAChildSurvives: on timeout only git itself
// is killed. A process git started (a stuck ssh, credential helper or pager)
// survives and keeps stdout/stderr open, and exec.Cmd waits for those pipes
// without limit, so one stuck child hung the request far past its timeout.
func TestGitReturnsAfterTimeoutEvenIfAChildSurvives(t *testing.T) {
	bin := fakeBinary(t, "sleep 8\ntrue\n") // sleep is a child of the script, as ssh is of git
	c := &CLI{Binary: bin, Timeout: 300 * time.Millisecond}

	start := time.Now()
	_, err := c.git(context.Background(), t.TempDir(), "rev-parse")
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("git blocked for %v although its timeout is %v: a surviving child held the output open", took.Round(time.Millisecond), c.Timeout)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded so a timeout is distinguishable from a git failure", err)
	}
}

// TestGitCapsOutput: output is held in memory, and a repository can make git
// print without bound (a huge config, a hostile tool on PATH).
func TestGitCapsOutput(t *testing.T) {
	bin := fakeBinary(t, "head -c 8388608 /dev/zero\n")
	c := &CLI{Binary: bin}

	start := time.Now()
	out, err := c.git(context.Background(), t.TempDir(), "config", "--list")
	if err == nil {
		t.Fatalf("8 MiB of output was accepted (%d bytes)", len(out))
	}
	if !strings.Contains(err.Error(), "output") {
		t.Errorf("error should say the output was too large: %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v; the process should be stopped as soon as the cap is hit", took)
	}
}

// TestGitBoundsConcurrentProcesses: every request starts git processes, so an
// unbounded number of requests must not mean an unbounded number of processes.
func TestGitBoundsConcurrentProcesses(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	bin := fakeBinary(t, fmt.Sprintf("echo start >> %[1]s\nsleep 0.4\necho end >> %[1]s\n", log))
	c := &CLI{Binary: bin}

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.git(context.Background(), t.TempDir(), "rev-parse")
		}()
	}
	wg.Wait()

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	running, peak := 0, 0
	for _, line := range strings.Fields(string(raw)) {
		if line == "start" {
			running++
			peak = max(peak, running)
		} else {
			running--
		}
	}
	if peak > 4 {
		t.Errorf("%d git processes ran at once; want at most 4", peak)
	}
}

// TestGitWaitingForASlotHonoursContext: a request queued behind busy slots
// must give up when its caller does, rather than start git afterwards.
func TestGitWaitingForASlotHonoursContext(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	release := filepath.Join(filepath.Dir(log), "release")
	bin := fakeBinary(t, fmt.Sprintf("echo start >> %s\nwhile [ ! -f %s ]; do sleep 0.01; done\n", log, release))
	c := &CLI{Binary: bin, MaxProcs: 1}

	done := make(chan struct{})
	root := t.TempDir()
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer firstCancel()
	go func() { defer close(done); _, _ = c.git(firstCtx, root, "rev-parse") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if raw, _ := os.ReadFile(log); strings.Contains(string(raw), "start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first Git process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer os.WriteFile(release, nil, 0600)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := c.git(ctx, t.TempDir(), "rev-parse"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first Git process did not finish")
	}
	if raw, _ := os.ReadFile(log); strings.Count(string(raw), "start") != 1 {
		t.Errorf("git was started %d times, want 1:\n%s", strings.Count(string(raw), "start"), raw)
	}
}

// TestRefNameValidationIsNeverLooserThanGit: domain.ValidateRefName gates the
// branch and base ref that will be passed to Git. It may be stricter than Git
// (it also refuses a leading '-'), but a name Git rejects must never pass.
func TestRefNameValidationIsNeverLooserThanGit(t *testing.T) {
	requireGit(t)
	specials := []string{".", "..", "/", "//", "@", "@{", "{", "}", "~", "^", ":", "?", "*", "[", `\`, " ", "\t", "\x7f", "\x01", ".lock", "'", "\""}
	var names []string
	for _, sp := range specials {
		names = append(names, sp, "a"+sp, sp+"a", "a"+sp+"b", "a/"+sp, sp+"/a", "a/"+sp+"b", "a"+sp+"/b")
	}
	names = append(names, "main", "feature/x", "a.lock/b", "a/b.lock", "HEAD", "a/@", "@/a", "a/.", "a/b.", "refs/heads/x", "a.b", "a-b", "-")

	checked := 0
	for _, name := range names {
		if strings.HasPrefix(name, "-") {
			continue // `git check-ref-format -x` would read it as an option; we refuse it by design
		}
		gitAccepts := exec.Command("git", "check-ref-format", "--allow-onelevel", name).Run() == nil
		if !gitAccepts && domain.ValidateRefName(name) == nil {
			t.Errorf("ValidateRefName(%q) accepted a name Git rejects", name)
		}
		checked++
	}
	if checked < 100 {
		t.Errorf("only %d names were compared; the corpus is broken", checked)
	}
}
