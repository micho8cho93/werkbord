package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/events"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
	"devboard/internal/update"
)

func newTestServer(t *testing.T, mutate func(*Options)) *httptest.Server {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.NewBroker()
	t.Cleanup(bus.Close)
	deps := service.Deps{Store: db, Bus: bus}
	opt := Options{
		Projects: &service.Projects{Deps: deps, Git: &gitrepo.CLI{}},
		Tasks:    &service.Tasks{Deps: deps},
		Runs:     &service.Runs{Deps: deps},
		Settings: &service.Settings{Deps: deps, Version: "test"},
		Agents:   agent.NewRegistry(),
		Store:    db,
		Events:   bus,
		Version:  "test",
		Web:      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "shell") }),
	}
	if mutate != nil {
		mutate(&opt)
	}
	ts := httptest.NewServer(New(opt).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return dir
}

func do(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t, nil)
	var h healthResponse
	if code := do(t, "GET", ts.URL+"/api/health", "", &h); code != 200 || h.Status != "ok" || h.Database != "ok" {
		t.Fatalf("health = %d %+v", code, h)
	}
}

func TestProjectRegistrationAndTasks(t *testing.T) {
	ts := newTestServer(t, nil)
	repo := gitRepo(t)

	var p service.ProjectDetail
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(repo)+`}`, &p); code != 201 {
		t.Fatalf("register: %d", code)
	}
	if p.ID == "" || p.Repository == nil || p.Repository.CurrentBranch != "main" {
		t.Fatalf("unexpected project %+v", p)
	}
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(repo)+`}`, nil); code != 409 {
		t.Fatalf("duplicate register: %d", code)
	}
	var e errorBody
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(t.TempDir())+`}`, &e); code != 400 || e.Error.Code != "invalid" {
		t.Fatalf("non-repo register: %d %+v", code, e)
	}
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":"x","bogus":1}`, nil); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}

	var task struct {
		ID      string
		State   string
		Version int64
	}
	if code := do(t, "POST", ts.URL+"/api/projects/"+p.ID+"/tasks", `{"title":"Write docs"}`, &task); code != 201 || task.State != "backlog" {
		t.Fatalf("create task: %d %+v", code, task)
	}
	if code := do(t, "PATCH", ts.URL+"/api/projects/"+p.ID+"/tasks/"+task.ID, `{"state":"doing","version":1}`, &task); code != 200 || task.State != "doing" || task.Version != 2 {
		t.Fatalf("move task: %d %+v", code, task)
	}
	if code := do(t, "PATCH", ts.URL+"/api/projects/"+p.ID+"/tasks/"+task.ID, `{"state":"review","version":1}`, nil); code != 409 {
		t.Fatalf("stale move: %d", code)
	}
	if code := do(t, "PATCH", ts.URL+"/api/projects/"+p.ID+"/tasks/"+task.ID, `{"state":"blocked","version":2}`, nil); code != 400 {
		t.Fatalf("unknown state: %d", code)
	}
	if code := do(t, "PATCH", ts.URL+"/api/projects/"+p.ID+"/tasks/"+task.ID, `{"state":"review"}`, nil); code != 400 {
		t.Fatalf("missing version: %d", code)
	}
	var list struct{ Tasks []json.RawMessage }
	if code := do(t, "GET", ts.URL+"/api/projects/"+p.ID+"/tasks", "", &list); code != 200 || len(list.Tasks) != 1 {
		t.Fatalf("list tasks: %d %d", code, len(list.Tasks))
	}
	if code := do(t, "GET", ts.URL+"/api/projects/prj_nope/tasks", "", nil); code != 404 {
		t.Fatalf("missing project: %d", code)
	}
}

func TestEventStreamReplaysAndFollows(t *testing.T) {
	ts := newTestServer(t, nil)
	repo := gitRepo(t)
	var p service.ProjectDetail
	do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(repo)+`}`, &p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?after=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	next := func() (id, typ string) {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("read stream: %v", err)
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				typ = strings.TrimPrefix(line, "event: ")
			case line == "" && typ != "":
				return id, typ
			}
		}
	}

	if id, typ := next(); id != "1" || typ != "project.registered" {
		t.Fatalf("replayed event = %s %s", id, typ)
	}
	do(t, "POST", ts.URL+"/api/projects/"+p.ID+"/tasks", `{"title":"live"}`, nil)
	if id, typ := next(); id != "2" || typ != "task.created" {
		t.Fatalf("live event = %s %s", id, typ)
	}
}

func TestHostAndOriginChecks(t *testing.T) {
	ts := newTestServer(t, nil)

	req, _ := http.NewRequest("GET", ts.URL+"/api/health", nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("rebinding host: status %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("POST", ts.URL+"/api/projects", strings.NewReader(`{"path":"/tmp"}`))
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST: status %d", resp.StatusCode)
	}
}

func TestTokenAuth(t *testing.T) {
	ts := newTestServer(t, func(o *Options) { o.AuthRequired, o.Token = true, "s3cret" })
	if code := do(t, "GET", ts.URL+"/api/health", "", nil); code != 200 {
		t.Fatalf("health should be public: %d", code)
	}
	if code := do(t, "GET", ts.URL+"/api/projects", "", nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/projects", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("with token: %d", resp.StatusCode)
	}
	// The query-string token is only honoured for the event stream.
	if code := do(t, "GET", ts.URL+"/api/projects?access_token=s3cret", "", nil); code != 401 {
		t.Fatalf("query token on JSON endpoint: %d", code)
	}
}

func TestRoutingFallbacks(t *testing.T) {
	ts := newTestServer(t, nil)
	var e errorBody
	if code := do(t, "GET", ts.URL+"/api/nope", "", &e); code != 404 || e.Error.Code != "not_found" {
		t.Fatalf("unknown api route: %d %+v", code, e)
	}
	resp, err := http.Get(ts.URL + "/board")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "shell" || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("web fallback: %q, headers %v", body, resp.Header)
	}
}

// ---- GET /api/update ----

func TestUpdateStatusReportsAndIsBehindTheToken(t *testing.T) {
	checker := &update.Checker{Current: "v1.0.0"}
	var forced []bool
	ts := newTestServer(t, func(o *Options) {
		o.AuthRequired, o.Token = true, "s3cret"
		o.Update = func(ctx context.Context, force bool) update.Status {
			forced = append(forced, force)
			return update.Status{Current: checker.Current, Latest: "v1.1.0", Available: true, Release: true}
		}
	})
	// Like every /api route but /api/health, it needs the token: a phone or a page on the network does not learn the version for free.
	if code := do(t, "GET", ts.URL+"/api/update", "", nil); code != 401 {
		t.Fatalf("without a token: %d", code)
	}
	get := func(path string) update.Status {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer s3cret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var st update.Status
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&st) != nil {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
		return st
	}
	if st := get("/api/update"); !st.Available || st.Latest != "v1.1.0" || st.Current != "v1.0.0" {
		t.Fatalf("status = %+v", st)
	}
	get("/api/update?refresh=1")
	if len(forced) != 2 || forced[0] || !forced[1] {
		t.Fatalf("force flags = %v: only ?refresh=1 asks again", forced)
	}
	// It is read-only: there is no way to ask the controller to install anything.
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		if code := do(t, m, ts.URL+"/api/update", "", nil); code == 200 {
			t.Fatalf("%s /api/update was accepted", m)
		}
	}
}

func TestUpdateStatusWithoutAChecker(t *testing.T) {
	ts := newTestServer(t, func(o *Options) { o.Version = "v1.0.0" })
	var st update.Status
	if code := do(t, "GET", ts.URL+"/api/update", "", &st); code != 200 || !st.Disabled || st.Available || st.Current != "v1.0.0" {
		t.Fatalf("no checker: %d %+v", code, st)
	}
}
