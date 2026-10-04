package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func staleLock(t *testing.T, repo string) {
	t.Helper()
	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestHealthOfAQuietRepositoryIsHealthy(t *testing.T) {
	g := newGitAPI(t, "")
	var r domain.HealthReport
	if code := do(t, "GET", g.path(g.project.ID, "/health"), "", &r); code != 200 {
		t.Fatalf("health: %d", code)
	}
	if r.ProjectID != g.project.ID || r.Summary.State != "healthy" || r.Summary.Headline != "Healthy" || r.Check == nil {
		t.Fatalf("report = %+v", r)
	}
	if r.Findings == nil || r.Dismissed == nil || r.Resolved == nil {
		t.Fatal("lists must be [] in JSON, not null")
	}
}

func TestHealthFindingsDismissAndRefreshOverHTTP(t *testing.T) {
	g := newGitAPI(t, "")
	staleLock(t, g.repo)

	var r domain.HealthReport
	if code := do(t, "POST", g.path(g.project.ID, "/health/refresh"), "", &r); code != 200 {
		t.Fatalf("refresh: %d", code)
	}
	if r.Summary.State != "attention" || r.Summary.Headline != "1 item needs attention" || len(r.Findings) != 1 {
		t.Fatalf("report = %+v", r.Summary)
	}
	f := r.Findings[0]
	if f.Type != domain.FindAutomationBlocked || f.Action.Label == "" || len(f.Evidence) == 0 || f.State != domain.HealthOpen {
		t.Fatalf("finding = %+v", f)
	}

	// Dismiss: hidden, counted, and still listed folded away.
	var d domain.HealthReport
	if code := do(t, "POST", g.path(g.project.ID, "/health/findings/"+f.ID+"/dismiss"), "", &d); code != 200 || d.Summary.State != "healthy" || len(d.Dismissed) != 1 {
		t.Fatalf("dismiss: %d %+v", code, d.Summary)
	}
	var e errorBody
	if code := do(t, "POST", g.path(g.project.ID, "/health/findings/"+f.ID+"/dismiss"), "", &e); code != 409 {
		t.Errorf("dismissing twice: %d", code)
	}
	if code := do(t, "POST", g.path(g.project.ID, "/health/findings/hf_unknown/dismiss"), "", &e); code != 404 {
		t.Errorf("unknown finding: %d", code)
	}
	var back domain.HealthReport
	if code := do(t, "POST", g.path(g.project.ID, "/health/findings/"+f.ID+"/reopen"), "", &back); code != 200 || len(back.Findings) != 1 {
		t.Fatalf("reopen: %d %+v", code, back.Summary)
	}

	// Fixing it resolves it, and the report says so.
	if err := os.Remove(filepath.Join(g.repo, ".git", "index.lock")); err != nil {
		t.Fatal(err)
	}
	var fixed domain.HealthReport
	do(t, "POST", g.path(g.project.ID, "/health/refresh"), "", &fixed)
	if fixed.Summary.State != "healthy" || len(fixed.Resolved) != 1 {
		t.Fatalf("after the fix: %+v resolved=%d", fixed.Summary, len(fixed.Resolved))
	}
}

func TestHealthIsScopedToItsProject(t *testing.T) {
	g := newGitAPI(t, "")
	staleLock(t, g.repo)
	var r domain.HealthReport
	do(t, "POST", g.path(g.project.ID, "/health/refresh"), "", &r)
	if len(r.Findings) != 1 {
		t.Fatal("setup")
	}
	// Another project neither sees it nor can dismiss it.
	var other domain.HealthReport
	if code := do(t, "GET", g.path(g.other.ID, "/health"), "", &other); code != 200 || len(other.Findings) != 0 || other.Summary.State != "healthy" {
		t.Fatalf("other project: %d %+v", code, other.Summary)
	}
	var e errorBody
	if code := do(t, "POST", g.path(g.other.ID, "/health/findings/"+r.Findings[0].ID+"/dismiss"), "", &e); code != 404 {
		t.Errorf("dismissing another project's finding: %d", code)
	}
	if code := do(t, "GET", g.path("prj_nope", "/health"), "", &e); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
}

func TestACriticalFindingReachesTheControlCenter(t *testing.T) {
	g := newGitAPI(t, "")
	// An interrupted merge with conflicts in the user's checkout.
	if err := os.WriteFile(filepath.Join(g.repo, "README.md"), []byte("main's text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, g.repo, "commit", "-q", "-am", "main edits README")
	gitIn(t, g.repo, "checkout", "-q", g.branch)
	if err := os.WriteFile(filepath.Join(g.repo, "README.md"), []byte("the branch's text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, g.repo, "commit", "-q", "-am", "branch edits README")
	gitIn(t, g.repo, "checkout", "-q", "main")
	_ = os.Remove(filepath.Join(g.repo, ".git", "index.lock"))
	cmd := exec.Command("git", "-C", g.repo, "merge", g.branch)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	_ = cmd.Run() // stops on the conflict, as a merge in a terminal does

	var r domain.HealthReport
	do(t, "POST", g.path(g.project.ID, "/health/refresh"), "", &r)
	if r.Summary.State != "critical" {
		t.Fatalf("summary = %+v", r.Summary)
	}

	var cc service.Overview
	if code := do(t, "GET", g.url+"/api/control-center", "", &cc); code != 200 {
		t.Fatalf("control center: %d", code)
	}
	if len(cc.Repository) != 1 || cc.Repository[0].Severity != domain.HealthCritical || cc.Repository[0].ProjectName != "app" {
		t.Fatalf("repository = %+v", cc.Repository)
	}
	for _, p := range cc.Projects {
		if p.ProjectID == g.project.ID && (p.RepoRisk != 1 || p.RepoAttention != 1) {
			t.Fatalf("project activity = %+v", p)
		}
		if p.ProjectID == g.other.ID && (p.RepoRisk != 0 || p.RepoAttention != 0) {
			t.Fatalf("the other project is untouched = %+v", p)
		}
	}
}

func TestHealthDisabledAnswersUnavailable(t *testing.T) {
	ts := newTestServer(t, nil)
	var e errorBody
	if code := do(t, "GET", ts.URL+"/api/projects/prj_x/git/health", "", &e); code != 503 || e.Error.Code != "unavailable" {
		t.Errorf("%d %+v", code, e.Error)
	}
}
