package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
)

// gitAPI is a server with the Git Control Center, a project whose repository has
// a Werkbord branch with commits, and a second project.
type gitAPI struct {
	url     string
	repo    string
	project service.ProjectDetail
	branch  string
	headSha string
	other   service.ProjectDetail
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitAPI(t *testing.T, token string) *gitAPI {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@e.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@e.com")
	ga := &gitAPI{}
	ts := newTestServer(t, func(o *Options) {
		db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db2"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		bus := events.NewBroker()
		t.Cleanup(bus.Close)
		deps := service.Deps{Store: db, Bus: bus}
		g := &gitrepo.CLI{}
		wts := &service.Worktrees{Deps: deps, Root: filepath.Join(canon(t, t.TempDir()), "wt")}
		o.Store, o.Events = db, bus
		o.Projects = &service.Projects{Deps: deps, Git: g}
		o.Tasks = &service.Tasks{Deps: deps}
		o.Runs = &service.Runs{Deps: deps}
		o.Worktrees = wts
		gc := &service.GitControl{Deps: deps, Git: g, Worktrees: wts}
		o.Git = gc
		o.Health = &service.GitHealth{Deps: deps, Control: gc}
		if token != "" {
			o.AuthRequired, o.Token = true, token
		}
	})
	ga.url = ts.URL

	register := func(name string) (string, service.ProjectDetail) {
		repo := canon(t, gitRepo(t))
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, repo, "add", "-A")
		gitIn(t, repo, "commit", "-q", "-m", "init")
		var p service.ProjectDetail
		req, _ := http.NewRequest("POST", ts.URL+"/api/projects", strings.NewReader(`{"path":`+jsonString(repo)+`,"name":`+jsonString(name)+`}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("register: %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		return repo, p
	}
	ga.repo, ga.project = register("app")
	_, ga.other = register("other")

	// A branch with two commits, in the project (as a plain branch: ownership is tested in the service).
	ga.branch = "feature/api"
	gitIn(t, ga.repo, "checkout", "-q", "-b", ga.branch)
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(ga.repo, n+".txt"), []byte(n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, ga.repo, "add", "-A")
		gitIn(t, ga.repo, "commit", "-q", "-m", "add "+n)
	}
	ga.headSha = gitIn(t, ga.repo, "rev-parse", "HEAD")
	gitIn(t, ga.repo, "checkout", "-q", "main")
	return ga
}

func canon(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (g *gitAPI) path(project, suffix string) string {
	return g.url + "/api/projects/" + project + "/git" + suffix
}

func TestGitOverviewAndDrillDownOverHTTP(t *testing.T) {
	g := newGitAPI(t, "")
	var o domain.GitOverview
	if code := do(t, "GET", g.path(g.project.ID, ""), "", &o); code != 200 {
		t.Fatalf("overview: %d", code)
	}
	if o.ProjectID != g.project.ID || o.Local.Head.Branch != "main" || o.Local.Target.Name != "main" || o.Summary.Branches != 2 {
		t.Errorf("overview = %+v", o.Summary)
	}
	var feat *domain.GitBranch
	for i := range o.Branches {
		if o.Branches[i].Name == g.branch {
			feat = &o.Branches[i]
		}
	}
	if feat == nil || feat.VsTarget.Ahead != 2 || feat.VsTarget.Relation != domain.RelAhead || feat.DevBoard.Created {
		t.Fatalf("branch = %+v", feat)
	}

	// Branch → changed files → file diff. Names go in the query, so a slash in a branch name is no problem.
	var cmp domain.GitComparison
	q := "?branch=" + url.QueryEscape(g.branch) + "&limit=1"
	if code := do(t, "GET", g.path(g.project.ID, "/compare"+q), "", &cmp); code != 200 {
		t.Fatalf("compare: %d", code)
	}
	if cmp.FilesTotal != 2 || len(cmp.Files) != 1 || cmp.Unique.Total != 2 || cmp.Target != "main" || cmp.BranchSha != g.headSha {
		t.Errorf("comparison = %+v", cmp)
	}
	var d domain.GitFileDiff
	dq := "?from=" + cmp.MergeBase + "&to=" + cmp.BranchSha + "&path=" + url.QueryEscape(cmp.Files[0].Path)
	if code := do(t, "GET", g.path(g.project.ID, "/diff"+dq), "", &d); code != 200 || d.Additions != 1 || !strings.Contains(d.Diff, "+a") {
		t.Errorf("diff: %d %+v", code, d)
	}
	var page domain.GitCommitPage
	if code := do(t, "GET", g.path(g.project.ID, "/commits?branch="+url.QueryEscape(g.branch)+"&limit=1"), "", &page); code != 200 || len(page.Items) != 1 || !page.Truncated {
		t.Errorf("commits: %d %+v", code, page)
	}
	var wc service.WorkingChanges
	if code := do(t, "GET", g.path(g.project.ID, "/changes"), "", &wc); code != 200 || !wc.Primary || !wc.Tree.Clean {
		t.Errorf("changes: %d %+v", code, wc)
	}
	var st domain.GitHubState
	if code := do(t, "GET", g.path(g.project.ID, "/pull-requests"), "", &st); code != 200 || st.Available || st.Reason == "" || st.PullRequests == nil {
		t.Errorf("pull requests: %d %+v (no GitHub here: an answer that says why, not an error)", code, st)
	}
}

func TestGitErrorsMapToStatuses(t *testing.T) {
	g := newGitAPI(t, "")
	var e errorBody
	cases := []struct {
		name, method, url, body string
		status                  int
		code                    string
	}{
		{"unknown project", "GET", g.path("prj_nope", ""), "", 404, "not_found"},
		{"unknown branch", "GET", g.path(g.project.ID, "/compare?branch=nope"), "", 404, "not_found"},
		{"bad branch name", "GET", g.path(g.project.ID, "/compare?branch=a..b"), "", 400, "invalid"},
		{"option as branch", "GET", g.path(g.project.ID, "/compare?branch=--output%3Dx"), "", 400, "invalid"},
		{"names instead of commit ids", "GET", g.path(g.project.ID, "/diff?from=main&to="+g.headSha), "", 400, "invalid"},
		{"path traversal", "GET", g.path(g.project.ID, "/changes/diff?kind=untracked&path=../../etc/passwd"), "", 400, "invalid"},
		{"bad window", "GET", g.path(g.project.ID, "/diff?from="+g.headSha+"&to="+g.headSha+"&offset=-1"), "", 400, "invalid"},
		{"unknown fields are refused", "POST", g.path(g.project.ID, "/push"), `{"branch":"x","force":true}`, 400, "invalid"},
		{"bad strategy", "POST", g.path(g.project.ID, "/merge/plan"), `{"branch":"` + g.branch + `","strategy":"squash"}`, 400, "invalid"},
		{"unknown worktree", "GET", g.path(g.project.ID, "/changes?worktree=wt_nope"), "", 404, "not_found"},
		{"clean an unknown worktree", "POST", g.path(g.project.ID, "/worktrees/clean"), `{"worktreeId":"wt_nope"}`, 404, "not_found"},
	}
	for _, c := range cases {
		e = errorBody{}
		if got := do(t, c.method, c.url, c.body, &e); got != c.status || e.Error.Code != c.code {
			t.Errorf("%s: %d %+v, want %d %s", c.name, got, e.Error, c.status, c.code)
		}
	}

	// A broken repository is a git failure with its message, not "internal error".
	gitIn(t, g.repo, "update-ref", "refs/heads/broken", g.headSha)
	if err := os.WriteFile(filepath.Join(g.repo, ".git", "refs", "heads", "corrupt"), []byte("not a sha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e = errorBody{}
	if code := do(t, "GET", g.path(g.project.ID, ""), "", &e); code != 200 && (code != 502 || e.Error.Code != "git_failed") {
		t.Errorf("a corrupt ref: %d %+v", code, e.Error)
	}
}

func TestGitActionsAnswerWithAnOutcomeNotAnError(t *testing.T) {
	g := newGitAPI(t, "")
	var res domain.GitActionResult

	// Pushing with no remote: refused, with the reason, and HTTP 200.
	code := do(t, "POST", g.path(g.project.ID, "/push"), `{"branch":"`+g.branch+`","expectedSha":"`+g.headSha+`"}`, &res)
	if code != 200 || res.OK || res.Outcome != domain.OutcomeRefused || len(res.Blockers) == 0 || res.Blockers[0].Code != domain.BlockNoRemote {
		t.Errorf("push: %d %+v", code, res)
	}
	// A stale push: refused, and nothing was said to have happened.
	res = domain.GitActionResult{}
	do(t, "POST", g.path(g.project.ID, "/push"), `{"branch":"`+g.branch+`","expectedSha":"`+strings.Repeat("c", 40)+`"}`, &res)
	if res.OK || res.Blockers[0].Code != domain.BlockBranchMoved {
		t.Errorf("stale push: %+v", res)
	}

	// Merge: plan, then do it, then see it in the overview. Checking the target out is the user's.
	var plan domain.GitMergePlan
	body := `{"branch":"` + g.branch + `","branchSha":"` + g.headSha + `","target":"main","targetSha":"` + gitIn(t, g.repo, "rev-parse", "main") + `"}`
	if code := do(t, "POST", g.path(g.project.ID, "/merge/plan"), body, &plan); code != 200 || !plan.CanMerge || plan.Ahead != 2 {
		t.Fatalf("plan: %d %+v", code, plan)
	}
	res = domain.GitActionResult{}
	if code := do(t, "POST", g.path(g.project.ID, "/merge"), body, &res); code != 200 || !res.OK || res.Remote != nil || res.Local == nil {
		t.Fatalf("merge: %d %+v", code, res)
	}
	if got := gitIn(t, g.repo, "rev-parse", "main"); got == "" || gitIn(t, g.repo, "merge-base", "main", g.branch) != g.headSha {
		t.Error("main does not contain the branch")
	}

	// Deleting a branch Werkbord did not create is refused.
	var dplan domain.GitDeletePlan
	dbody := `{"branch":"` + g.branch + `","branchSha":"` + g.headSha + `"}`
	if code := do(t, "POST", g.path(g.project.ID, "/branches/delete-plan"), dbody, &dplan); code != 200 || dplan.CanDelete || dplan.Blockers[0].Code != domain.BlockNotOwned && dplan.Blockers[1].Code != domain.BlockNotOwned {
		t.Errorf("delete plan: %d %+v", code, dplan)
	}
	res = domain.GitActionResult{}
	if code := do(t, "POST", g.path(g.project.ID, "/branches/delete"), dbody, &res); code != 200 || res.OK || res.Outcome != domain.OutcomeRefused {
		t.Errorf("delete: %d %+v", code, res)
	}
	if gitIn(t, g.repo, "branch", "--list", g.branch) == "" {
		t.Error("a branch Werkbord did not create was deleted")
	}
	// Fetch with no remote.
	res = domain.GitActionResult{}
	if code := do(t, "POST", g.path(g.project.ID, "/fetch"), "", &res); code != 200 || res.Outcome != domain.OutcomeNoop {
		t.Errorf("fetch: %d %+v", code, res)
	}
}

func TestGitEndpointsAreScopedToTheirProject(t *testing.T) {
	g := newGitAPI(t, "")
	// The branch exists in "app" only. Asking "other" about it finds nothing, and
	// acting on it through "other" cannot touch app's repository.
	var e errorBody
	if code := do(t, "GET", g.path(g.other.ID, "/compare?branch="+url.QueryEscape(g.branch)), "", &e); code != 404 {
		t.Errorf("compare through another project: %d", code)
	}
	var res domain.GitActionResult
	do(t, "POST", g.path(g.other.ID, "/push"), `{"branch":"`+g.branch+`","expectedSha":"`+g.headSha+`"}`, &res)
	if res.OK || len(res.Blockers) == 0 || res.Blockers[0].Code != domain.BlockBranchMissing {
		t.Errorf("push through another project: %+v", res)
	}
	var o domain.GitOverview
	do(t, "GET", g.path(g.other.ID, ""), "", &o)
	for _, b := range o.Branches {
		if b.Name == g.branch {
			t.Error("another project's branch leaked into this overview")
		}
	}
	// A diff between two commits of "app" is not available through "other": they are not in its repository.
	if code := do(t, "GET", g.path(g.other.ID, "/diff?from="+g.headSha+"&to="+g.headSha), "", &e); code != 404 {
		t.Errorf("diff of another project's commits: %d", code)
	}
}

func TestGitEndpointsRequireTheToken(t *testing.T) {
	g := newGitAPI(t, "s3cret-token")
	for _, c := range []struct{ method, path string }{
		{"GET", ""}, {"GET", "/pull-requests"}, {"GET", "/compare?branch=x"}, {"GET", "/diff"}, {"GET", "/changes"},
		{"POST", "/push"}, {"POST", "/merge"}, {"POST", "/merge/plan"}, {"POST", "/branches/delete"}, {"POST", "/worktrees/clean"}, {"POST", "/pull-requests"}, {"POST", "/fetch"},
		{"GET", "/health"}, {"POST", "/health/refresh"}, {"POST", "/health/findings/hf_x/dismiss"}, {"POST", "/health/findings/hf_x/reopen"},
	} {
		req, _ := http.NewRequest(c.method, g.path(g.project.ID, c.path), strings.NewReader("{}"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", c.method, c.path, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", g.path(g.project.ID, ""), nil)
	req.Header.Set("Authorization", "Bearer s3cret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("with the token: %v %v", resp, err)
	}
}

func TestGitDisabledAnswersUnavailable(t *testing.T) {
	ts := newTestServer(t, nil)
	var e errorBody
	if code := do(t, "GET", ts.URL+"/api/projects/prj_x/git", "", &e); code != 503 || e.Error.Code != "unavailable" {
		t.Errorf("%d %+v", code, e.Error)
	}
}
