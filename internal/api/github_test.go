package api

import (
	"strings"
	"testing"
	"time"

	"devboard/internal/github"
	"devboard/internal/github/ghtest"
	"devboard/internal/service"
)

func newGitHubServer(t *testing.T) (url string, gh *ghtest.Fake) {
	t.Helper()
	gh = ghtest.New(t)
	cli := &github.CLI{Binary: gh.Binary}
	ts := newTestServer(t, func(o *Options) {
		o.GitHub = &service.GitHubSetup{Deps: o.Projects.Deps, CLI: cli, Login: &github.LoginSession{CLI: cli}, Projects: o.Projects,
			Home: t.TempDir(), ScanRoots: []string{}}
	})
	return ts.URL, gh
}

func TestGitHubConnectionOverHTTP(t *testing.T) {
	url, gh := newGitHubServer(t)
	var st service.GitHubStatus
	if code := do(t, "GET", url+"/api/github", "", &st); code != 200 || st.State != service.GitHubSignedOut {
		t.Fatalf("status = %d %+v", code, st)
	}
	var e apiError
	if code := do(t, "GET", url+"/api/github/repos", "", &e); code != 409 {
		t.Fatalf("repos while signed out = %d %+v", code, e)
	}

	var login github.Login
	if code := do(t, "POST", url+"/api/github/login", `{}`, &login); code != 200 || login.Code != "ABCD-1234" || !strings.HasPrefix(login.URL, "https://github.com/") {
		t.Fatalf("login = %d %+v", code, login)
	}
	gh.Approve()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		do(t, "GET", url+"/api/github", "", &st)
		if st.State == service.GitHubSignedIn {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.State != service.GitHubSignedIn || st.Account == nil || st.Account.Login != "octo" {
		t.Fatalf("connected = %+v", st)
	}

	gh.Repos(1, twoReposJSON)
	var list service.RepoList
	if code := do(t, "GET", url+"/api/github/repos", "", &list); code != 200 || len(list.Repos) != 2 || list.Repos[0].LocalPaths == nil {
		t.Fatalf("repos = %d %+v", code, list)
	}
	if code := do(t, "POST", url+"/api/github/repos/add", `{"fullName":"octo/app","path":"/nowhere"}`, &e); code != 400 || e.Error.Code != "invalid" {
		t.Fatalf("add a path that is no clone = %d %+v", code, e)
	}
	if code := do(t, "POST", url+"/api/github/repos/add", `{"fullName":"-x/y"}`, &e); code != 400 {
		t.Fatalf("add a bad name = %d", code)
	}
	if code := do(t, "POST", url+"/api/github/repos/add", `{"fullName":"octo/app","extra":1}`, &e); code != 400 {
		t.Fatalf("unknown field = %d", code)
	}
	if code := do(t, "POST", url+"/api/github/login/cancel", `{}`, &st); code != 200 {
		t.Fatalf("cancel = %d", code)
	}
}

const twoReposJSON = `[
 {"full_name":"octo/app","name":"app","pushed_at":"2026-09-01T10:00:00Z","html_url":"https://github.com/octo/app","owner":{"login":"octo"}},
 {"full_name":"octo/lib","name":"lib","pushed_at":"2026-09-02T10:00:00Z","html_url":"https://github.com/octo/lib","owner":{"login":"octo"}}]`

func TestOptionalEndpointsAreUnavailableWithoutTheirService(t *testing.T) {
	ts := newTestServer(t, func(o *Options) { o.Settings = nil })
	var e apiError
	for _, path := range []string{"/api/github", "/api/github/repos", "/api/network", "/api/network/phone", "/api/settings", "/api/runners", "/api/onboarding"} {
		if code := do(t, "GET", ts.URL+path, "", &e); code != 503 || e.Error.Code != "unavailable" {
			t.Errorf("%s = %d %+v", path, code, e)
		}
	}
}
