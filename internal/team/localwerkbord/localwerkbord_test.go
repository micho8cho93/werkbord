package localwerkbord

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var bg = context.Background()

// fake is a Werkbord as far as the bridge can tell: it records what it was sent.
type fake struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	seen   []string          // "METHOD path"
	bodies map[string]string // "METHOD path" → body
	auth   map[string]string // "METHOD path" → Authorization
	routes map[string]func(w http.ResponseWriter, body string)
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, bodies: map[string]string{}, auth: map[string]string{}, routes: map[string]func(http.ResponseWriter, string){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.seen = append(f.seen, key)
		f.bodies[key] = string(b)
		f.auth[key] = r.Header.Get("Authorization")
		h := f.routes[key]
		f.mu.Unlock()
		if h == nil {
			http.Error(w, `{"error":{"code":"not_found","message":"no such thing"}}`, http.StatusNotFound)
			return
		}
		h(w, string(b))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) on(key string, h func(w http.ResponseWriter, body string)) { f.routes[key] = h }
func (f *fake) json(key, body string, code int) {
	f.on(key, func(w http.ResponseWriter, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	})
}
func (f *fake) client() *Client {
	c, err := New(f.srv.URL, "wba_secret")
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func TestOnlyAnAddressOnThisComputerIsAccepted(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:7420":  "http://127.0.0.1:7420",
		"http://localhost:7420/": "http://127.0.0.1:7420",
		"http://[::1]:7420":      "http://[::1]:7420",
		"http://127.0.0.1":       "http://127.0.0.1",
	} {
		got, err := CheckBase(in)
		if err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "https://127.0.0.1:7420", "http://192.168.1.5:7420", "http://100.64.0.1:7420", "http://example.com", "http://localhost.evil.example",
		"http://user:pw@127.0.0.1:7420", "http://127.0.0.1:7420/x", "http://127.0.0.1:7420?x=1", "ftp://127.0.0.1", "127.0.0.1:7420", "http://0.0.0.0:7420"} {
		if _, err := CheckBase(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// Even if a request were somehow aimed elsewhere, the connection is refused: nothing is resolved by name and only a literal
// loopback address is dialled.
func TestTheBridgeTalksOnlyToThisComputer(t *testing.T) {
	dial := loopbackOnly(&net.Dialer{Timeout: time.Second})
	for _, addr := range []string{"192.168.1.5:80", "10.0.0.1:80", "100.64.0.1:80", "example.com:80", "localhost:80", "[2001:db8::1]:80", "8.8.8.8:53", "noport"} {
		if c, err := dial(bg, "tcp", addr); err == nil {
			c.Close()
			t.Errorf("dialled %s", addr)
		}
	}
	// A redirect to somewhere else is not followed, and the token goes only where it was sent.
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = r.Header.Get("Authorization") }))
	defer other.Close()
	f := newFake(t)
	f.on("GET /api/local-access/self", func(w http.ResponseWriter, _ string) { http.Redirect(w, &http.Request{}, other.URL, http.StatusFound) })
	if _, err := f.client().Self(bg); err == nil {
		t.Fatal("a redirect was followed")
	}
	if leaked != "" {
		t.Fatalf("the token was sent on: %q", leaked)
	}
}

func TestConnectUsesTheControllersTokenOnceAndKeepsOnlyTheNarrowOne(t *testing.T) {
	f := newFake(t)
	f.json("POST /api/local-access", `{"id":"la_1","name":"Werkbord Team","token":"wba_abc"}`, 201)
	tok, err := Connect(bg, f.srv.URL, "controller-token", "Werkbord Team")
	if err != nil || tok != "wba_abc" {
		t.Fatalf("%q %v", tok, err)
	}
	if f.auth["POST /api/local-access"] != "Bearer controller-token" || !strings.Contains(f.bodies["POST /api/local-access"], "Werkbord Team") {
		t.Fatalf("auth %q body %q", f.auth["POST /api/local-access"], f.bodies["POST /api/local-access"])
	}
	// Something that is not Werkbord, or gives a token of the wrong kind, is not believed.
	f.json("POST /api/local-access", `{"token":"the-controllers-own-token"}`, 201)
	if _, err := Connect(bg, f.srv.URL, "x", "n"); err != ErrNotAnswering {
		t.Fatalf("err = %v", err)
	}
}

func TestWhatIsSentToWerkbordIsOnlyWhatALocalAccessTokenAllows(t *testing.T) {
	f := newFake(t)
	f.json("POST /api/projects/prj_1/tasks", `{"id":"tsk_1"}`, 201)
	f.json("POST /api/projects/prj_1/tasks/tsk_1/runs", `{"id":"run_1"}`, 201)
	c := f.client()
	id, err := c.CreateTask(bg, "prj_1", NewTask{Title: "WB-1: Fix", Description: "d", SourceRef: "https://t/1", WorkBranch: "wb-1", BaseBranch: "main"})
	if err != nil || id != "tsk_1" {
		t.Fatalf("%q %v", id, err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.bodies["POST /api/projects/prj_1/tasks"]), &sent)
	for k := range sent {
		switch k {
		case "title", "description", "sourceRef", "workBranch", "baseBranch":
		default:
			t.Errorf("a task was sent with %q: a local access token may not set it", k)
		}
	}
	rid, err := c.StartRun(bg, "prj_1", "tsk_1")
	if err != nil || rid != "run_1" {
		t.Fatalf("%q %v", rid, err)
	}
	if got := f.bodies["POST /api/projects/prj_1/tasks/tsk_1/runs"]; strings.TrimSpace(got) != "{}" {
		t.Fatalf("a run was started with %q: nothing may choose an agent, a model, a policy, instructions or a runner", got)
	}
	for k, v := range f.auth {
		if v != "Bearer wba_secret" {
			t.Errorf("%s was sent with %q", k, v)
		}
	}
	// What goes into a path is an identifier and nothing else.
	for _, bad := range []string{"", "../x", "a/b", "a b", "a?b", "a#b", strings.Repeat("a", 200)} {
		if _, err := c.StartRun(bg, "prj_1", bad); err == nil {
			t.Errorf("task id %q was accepted", bad)
		}
		if _, err := c.CreateTask(bg, bad, NewTask{Title: "x"}); err == nil {
			t.Errorf("project id %q was accepted", bad)
		}
	}
	// A ticket too long for a task is refused here, with the reason.
	if _, err := c.CreateTask(bg, "prj_1", NewTask{Title: "x", Description: strings.Repeat("a", MaxDescription+1)}); err == nil {
		t.Fatal("an oversized description was sent")
	}
}

func TestATeamProjectIsMatchedToTheOneProjectWithItsRemote(t *testing.T) {
	f := newFake(t)
	f.json("GET /api/projects", `{"projects":[
		{"id":"prj_a","name":"A","repository":{"remotes":[{"url":"git@github.com:acme/shop.git"}]}},
		{"id":"prj_b","name":"B","repository":{"remotes":[{"url":"https://github.com/acme/other"}]}},
		{"id":"prj_c","name":"C"}]}`, 200)
	c := f.client()
	p, err := c.ProjectFor(bg, "https://github.com/acme/shop")
	if err != nil || p.ID != "prj_a" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := c.ProjectFor(bg, "https://github.com/acme/none"); err == nil || !strings.Contains(err.Error(), "none of your Werkbord projects") {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.ProjectFor(bg, ""); err == nil {
		t.Fatal("no repository matched")
	}
	f.json("GET /api/projects", `{"projects":[{"id":"1","repository":{"remotes":[{"url":"https://github.com/acme/shop"}]}},{"id":"2","repository":{"remotes":[{"url":"git@github.com:acme/shop.git"}]}}]}`, 200)
	if _, err := c.ProjectFor(bg, "https://github.com/acme/shop"); err == nil || !strings.Contains(err.Error(), "cannot choose") {
		t.Fatalf("an ambiguous match was resolved: %v", err)
	}
}

func TestAnAgentsQuestionIsAnsweredWithAnOptionItOfferedOrInWordsWhenAllowed(t *testing.T) {
	f := newFake(t)
	f.json("GET /api/projects", `{"projects":[{"id":"prj_1"},{"id":"prj_2"}]}`, 200)
	f.json("GET /api/projects/prj_2/runs/run_9", `{"id":"run_9"}`, 200)
	f.json("GET /api/projects/prj_2/questions/q_1", `{"id":"q_1","runId":"run_9","prompt":"Run npm test?","options":["Yes","No"],"allowFreeText":false,"state":"pending"}`, 200)
	f.json("POST /api/projects/prj_2/questions/q_1/answer", `{}`, 200)
	c := f.client()
	if err := c.Answer(bg, "run_9", "q_1", "1", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.bodies["POST /api/projects/prj_2/questions/q_1/answer"], `"No"`) {
		t.Fatalf("body = %q", f.bodies["POST /api/projects/prj_2/questions/q_1/answer"])
	}
	if err := c.Answer(bg, "run_9", "q_1", "Yes", ""); err != nil {
		t.Fatal(err)
	}
	// Not one of the options; words where only options are wanted; another run's question.
	for name, call := range map[string]func() error{
		"not an option": func() error { return c.Answer(bg, "run_9", "q_1", "Maybe", "") },
		"out of range":  func() error { return c.Answer(bg, "run_9", "q_1", "7", "") },
		"words only":    func() error { return c.Answer(bg, "run_9", "q_1", "", "just do it") },
		"another run":   func() error { return c.Answer(bg, "run_x", "q_1", "0", "") },
		"a bad id":      func() error { return c.Answer(bg, "run_9", "../q", "0", "") },
	} {
		if err := call(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	f.json("GET /api/projects/prj_2/questions/q_1", `{"id":"q_1","runId":"run_9","options":[],"allowFreeText":true,"state":"answered"}`, 200)
	if err := c.Answer(bg, "run_9", "q_1", "", "again"); err == nil {
		t.Fatal("an answered question was answered again")
	}
}

func TestStoppingARunFindsItsProject(t *testing.T) {
	f := newFake(t)
	f.json("GET /api/projects", `{"projects":[{"id":"prj_1"},{"id":"prj_2"}]}`, 200)
	f.json("GET /api/projects/prj_2/runs/run_9", `{"id":"run_9"}`, 200)
	f.json("POST /api/projects/prj_2/runs/run_9/stop", `{}`, 200)
	if err := f.client().StopRun(bg, "run_9"); err != nil {
		t.Fatal(err)
	}
	if err := f.client().StopRun(bg, "run_none"); err == nil || !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusNamesNothingButCounts(t *testing.T) {
	f := newFake(t)
	f.json("GET /api/control-center", `{"runners":[{"online":true,"kind":"local","name":"Bo's Mac","hostname":"bo.local"}],"projects":[{"name":"Secret project"},{}],
		"questions":[{"prompt":"rm -rf?"}],"runs":[{"taskTitle":"x"},{},{}]}`, 200)
	st, err := f.client().Status(bg)
	if err != nil || st.Projects != 2 || st.ActiveRuns != 3 || st.NeedsInput != 1 || !st.Runner {
		t.Fatalf("%+v %v", st, err)
	}
	b, _ := json.Marshal(st)
	for _, leak := range []string{"Secret", "Bo's", "bo.local", "rm -rf"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("the status leaks %q: %s", leak, b)
		}
	}
}

func TestWhatWentWrongIsSaidInWords(t *testing.T) {
	// Nothing listening.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	c, _ := New("http://"+addr, "wba_x")
	if _, err := c.Self(bg); err != ErrNotRunning {
		t.Fatalf("err = %v", err)
	}
	// Access taken away.
	f := newFake(t)
	f.json("GET /api/local-access/self", `{"error":{"code":"unauthorized","message":"missing or invalid token"}}`, 401)
	if _, err := f.client().Self(bg); err != ErrAccessDenied {
		t.Fatalf("err = %v", err)
	}
	// Werkbord refusing.
	f.json("POST /api/projects/prj_1/tasks", `{"error":{"code":"forbidden","message":"this program's access to Werkbord does not include that"}}`, 403)
	_, err := f.client().CreateTask(bg, "prj_1", NewTask{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "does not include") {
		t.Fatalf("err = %v", err)
	}
	// Not Werkbord.
	f.json("GET /api/health", `{"hello":"world"}`, 200)
	if _, err := Probe(bg, f.srv.URL); err != ErrNotAnswering {
		t.Fatalf("err = %v", err)
	}
	f.json("GET /api/health", `{"status":"ok","version":"1.4.0"}`, 200)
	if h, err := Probe(bg, f.srv.URL); err != nil || h.Version != "1.4.0" {
		t.Fatalf("%+v %v", h, err)
	}
}
