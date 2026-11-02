package api

import (
	"strings"
	"testing"
	"time"

	"werkbord/internal/github"
	"werkbord/internal/github/ghtest"
	"werkbord/internal/service"
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
	for _, path := range []string{"/api/github", "/api/github/repos", "/api/network", "/api/settings", "/api/runners", "/api/onboarding"} {
		if code := do(t, "GET", ts.URL+path, "", &e); code != 503 || e.Error.Code != "unavailable" {
			t.Errorf("%s = %d %+v", path, code, e)
		}
	}
}

func TestCreateRepoAndPagesEndpointsRefuseWhatIsNotConfirmedOrValid(t *testing.T) {
	url, gh := newGitHubServer(t)
	gh.SignIn()
	var out map[string]any
	if code := do(t, "POST", url+"/api/github/repos/create", `{"name":"bad name","description":"","private":false}`, &out); code != 400 {
		t.Fatalf("a bad name = %d %v", code, out)
	}
	if code := do(t, "POST", url+"/api/github/repos/create", `{"name":"ok","extra":1}`, &out); code != 400 {
		t.Fatalf("an unknown field = %d", code)
	}
	var st service.PagesState
	if code := do(t, "GET", url+"/api/github/pages?repo=octo/none", "", &st); code != 200 || st.State != "disabled" {
		t.Fatalf("pages = %d %+v", code, st)
	}
	if code := do(t, "GET", url+"/api/github/pages?repo=nonsense", "", &out); code != 400 {
		t.Fatalf("a bad repo = %d", code)
	}
}

func TestRepositoryEndpointTellsLiveFromNoAccessFromNotConnected(t *testing.T) {
	url, gh := newGitHubServer(t)
	var st service.RepoState
	if code := do(t, "GET", url+"/api/github/repository?repo=octo/live", "", &st); code != 200 || st.State != "not_connected" {
		t.Fatalf("signed out = %d %+v", code, st)
	}
	gh.SignIn()
	if code := do(t, "GET", url+"/api/github/repository?repo=octo/live", "", &st); code != 200 || st.State != "live" || st.Repo == nil {
		t.Fatalf("signed in = %d %+v", code, st)
	}
	if r := st.Repo; r.DefaultBranch != "main" || !r.Permissions.Push || r.Permissions.Admin || r.Permissions.Role() != "write" || len(r.Branches) != 2 || len(r.Commits) != 1 || r.Commits[0].Message != "Fix the thing" || r.Commits[0].Author != "ana" {
		t.Fatalf("the view = %+v", r)
	}
	var hidden service.RepoState
	if code := do(t, "GET", url+"/api/github/repository?repo=octo/secret", "", &hidden); code != 200 || hidden.State != "no_access" {
		t.Fatalf("a repository GitHub does not show = %d %+v", code, hidden)
	}
	var out map[string]any
	if code := do(t, "GET", url+"/api/github/repository?repo=--bad", "", &out); code != 400 {
		t.Fatalf("a bad name = %d", code)
	}
}

func TestRepositoryHeadsAreOneCheapReadAndNeverFailTheWatcher(t *testing.T) {
	url, gh := newGitHubServer(t)
	var out struct {
		Branches []struct{ Name, SHA string } `json:"branches"`
	}
	if code := do(t, "GET", url+"/api/github/repository/heads?repo=octo/live", "", &out); code != 200 || len(out.Branches) != 0 {
		t.Fatalf("signed out = %d %+v", code, out)
	}
	gh.SignIn()
	if code := do(t, "GET", url+"/api/github/repository/heads?repo=octo/live", "", &out); code != 200 || len(out.Branches) != 2 || out.Branches[0].SHA != "aaa111" {
		t.Fatalf("signed in = %d %+v", code, out)
	}
	var bad map[string]any
	if code := do(t, "GET", url+"/api/github/repository/heads?repo=--bad", "", &bad); code != 400 {
		t.Fatalf("a bad name = %d", code)
	}
}

func TestRepositoryHeadsAskWithTheEntityTagAndKeepTheLastAnswerWhenUnchanged(t *testing.T) {
	url, gh := newGitHubServer(t)
	gh.SignIn()
	var out struct {
		Branches []struct{ Name, SHA string } `json:"branches"`
	}
	for i := 0; i < 2; i++ {
		if code := do(t, "GET", url+"/api/github/repository/heads?repo=octo/live", "", &out); code != 200 || len(out.Branches) != 2 {
			t.Fatalf("look %d = %d %+v", i, code, out)
		}
	}
	if !strings.Contains(gh.Calls(), `If-None-Match: "tag1"`) {
		t.Fatalf("the second look did not ask with the tag:\n%s", gh.Calls())
	}
}

func TestRepositoryActionsAreConfirmedByNameCheckedAndReadBack(t *testing.T) {
	url, gh := newGitHubServer(t)
	gh.SignIn()
	act := func(body string) service.RepoActionResult {
		t.Helper()
		var out service.RepoActionResult
		if code := do(t, "POST", url+"/api/github/repository/actions", body, &out); code != 200 {
			t.Fatalf("%s = %d", body, code)
		}
		return out
	}
	if r := act(`{"repo":"octo/live","action":"create_branch","branch":"topic"}`); r.Outcome != "refused" || r.OK {
		t.Fatalf("no confirmation by name = %+v", r)
	}
	if r := act(`{"repo":"octo/live","confirmRepo":"octo/live","action":"create_branch","branch":"bad..name"}`); r.Outcome != "refused" {
		t.Fatalf("a bad branch name = %+v", r)
	}
	if r := act(`{"repo":"octo/live","confirmRepo":"octo/live","action":"delete_branch","branch":"main"}`); r.Outcome != "refused" || !strings.Contains(r.Message, "default") {
		t.Fatalf("the default branch is not deleted = %+v", r)
	}
	if r := act(`{"repo":"octo/live","confirmRepo":"octo/live","action":"create_branch","branch":"topic"}`); r.Outcome != "done" || !r.OK {
		t.Fatalf("create = %+v", r)
	}
	if r := act(`{"repo":"octo/live","confirmRepo":"octo/live","action":"comment","number":7,"body":"Looks good"}`); r.Outcome != "done" || r.URL == "" {
		t.Fatalf("comment = %+v", r)
	}
	var out map[string]any
	if code := do(t, "POST", url+"/api/github/repository/actions", `{"repo":"octo/live","confirmRepo":"octo/live","action":"format_disk"}`, &out); code != 400 {
		t.Fatalf("an unknown action = %d", code)
	}
	if code := do(t, "POST", url+"/api/github/repository/actions", `{"repo":"octo/live","confirmRepo":"octo/live","action":"comment","extra":1}`, &out); code != 400 {
		t.Fatalf("an unknown field = %d", code)
	}
}

func TestAdminActionsNeedTheAdminRoleAndTheTargetTypedAndAreReadBack(t *testing.T) {
	url, gh := newGitHubServer(t)
	gh.SignIn()
	act := func(body string) service.RepoActionResult {
		t.Helper()
		var out service.RepoActionResult
		if code := do(t, "POST", url+"/api/github/repository/actions", body, &out); code != 200 {
			t.Fatalf("%s = %d", body, code)
		}
		return out
	}
	// A write role is not enough.
	if r := act(`{"repo":"octo/live","confirmRepo":"octo/live","action":"protect_branch","branch":"main","reviews":1}`); r.Outcome != "refused" || !strings.Contains(r.Message, "admin") {
		t.Fatalf("a writer protecting a branch = %+v", r)
	}
	// The target is typed again, and is never oneself.
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"set_role","user":"ana","role":"write"}`); r.Outcome != "refused" {
		t.Fatalf("no typed confirmation = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"set_role","user":"octo","confirmTarget":"octo","role":"read"}`); r.Outcome != "refused" || !strings.Contains(r.Message, "own") {
		t.Fatalf("changing oneself = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"set_role","user":"../x","confirmTarget":"../x","role":"read"}`); r.Outcome != "refused" {
		t.Fatalf("a bad user name = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"set_role","user":"ana","confirmTarget":"ana","role":"write"}`); r.Outcome != "done" || !r.OK {
		t.Fatalf("set role = %+v", r)
	}
	var adm service.RepoAdmin
	if code := do(t, "GET", url+"/api/github/repository/admin?repo=octo/ops", "", &adm); code != 200 || !adm.CanAdmin || len(adm.Collaborators) != 2 || adm.Protection == nil || adm.Protection.Protected {
		t.Fatalf("admin view = %d %+v", code, adm)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"protect_branch","branch":"main","reviews":2}`); r.Outcome != "done" {
		t.Fatalf("protect = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"protect_branch","branch":"main","reviews":9}`); r.Outcome != "refused" {
		t.Fatalf("too many reviews = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"unprotect_branch","branch":"main"}`); r.Outcome != "done" {
		t.Fatalf("unprotect = %+v", r)
	}
	if r := act(`{"repo":"octo/ops","confirmRepo":"octo/ops","action":"remove_collaborator","user":"ana","confirmTarget":"ana"}`); r.Outcome != "done" {
		t.Fatalf("remove = %+v", r)
	}
}
