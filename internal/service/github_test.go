package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/github"
	"devboard/internal/github/ghtest"
	"devboard/internal/gitrepo"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ghFixture is a GitHubSetup over a fake gh, a real database and a home
// directory with some clones in it.
type ghFixture struct {
	*fixture
	gh    *ghtest.Fake
	setup *GitHubSetup
	home  string
}

func newGHFixture(t *testing.T) *ghFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	f := newFixture(t)
	f.projects.Git = &gitrepo.CLI{}
	gh := ghtest.New(t)
	cli := &github.CLI{Binary: gh.Binary}
	home, _ := filepath.EvalSymlinks(t.TempDir())
	return &ghFixture{fixture: f, gh: gh, home: home, setup: &GitHubSetup{
		Deps: f.deps, CLI: cli, Login: &github.LoginSession{CLI: cli}, Projects: f.projects, Home: home,
	}}
}

// repoAt makes a real repository with a GitHub remote.
func (g *ghFixture) repoAt(t *testing.T, dir, remote string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", remote)
	real, _ := filepath.EvalSymlinks(dir)
	return real
}

const twoRepos = `[
 {"full_name":"octo/app","name":"app","private":true,"default_branch":"main","pushed_at":"2026-09-01T10:00:00Z","html_url":"https://github.com/octo/app","owner":{"login":"octo"},"permissions":{"push":true}},
 {"full_name":"octo/remote-only","name":"remote-only","pushed_at":"2026-09-20T10:00:00Z","html_url":"https://github.com/octo/remote-only","owner":{"login":"octo"},"permissions":{"push":true}},
 {"full_name":"octo/already","name":"already","pushed_at":"2026-08-01T10:00:00Z","html_url":"https://github.com/octo/already","owner":{"login":"octo"},"permissions":{"push":true}}]`

func TestGitHubStatusThroughItsStates(t *testing.T) {
	g := newGHFixture(t)
	ctx := context.Background()

	// Signed out: the user is offered to connect.
	if st := g.setup.Status(ctx); st.State != GitHubSignedOut || st.Account != nil || st.Version != "2.86.0" {
		t.Fatalf("signed out = %+v", st)
	}
	// Signing in: no calls to GitHub while the user is on its page.
	st, err := g.setup.StartLogin(ctx)
	if err != nil || st.Code != "ABCD-1234" {
		t.Fatalf("login = %+v, %v", st, err)
	}
	callsBefore := strings.Count(g.gh.Calls(), "\n")
	if s := g.setup.Status(ctx); s.State != GitHubSigningIn || s.Login.Code != "ABCD-1234" || s.Login.URL == "" {
		t.Fatalf("signing in = %+v", s)
	}
	if strings.Count(g.gh.Calls(), "\n") != callsBefore {
		t.Fatalf("polling during sign-in called gh:\n%s", g.gh.Calls())
	}
	// Approved: connected, and who as.
	g.gh.Approve()
	deadline := time.Now().Add(10 * time.Second)
	var s GitHubStatus
	for time.Now().Before(deadline) {
		if s = g.setup.Status(ctx); s.State == GitHubSignedIn {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.State != GitHubSignedIn || s.Account == nil || s.Account.Login != "octo" {
		t.Fatalf("connected = %+v", s)
	}

	// GitHub unreachable is an error that says so, not "signed out".
	g.gh.Offline(true)
	g.setup.forgetAccount()
	if s := g.setup.Status(ctx); s.State != GitHubError || !strings.Contains(s.Message, "could not be reached") {
		t.Fatalf("offline = %+v", s)
	}
}

func TestGitHubStatusWhenMissingOrTurnedOff(t *testing.T) {
	g := newGHFixture(t)
	g.setup.CLI = &github.CLI{Binary: filepath.Join(t.TempDir(), "no-gh")}
	g.setup.Login = &github.LoginSession{CLI: g.setup.CLI}
	st := g.setup.Status(context.Background())
	if st.State != GitHubMissing || !strings.Contains(st.Guidance, "cli.github.com") || !strings.Contains(st.Guidance, "skip") {
		t.Fatalf("missing = %+v: it should say how to install it, and that it can be skipped", st)
	}
	if _, err := g.setup.StartLogin(context.Background()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("signing in without gh: %v", err)
	}

	off := &GitHubSetup{Deps: g.deps, Projects: g.projects}
	if st := off.Status(context.Background()); st.State != GitHubDisabled {
		t.Fatalf("disabled = %+v", st)
	}
	if _, err := off.Repositories(context.Background()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("repositories when off: %v", err)
	}
}

func TestRepositoriesSeparateLocalFromGitHubOnly(t *testing.T) {
	g := newGHFixture(t)
	g.gh.SignIn()
	g.gh.Repos(1, twoRepos)
	ctx := context.Background()

	local := g.repoAt(t, filepath.Join(g.home, "code", "app"), "https://github.com/octo/app.git")
	// Already a project, living outside every place that is searched.
	other := g.repoAt(t, filepath.Join(t.TempDir(), "somewhere", "already"), "git@github.com:octo/already.git")
	if _, err := g.projects.Register(ctx, other, "Already"); err != nil {
		t.Fatal(err)
	}

	list, err := g.setup.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]RepoChoice{}
	for _, r := range list.Repos {
		by[r.FullName] = r
	}
	if list.Account.Login != "octo" || len(list.Repos) != 3 {
		t.Fatalf("list = %+v", list)
	}
	if app := by["octo/app"]; len(app.LocalPaths) != 1 || app.LocalPaths[0] != local || app.Project != nil || !app.Private {
		t.Fatalf("app = %+v: found on this computer, not yet a project", app)
	}
	if ro := by["octo/remote-only"]; len(ro.LocalPaths) != 0 || ro.LocalPaths == nil || ro.Project != nil {
		t.Fatalf("remote-only = %+v: GitHub only, and the list must be [] not null", ro)
	}
	if al := by["octo/already"]; al.Project == nil || al.Project.Name != "Already" || len(al.LocalPaths) != 1 || al.LocalPaths[0] != other {
		t.Fatalf("already = %+v: a project, so it is local too", al)
	}
	// What can be worked on now comes first.
	if list.Repos[len(list.Repos)-1].FullName != "octo/remote-only" && len(list.Repos[0].LocalPaths) == 0 {
		t.Fatalf("order = %v", list.Repos)
	}
	if list.CloneDir != filepath.Join(g.home, "code") {
		t.Fatalf("clone dir = %s: an existing code directory should be used", list.CloneDir)
	}

	// Signed out is an error, not an empty list.
	g.gh.SignOut()
	g.setup.forgetAccount()
	if _, err := g.setup.Repositories(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("signed out: %v", err)
	}
}

func TestAddingALocalCloneRegistersThatCloneOnly(t *testing.T) {
	g := newGHFixture(t)
	ctx := context.Background()
	local := g.repoAt(t, filepath.Join(g.home, "code", "app"), "https://github.com/octo/app.git")
	other := g.repoAt(t, filepath.Join(g.home, "code", "unrelated"), "https://github.com/someone/else.git")

	res, err := g.setup.Add(ctx, "octo/app", local)
	if err != nil || res.Cloned || res.Project.RepoPath != local || res.Project.Name != "app" {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if _, err := g.setup.Add(ctx, "octo/app", local); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("adding twice: %v", err)
	}
	// A path that is not a clone of that repository is refused, however it was found.
	if _, err := g.setup.Add(ctx, "octo/app", other); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("someone else's clone: %v", err)
	}
	if _, err := g.setup.Add(ctx, "octo/app", g.home); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("not a repository: %v", err)
	}
	for _, bad := range []string{"", "octo", "-x/app", "octo/app;rm -rf /", "../a/b"} {
		if _, err := g.setup.Add(ctx, bad, ""); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Add(%q): %v", bad, err)
		}
	}
}

// remoteRepo makes a repository with a commit to clone from, as if it were on GitHub.
func remoteRepo(t *testing.T, base, owner, name string) {
	t.Helper()
	dir := filepath.Join(base, owner, name+".git")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	gitIn(t, src, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-q", "-m", "init")
	if out, err := exec.Command("git", "clone", "-q", "--bare", src, dir).CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v %s", err, out)
	}
}

// pretendGitHub makes git read https://github.com/ from a directory on disk, so a
// clone is exercised end to end without the network.
func pretendGitHub(t *testing.T, base string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.file://"+base+"/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
}

func TestAddingAGitHubOnlyRepositoryClonesItFirst(t *testing.T) {
	g := newGHFixture(t)
	ctx := context.Background()
	remotes := t.TempDir()
	remoteRepo(t, remotes, "octo", "remote-only")
	pretendGitHub(t, remotes)

	res, err := g.setup.Add(ctx, "octo/remote-only", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(g.home, "Code", "remote-only") // no code directory yet: ~/Code
	got, _ := filepath.EvalSymlinks(res.Project.RepoPath)
	if !res.Cloned || got != want {
		t.Fatalf("add = %+v (project path %s), want a clone at %s", res, res.Project.RepoPath, want)
	}
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("the clone has no files: %v", err)
	}
	// The clone is usable for pushing too: the GitHub CLI is its credential helper, set for this repository only.
	out, _ := exec.Command("git", "-C", want, "config", "--local", "credential.helper").Output()
	if !strings.Contains(string(out), "auth git-credential") {
		t.Fatalf("local credential helper = %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(g.home, ".gitconfig")); len(b) != 0 {
		t.Fatalf("the user's own git configuration was changed: %s", b)
	}
	// And it is found as a local clone from now on.
	g.gh.SignIn()
	g.gh.Repos(1, twoRepos)
	list, err := g.setup.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list.Repos {
		if r.FullName == "octo/remote-only" && (r.Project == nil || len(r.LocalPaths) == 0) {
			t.Fatalf("after cloning = %+v", r)
		}
	}
}

func TestCloningNeverOverwritesAndCleansUpAfterAFailure(t *testing.T) {
	g := newGHFixture(t)
	ctx := context.Background()
	remotes := t.TempDir()
	remoteRepo(t, remotes, "octo", "app")
	pretendGitHub(t, remotes)
	root := filepath.Join(g.home, "code")
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "app", "mine.txt")
	_ = os.WriteFile(marker, []byte("keep"), 0o644)

	// A directory is already there: the clone goes next to it, under a name that says whose it is.
	res, err := g.setup.Add(ctx, "octo/app", "")
	if err != nil || !strings.HasSuffix(res.Project.RepoPath, "octo-app") {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "keep" {
		t.Fatal("an existing directory was touched")
	}
	// Both names are taken: refuse, say why.
	if _, err := g.setup.Add(ctx, "octo/app", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("both taken: %v", err)
	}

	// A repository that does not exist: the half-made directory is removed, the error says what git said.
	_, err = g.setup.Add(ctx, "octo/missing", "")
	if !errors.Is(err, domain.ErrGit) || !strings.Contains(err.Error(), "octo/missing") {
		t.Fatalf("missing repository: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a failed clone left a directory behind")
	}
}
