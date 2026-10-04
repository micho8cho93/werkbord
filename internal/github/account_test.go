package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/github/ghtest"
)

var bg = context.Background()

func TestAccountAndVersion(t *testing.T) {
	f := ghtest.New(t)
	cli := &CLI{Binary: f.Binary}

	if v, err := cli.Version(bg); err != nil || v != "2.86.0" {
		t.Fatalf("version = %q, %v", v, err)
	}
	// Signed out is an Error with the reason the app words itself.
	_, err := cli.Account(bg)
	var ge *Error
	if !errors.As(err, &ge) || ge.Reason != domain.GHUnauthenticated {
		t.Fatalf("signed out: %v", err)
	}
	f.SignIn()
	a, err := cli.Account(bg)
	if err != nil || a.Login != "octo" || a.Name != "Octo Cat" || a.Host != "github.com" {
		t.Fatalf("account = %+v, %v", a, err)
	}
	f.Offline(true)
	if _, err := cli.Account(bg); !errors.As(err, &ge) || ge.Reason != domain.GHError {
		t.Fatalf("offline: %v", err)
	}
	// A missing gh is its own reason.
	missing := &CLI{Binary: filepath.Join(t.TempDir(), "no-gh")}
	if _, err := missing.Version(bg); !errors.As(err, &ge) || ge.Reason != domain.GHMissing {
		t.Fatalf("missing: %v", err)
	}
}

func TestRepositoriesAreReadAcrossPagesAndFiltered(t *testing.T) {
	f := ghtest.New(t)
	f.SignIn()
	f.Repos(1, `[
	 {"full_name":"octo/app","name":"app","description":"The app","private":true,"fork":false,"archived":false,"default_branch":"main","pushed_at":"2026-09-01T10:00:00Z","html_url":"https://github.com/octo/app","owner":{"login":"octo"},"permissions":{"push":true}},
	 {"full_name":"--evil/x","name":"x","owner":{"login":"--evil"}},
	 {"full_name":"org/lib","name":"lib","fork":true,"archived":true,"html_url":"https://github.com/org/lib","owner":{"login":"org"},"permissions":{"push":false}}]`)
	cli := &CLI{Binary: f.Binary}
	repos, truncated, err := cli.Repositories(bg)
	if err != nil || truncated {
		t.Fatalf("repos = %+v, truncated %v, %v", repos, truncated, err)
	}
	if len(repos) != 2 || repos[0].FullName != "octo/app" || !repos[0].Private || !repos[0].CanPush || repos[0].DefaultBranch != "main" ||
		repos[0].PushedAt.Year() != 2026 || repos[1].FullName != "org/lib" || !repos[1].Fork || !repos[1].Archived || repos[1].CanPush {
		t.Fatalf("repos = %+v (a name that looks like a flag must be dropped)", repos)
	}
	if !strings.Contains(f.Calls(), "affiliation=owner,collaborator,organization_member") {
		t.Fatalf("it should ask for every repository the user can reach:\n%s", f.Calls())
	}
}

func TestValidRepoName(t *testing.T) {
	for _, ok := range []string{"octo/app", "octo/app.js", "a-b/c_d", "octo/.github", "o/x"} {
		if !ValidRepoName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "octo", "octo/", "/app", "-x/app", "octo/-app", "octo/app/extra", "octo/..", "../x/y", "octo/a b", "octo/a;rm", "octo/a\nb", "a/b/../c", "octo/."} {
		if ValidRepoName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSigningInShowsTheCodeThenCompletes(t *testing.T) {
	f := ghtest.New(t)
	cli := &CLI{Binary: f.Binary}
	ls := &LoginSession{CLI: cli}
	if ls.Status().State != LoginIdle {
		t.Fatalf("idle = %+v", ls.Status())
	}

	l, err := ls.Start(bg)
	if err != nil {
		t.Fatal(err)
	}
	if l.State != LoginPending || l.Code != "ABCD-1234" || l.URL != "https://github.com/login/device" {
		t.Fatalf("login = %+v", l)
	}
	// Starting again while it waits returns the same attempt, not a second process.
	if again, _ := ls.Start(bg); again.Code != "ABCD-1234" || strings.Count(f.Calls(), "auth login") != 1 {
		t.Fatalf("a second start launched another sign-in:\n%s", f.Calls())
	}
	for _, want := range []string{"--hostname github.com", "--web", "--git-protocol https", "--skip-ssh-key"} {
		if !strings.Contains(f.Calls(), want) {
			t.Errorf("gh auth login was not given %q:\n%s", want, f.Calls())
		}
	}

	f.Approve()
	waitLogin(t, ls, LoginDone)
	if a, err := cli.Account(bg); err != nil || a.Login != "octo" {
		t.Fatalf("after signing in = %+v, %v", a, err)
	}
}

func waitLogin(t *testing.T, ls *LoginSession, want LoginState) Login {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l := ls.Status(); l.State == want {
			return l
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("login never reached %s; it is %+v", want, ls.Status())
	return Login{}
}

func TestSigningInCanFailOrBeCancelled(t *testing.T) {
	f := ghtest.New(t)
	ls := &LoginSession{CLI: &CLI{Binary: f.Binary}}
	if _, err := ls.Start(bg); err != nil {
		t.Fatal(err)
	}
	f.Deny()
	l := waitLogin(t, ls, LoginFailed)
	if l.Error == "" {
		t.Fatalf("a failed sign-in must say so: %+v", l)
	}

	// Cancelled: back to idle, and the old attempt cannot overwrite a new one.
	f2 := ghtest.New(t)
	ls2 := &LoginSession{CLI: &CLI{Binary: f2.Binary}}
	if _, err := ls2.Start(bg); err != nil {
		t.Fatal(err)
	}
	ls2.Cancel()
	if l := ls2.Status(); l.State != LoginIdle {
		t.Fatalf("after cancel = %+v", l)
	}
	time.Sleep(200 * time.Millisecond)
	if l := ls2.Status(); l.State != LoginIdle {
		t.Fatalf("the cancelled attempt wrote %+v", l)
	}
	l2, err := ls2.Start(bg)
	if err != nil || l2.State != LoginPending {
		t.Fatalf("restart = %+v, %v", l2, err)
	}
	ls2.Cancel()
}

func TestSigningInWithoutGHSaysSo(t *testing.T) {
	ls := &LoginSession{CLI: &CLI{Binary: filepath.Join(t.TempDir(), "no-gh")}}
	_, err := ls.Start(bg)
	var ge *Error
	if !errors.As(err, &ge) || ge.Reason != domain.GHMissing {
		t.Fatalf("err = %v", err)
	}
}

func makeClone(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCloneScan(t *testing.T) {
	home := t.TempDir()
	makeClone(t, filepath.Join(home, "code", "app"), "https://github.com/octo/app.git")
	makeClone(t, filepath.Join(home, "code", "org", "lib"), "git@github.com:org/lib.git") // two levels down
	makeClone(t, filepath.Join(home, "dev", "elsewhere"), "https://gitlab.com/x/y.git")   // not GitHub
	makeClone(t, filepath.Join(home, "code", "app", "nested"), "https://github.com/octo/nested.git")
	makeClone(t, filepath.Join(home, "code", "node_modules", "dep"), "https://github.com/dep/dep.git")
	makeClone(t, filepath.Join(home, "code", ".hidden", "h"), "https://github.com/hid/h.git")
	makeClone(t, filepath.Join(home, "code", "a", "b", "c", "toodeep"), "https://github.com/deep/deep.git")
	// A linked worktree has a .git file, not a directory: it is not a clone of its own.
	wt := filepath.Join(home, "code", "wt")
	_ = os.MkdirAll(wt, 0o755)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere\n"), 0o644)

	got := ScanLocalClones(bg, ScanOptions{Roots: DefaultScanRoots(home)})
	found := map[string]string{}
	for _, c := range got {
		found[c.Repo.String()] = c.Path
	}
	if len(found) != 2 || found["octo/app"] != filepath.Join(home, "code", "app") || found["org/lib"] != filepath.Join(home, "code", "org", "lib") {
		t.Fatalf("found = %v: a repository is not searched inside, node_modules and hidden directories are skipped, and depth is bounded", found)
	}

	if cl := ClonesAt(filepath.Join(home, "code", "app")); len(cl) != 1 || cl[0].String() != "octo/app" {
		t.Fatalf("ClonesAt = %v", cl)
	}
	if cl := ClonesAt(filepath.Join(home, "code")); len(cl) != 0 {
		t.Fatalf("a directory that is not a clone: %v", cl)
	}

	// A cancelled search returns what it has, promptly.
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if got := ScanLocalClones(ctx, ScanOptions{Roots: []string{home}}); len(got) != 0 {
		t.Fatalf("a cancelled scan found %v", got)
	}
	// Bounded work.
	if got := ScanLocalClones(bg, ScanOptions{Roots: []string{home}, MaxDirs: 1}); len(got) != 0 {
		t.Fatalf("MaxDirs was ignored: %v", got)
	}
}
