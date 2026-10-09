package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/localaccess"
)

// A scoped server: a controller that requires its token, with a store of local access tokens.
type scoped struct {
	*runsServer
	owner string
	store *localaccess.Store
}

const ownerToken = "owner-token-123"

func newScoped(t *testing.T) *scoped {
	t.Helper()
	st, err := localaccess.Open(filepath.Join(t.TempDir(), "local-access.json"))
	if err != nil {
		t.Fatal(err)
	}
	sc := &scoped{owner: ownerToken, store: st}
	testAuthToken = ownerToken
	t.Cleanup(func() { testAuthToken = "" })
	sc.runsServer = newRunsServer(t, func(o *Options) { o.AuthRequired, o.Token, o.LocalAccess = true, ownerToken, st })
	return sc
}

func call(t *testing.T, method, url, token, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// mint gives a program access, as the owner.
func (sc *scoped) mint(t *testing.T, name string) string {
	t.Helper()
	code, b := call(t, "POST", sc.url+"/api/local-access", sc.owner, `{"name":`+jsonString(name)+`}`)
	if code != 201 {
		t.Fatalf("mint: %d %s", code, b)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(b, &out)
	if !strings.HasPrefix(out.Token, localaccess.TokenPrefix) {
		t.Fatalf("token = %q", out.Token)
	}
	return out.Token
}

func TestOnlyTheOwnerGivesAProgramAccessAndSeesAndRevokesIt(t *testing.T) {
	sc := newScoped(t)
	if code, _ := call(t, "POST", sc.url+"/api/local-access", "", `{"name":"x"}`); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	tok := sc.mint(t, "A program")
	// A program with access cannot give itself or anyone else more, see who has access, or take it away.
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/local-access", `{"name":"another"}`}, {"GET", "/api/local-access", ""}, {"DELETE", "/api/local-access/la_x", ""},
	} {
		if code, _ := call(t, c.method, sc.url+c.path, tok, c.body); code != 403 {
			t.Errorf("%s %s with a program's token: %d", c.method, c.path, code)
		}
	}
	// It can ask who it is, and the owner's list names it without a token.
	code, b := call(t, "GET", sc.url+"/api/local-access/self", tok, "")
	if code != 200 || !strings.Contains(string(b), "A program") || strings.Contains(string(b), tok) {
		t.Fatalf("self: %d %s", code, b)
	}
	code, b = call(t, "GET", sc.url+"/api/local-access", sc.owner, "")
	if code != 200 || !strings.Contains(string(b), "A program") || strings.Contains(string(b), tok) || strings.Contains(string(b), strings.TrimPrefix(tok, localaccess.TokenPrefix)) {
		t.Fatalf("list: %d %s", code, b)
	}
	// The owner's own token has no "self".
	if code, _ := call(t, "GET", sc.url+"/api/local-access/self", sc.owner, ""); code != 404 {
		t.Fatalf("the owner's token has a self: %d", code)
	}
	// Revoking ends it at once.
	var list []localaccess.Entry
	_ = json.Unmarshal(b, &list)
	if code, _ := call(t, "DELETE", sc.url+"/api/local-access/"+list[0].ID, sc.owner, ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := call(t, "GET", sc.url+"/api/projects", tok, ""); code != 401 {
		t.Fatalf("a revoked token still works: %d", code)
	}
}

// The list of what a program may do is exactly this, and every other route the controller has refuses it.
var reviewedScopedRoutes = []string{
	"GET /api/integration/v1/projects", "POST /api/integration/v1/import", "PUT /api/integration/v1/waiting", "GET /api/integration/v1/projects/{}/tasks/{}/status", "GET /api/integration/v1/projects/{}/tasks/{}/events", // projected metadata only; raw event route remains forbidden
	"GET /api/projects", "GET /api/projects/{}", "GET /api/projects/{}/tasks", "GET /api/projects/{}/runs", "GET /api/projects/{}/runs/{}",
	"GET /api/projects/{}/questions", "GET /api/projects/{}/questions/{}", "GET /api/runners", "GET /api/control-center", "GET /api/local-access/self",
	"POST /api/projects/{}/tasks", "POST /api/projects/{}/tasks/{}/runs", "POST /api/projects/{}/runs/{}/stop", "POST /api/projects/{}/questions/{}/answer",
}

func TestAProgramMayDoExactlyWhatWasReviewed(t *testing.T) {
	var have []string
	for _, r := range scopedRoutes {
		have = append(have, r.method+" "+r.pattern)
	}
	sort.Strings(have)
	want := append([]string(nil), reviewedScopedRoutes...)
	sort.Strings(want)
	if strings.Join(have, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the routes a program may use changed.\n got: %v\nwant: %v\nA new route here is a decision about what another program on this computer may do: read it, and update this list in the same change.", have, want)
	}
	// Nothing in the list reaches anything that changes settings, Git, repositories, runners' pairing or the token.
	for _, r := range scopedRoutes {
		if strings.HasPrefix(r.pattern, "/api/integration/v1/") {
			continue
		} // reviewed separately by integration tests; no raw event payloads
		for _, bad := range []string{"settings", "git", "github", "network", "folders", "pair", "revoke", "routing", "doctor", "update", "onboarding", "handoff", "continue", "worktrees", "execution", "orchestration", "usage", "assessment", "refresh", "events", "archive"} {
			if strings.Contains(r.pattern, bad) {
				t.Errorf("%s %s mentions %q", r.method, r.pattern, bad)
			}
		}
	}
}

// Every route the controller registers that is not on the list refuses a program's token with a 403, whatever the handler would have done.
func TestEveryOtherRouteRefusesAProgram(t *testing.T) {
	sc := newScoped(t)
	tok := sc.mint(t, "A program")
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`HandleFunc\("([A-Z]+) (/api/[^"]*)"`)
	ms := re.FindAllStringSubmatch(string(src), -1)
	if len(ms) < 40 {
		t.Fatalf("found only %d routes in server.go", len(ms))
	}
	checked := 0
	for _, m := range ms {
		method, pattern := m[1], m[2]
		path := regexp.MustCompile(`\{[a-z]+\}`).ReplaceAllString(pattern, "x")
		if _, ok := scopedRouteFor(method, path); ok {
			continue
		}
		if pattern == "/api/health" || strings.HasPrefix(pattern, "/api/runner/") {
			continue
		}
		checked++
		code, _ := call(t, method, sc.url+path, tok, `{}`)
		if code != 403 {
			t.Errorf("%s %s answered %d to a program's token, want 403", method, pattern, code)
		}
	}
	if checked < 30 {
		t.Fatalf("checked only %d refused routes", checked)
	}
}

// What a program may ask, it is asked with what a person could type, and not with how to run or when.
func TestAProgramHandsOverATaskAndStartsItWithTheTasksOwnSettings(t *testing.T) {
	sc := newScoped(t)
	tok := sc.mint(t, "A program")
	pid := sc.project.ID

	// The task: a title, a description, where it came from, which branches.
	body := `{"title":"WB-1: Fix it","description":"details","sourceRef":"https://x.example/t/1","workBranch":"wb-1-fix-it","baseBranch":"main"}`
	code, b := call(t, "POST", sc.url+"/api/projects/"+pid+"/tasks", tok, body)
	if code != 201 {
		t.Fatalf("create: %d %s", code, b)
	}
	var task domain.Task
	_ = json.Unmarshal(b, &task)
	// Its settings and its schedule are the owner's to give, not a program's.
	for name, extra := range map[string]string{
		"execution":     `"execution":{"agentId":"fake"}`,
		"orchestration": `"orchestration":{"mode":"scheduled"}`,
		"anything else": `"state":"doing"`,
	} {
		code, b := call(t, "POST", sc.url+"/api/projects/"+pid+"/tasks", tok, `{"title":"x",`+extra+`}`)
		if code != 403 || !strings.Contains(string(b), "does not include") {
			t.Errorf("a task with %s: %d %s", name, code, b)
		}
	}
	// Starting: nothing chooses an agent, a model, a policy, extra instructions or a runner.
	for name, extra := range map[string]string{
		"an agent": `{"agentId":"fake"}`, "a model": `{"model":"x"}`, "a policy": `{"policy":{"interaction":"autonomous"}}`,
		"instructions": `{"instructions":"run rm -rf /"}`, "a runner": `{"runnerId":"r"}`, "resume": `{"resume":true}`,
	} {
		if code, b := call(t, "POST", sc.url+"/api/projects/"+pid+"/tasks/"+task.ID+"/runs", tok, extra); code != 403 {
			t.Errorf("a start with %s: %d %s", name, code, b)
		}
	}
	// With nothing chosen, it starts with what the task and the project say (here the owner made the fake agent the default by naming it first).
	if code, b := call(t, "POST", sc.url+"/api/projects/"+pid+"/tasks/"+task.ID+"/runs", tok, `{}`); code == 403 {
		t.Fatalf("a plain start was refused by the program's scope: %d %s", code, b)
	}
}

// A program's token does nothing from another computer, and nothing at all when the controller has none to give out.
func TestAProgramsTokenWorksOnlyFromThisComputer(t *testing.T) {
	sc := newScoped(t)
	tok := sc.mint(t, "A program")
	srv := New(Options{AuthRequired: true, Token: ownerToken, LocalAccess: sc.store, Store: nil, Version: "t"})
	h := srv.Handler()
	for remote, want := range map[string]int{"127.0.0.1:5555": http.StatusOK, "[::1]:5555": http.StatusOK, "192.168.1.9:5555": http.StatusUnauthorized, "100.64.0.7:5555": http.StatusUnauthorized, "bad": http.StatusUnauthorized} {
		req := httptest.NewRequest("GET", "/api/local-access/self", nil)
		req.RemoteAddr = remote
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("from %s: %d, want %d", remote, rec.Code, want)
		}
	}
	// A controller with no store (the private network's listener) does not know the token at all.
	priv := New(Options{AuthRequired: true, Token: ownerToken, Version: "t"}).Handler()
	req := httptest.NewRequest("GET", "/api/projects", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	priv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a listener with no local access store accepted a program's token: %d", rec.Code)
	}
	// Without a required token there is nothing to scope: no tokens are given out.
	open := newTestServer(t, func(o *Options) {
		st, _ := localaccess.Open(filepath.Join(t.TempDir(), "x.json"))
		o.LocalAccess = st
	})
	if code, _ := call(t, "POST", open.URL+"/api/local-access", "", `{"name":"x"}`); code != 409 {
		t.Fatalf("minting on a controller that does not require its token: %d", code)
	}
}

func TestIntegrationGrantCannotControlRunsOrReadPrivateExecution(t *testing.T) {
	s := &Server{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/integration/v1/projects", "", 204},
		{"GET", "/api/integration/v1/projects/p/tasks/t/events?after=0", "", 204},
		{"GET", "/api/projects", "", 403},
		{"GET", "/api/projects/p/runs", "", 403},
		{"GET", "/api/events", "", 403},
		{"GET", "/api/runners", "", 403},
		{"POST", "/api/projects/p/tasks/t/runs", "{}", 403},
		{"POST", "/api/projects/p/runs/r/stop", "{}", 403},
		{"POST", "/api/projects/p/questions/q/answer", `{"answer":"text"}`, 403},
		{"POST", "/api/integration/v1/import", `{"execution":{"agentId":"agent"}}`, 403},
		{"PUT", "/api/integration/v1/waiting", `{"schema":"x","source":"y","items":[]}`, 204},
		{"PUT", "/api/integration/v1/waiting", `{"schema":"x","source":"y","items":[],"path":"/tmp"}`, 403},
		{"GET", "/api/integration/waiting", "", 403},
		{"POST", "/api/integration/waiting/link", `{"path":"/tmp","repository":"https://github.com/a/b"}`, 403},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.scoped(localaccess.Entry{Scope: "integration-v1"}, next).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != tc.want {
				t.Fatalf("%d expected %d", w.Code, tc.want)
			}
		})
	}
	w := httptest.NewRecorder()
	s.scoped(localaccess.Entry{Scope: "future-unknown"}, next).ServeHTTP(w, httptest.NewRequest("GET", "/api/integration/v1/projects", nil))
	if w.Code != 403 {
		t.Fatal("unknown scope failed open")
	}
}
