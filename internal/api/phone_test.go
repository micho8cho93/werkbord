package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/netprivate"
	"devboard/internal/phoneaccess"
)

// A controller that requires its token, with a store of phones.
type phoned struct {
	*runsServer
	store *phoneaccess.Store
}

func newPhoned(t *testing.T, extra ...func(*Options)) *phoned {
	t.Helper()
	st, err := phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
	if err != nil {
		t.Fatal(err)
	}
	testAuthToken = ownerToken
	t.Cleanup(func() { testAuthToken = "" })
	p := &phoned{store: st}
	p.runsServer = newRunsServer(t, append([]func(*Options){func(o *Options) { o.AuthRequired, o.Token, o.Phones = true, ownerToken, st }}, extra...)...)
	return p
}

// enrolled makes a code and spends it, as a phone would, and returns the phone's token and id.
func (p *phoned) enrolled(t *testing.T, name string) (token, id string) {
	t.Helper()
	code, _, err := p.store.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	status, b := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(code)+`,"name":`+jsonString(name)+`}`)
	if status != 201 {
		t.Fatalf("enroll: %d %s", status, b)
	}
	var out struct{ ID, Token string }
	_ = json.Unmarshal(b, &out)
	if !strings.HasPrefix(out.Token, phoneaccess.TokenPrefix) {
		t.Fatalf("token = %q", out.Token)
	}
	return out.Token, out.ID
}

func TestAPhoneEnrollsWithACodeAndNeedsNoTokenToDoIt(t *testing.T) {
	p := newPhoned(t)
	tok, id := p.enrolled(t, "Michel's iPhone")
	status, b := call(t, "GET", p.url+"/api/phones/self", tok, "")
	if status != 200 || !strings.Contains(string(b), "Michel's iPhone") || !strings.Contains(string(b), id) || strings.Contains(string(b), tok) {
		t.Fatalf("self: %d %s", status, b)
	}
	// The owner's list names it, with no secret in it.
	status, b = call(t, "GET", p.url+"/api/phones", ownerToken, "")
	if status != 200 || !strings.Contains(string(b), "Michel's iPhone") || strings.Contains(string(b), tok) || strings.Contains(string(b), strings.TrimPrefix(tok, phoneaccess.TokenPrefix)) || strings.Contains(string(b), "hash") {
		t.Fatalf("list: %d %s", status, b)
	}
	// The owner's token is not a phone.
	if status, _ := call(t, "GET", p.url+"/api/phones/self", ownerToken, ""); status != 404 {
		t.Fatalf("the owner's token has a self: %d", status)
	}
	if status, _ := call(t, "POST", p.url+"/api/phones/self/signout", ownerToken, ""); status != 404 {
		t.Fatalf("the owner's token signed out: %d", status)
	}
}

func TestAWrongExpiredOrUsedCodeAllGetTheSameAnswer(t *testing.T) {
	p := newPhoned(t)
	used, _, _ := p.store.NewCode()
	if status, b := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(used)+`,"name":"A"}`); status != 201 {
		t.Fatalf("%d %s", status, b)
	}
	answers := map[string]string{}
	for name, code := range map[string]string{"used": used, "wrong": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "empty": "", "long": strings.Repeat("x", 5000)} {
		status, b := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(code)+`,"name":"B"}`)
		answers[name] = http.StatusText(status) + " " + string(b)
		if status != 403 {
			t.Errorf("%s code: %d %s", name, status, b)
		}
	}
	for name, a := range answers {
		if a != answers["wrong"] {
			t.Errorf("%s code is answered differently from a wrong one:\n%s\n%s", name, a, answers["wrong"])
		}
	}
	if len(p.store.List()) != 1 {
		t.Fatal("a bad code enrolled a phone")
	}
}

func TestEnrollmentIsRateLimited(t *testing.T) {
	p := newPhoned(t)
	var last int
	for i := 0; i < enrollAttemptsPerMinute+1; i++ {
		last, _ = call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":"nope","name":"A"}`)
	}
	if last != 429 {
		t.Fatalf("attempt %d = %d, want 429", enrollAttemptsPerMinute+1, last)
	}
	// Even a good code waits its turn.
	code, _, _ := p.store.NewCode()
	status, b := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(code)+`,"name":"A"}`)
	if status != 429 || !strings.Contains(string(b), "rate_limited") {
		t.Fatalf("a good code while limited: %d %s", status, b)
	}
}

func TestTheLimiterForgetsAfterTheWindow(t *testing.T) {
	now := time.Now()
	l := &attemptLimiter{max: 2, per: time.Minute, now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		if ok, _ := l.allow(); !ok {
			t.Fatal("refused too soon")
		}
	}
	if ok, wait := l.allow(); ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("third: %v %v", ok, wait)
	}
	now = now.Add(time.Minute + time.Second)
	if ok, _ := l.allow(); !ok {
		t.Fatal("still refused after the window")
	}
}

func TestOnlyTheOwnerMakesCodesSeesPhonesAndRevokesThem(t *testing.T) {
	p := newPhoned(t)
	tok, id := p.enrolled(t, "A")
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/phones/codes", "{}"}, {"GET", "/api/phones", ""}, {"DELETE", "/api/phones/" + id, ""},
	} {
		if status, _ := call(t, c.method, p.url+c.path, tok, c.body); status != 403 {
			t.Errorf("%s %s with a phone's token: %d", c.method, c.path, status)
		}
		if status, _ := call(t, c.method, p.url+c.path, "", c.body); status != 401 {
			t.Errorf("%s %s with no token: %d", c.method, c.path, status)
		}
	}
	if status, _ := call(t, "DELETE", p.url+"/api/phones/ph_nope", ownerToken, ""); status != 404 {
		t.Fatalf("revoking what is not there: %d", status)
	}
	if status, _ := call(t, "DELETE", p.url+"/api/phones/"+id, ownerToken, ""); status != 204 {
		t.Fatalf("revoke: %d", status)
	}
	if status, _ := call(t, "GET", p.url+"/api/projects", tok, ""); status != 401 {
		t.Fatalf("a revoked phone still works: %d", status)
	}
}

func TestAPhoneSignsItselfOut(t *testing.T) {
	p := newPhoned(t)
	tok, _ := p.enrolled(t, "A")
	other, _ := p.enrolled(t, "B")
	if status, _ := call(t, "POST", p.url+"/api/phones/self/signout", tok, ""); status != 204 {
		t.Fatalf("signout: %d", status)
	}
	if status, _ := call(t, "GET", p.url+"/api/phones/self", tok, ""); status != 401 {
		t.Fatalf("after signing out: %d", status)
	}
	if status, _ := call(t, "GET", p.url+"/api/phones/self", other, ""); status != 200 {
		t.Fatalf("signing one out signed another out: %d", status)
	}
}

// phoneStub is a network that is connected, for the link a code makes.
type phoneStub struct{ url string }

func (n phoneStub) Status() netprivate.Status {
	if n.url == "" {
		return netprivate.Status{State: netprivate.StateOff}
	}
	return netprivate.Status{State: netprivate.StateConnected, URL: n.url}
}
func (phoneStub) Enable(context.Context) error  { return nil }
func (phoneStub) Disable(context.Context) error { return nil }
func (phoneStub) Choice(context.Context) string { return "on" }

func TestTheCodeBecomesALinkWithTheCodeInTheFragmentAndNoToken(t *testing.T) {
	p := newPhoned(t, func(o *Options) { o.Network = phoneStub{url: "https://werkbord-x.tail1.ts.net/"} })
	status, b := call(t, "POST", p.url+"/api/phones/codes", ownerToken, "{}")
	var out struct {
		Link      string    `json:"link"`
		QRSVG     string    `json:"qrSvg"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	_ = json.Unmarshal(b, &out)
	if status != 201 || !strings.HasPrefix(out.Link, "https://werkbord-x.tail1.ts.net/#enroll=") || !strings.HasPrefix(out.QRSVG, "<svg") {
		t.Fatalf("%d %s", status, b)
	}
	if d := time.Until(out.ExpiresAt); d < 4*time.Minute || d > 5*time.Minute+time.Second {
		t.Fatalf("expires in %v", d)
	}
	if strings.Contains(string(b), ownerToken) {
		t.Fatal("the controller's token is in the enrollment response")
	}
	// The code in the link works, once.
	code := strings.TrimPrefix(out.Link, "https://werkbord-x.tail1.ts.net/#enroll=")
	if status, b := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(code)+`,"name":"A"}`); status != 201 {
		t.Fatalf("%d %s", status, b)
	}
	if status, _ := call(t, "POST", p.url+"/api/phones/enroll", "", `{"code":`+jsonString(code)+`,"name":"A"}`); status != 403 {
		t.Fatalf("a replay: %d", status)
	}

	// No address to open, no link or QR code: only the code, for a client that has its own way to carry it.
	off := newPhoned(t, func(o *Options) { o.Network = phoneStub{} })
	status, b = call(t, "POST", off.url+"/api/phones/codes", ownerToken, "{}")
	var bare struct{ Code, Link, QRSVG string }
	_ = json.Unmarshal(b, &bare)
	if status != 201 || bare.Code == "" || bare.Link != "" || bare.QRSVG != "" {
		t.Fatalf("a code with no network: %d %s", status, b)
	}
}

// ---- the doors ----

// theDoors serves a handler on a private network listener whose clients appear to come from a tailnet address, and returns
// the URL of it. The request really passes through netprivate's listener, which is what marks it private.
type doorNode struct {
	mu  sync.Mutex
	lns map[string]net.Listener
}

func (d *doorNode) Start(context.Context) error { return nil }
func (d *doorNode) Status(context.Context) (netprivate.BackendStatus, error) {
	return netprivate.BackendStatus{State: "Running", DNSName: "werkbord-x.tail1.ts.net.", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.9")}}, nil
}
func (d *doorNode) Listen(addr string, _ bool) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	d.mu.Lock()
	d.lns[addr] = l
	d.mu.Unlock()
	return l, err
}
func (d *doorNode) Close() error { return nil }

func privateDoor(t *testing.T, h http.Handler) string {
	t.Helper()
	node := &doorNode{lns: map[string]net.Listener{}}
	// What arrives here comes over the tailnet: from a tailnet address, not from this computer.
	tailnet := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "100.64.0.7:41000"
		h.ServeHTTP(w, r)
	})
	m := netprivate.New(netprivate.Options{Backend: node, Handler: tailnet})
	m.Start()
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		node.mu.Lock()
		l := node.lns[":80"]
		node.mu.Unlock()
		if l != nil {
			return "http://" + l.Addr().String()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the private listener did not start")
	return ""
}

func TestAPhoneTokenIsAcceptedOnlyOnTheDoorsForPhones(t *testing.T) {
	st, _ := phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
	code, _, _ := st.NewCode()
	_, tok, err := st.Enroll(code, "A")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{AuthRequired: true, Token: ownerToken, Phones: st, Version: "t"})
	h := srv.Handler()
	priv := privateDoor(t, h)

	ask := func(handler http.Handler, remote string) int {
		req := httptest.NewRequest("GET", "/api/phones/self", nil)
		req.RemoteAddr = remote
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	// This computer.
	for _, remote := range []string{"127.0.0.1:5555", "[::1]:5555"} {
		if got := ask(h, remote); got != 200 {
			t.Errorf("loopback %s: %d", remote, got)
		}
	}
	// Any other way in, that the private network's listener did not mark: refused, as if there were no such token.
	for _, remote := range []string{"192.168.1.9:5555", "100.64.0.7:5555", "10.0.0.2:1", "bad"} {
		if got := ask(h, remote); got != 401 {
			t.Errorf("other door from %s: %d, want 401", remote, got)
		}
	}
	// The private network's listener, from a tailnet address.
	req, _ := http.NewRequest("GET", priv+"/api/phones/self", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("private door: %d", resp.StatusCode)
	}
	// A server with no store at all (nothing configured) does not know the token.
	bare := New(Options{AuthRequired: true, Token: ownerToken, Version: "t"}).Handler()
	if got := ask(bare, "127.0.0.1:1"); got != 401 {
		t.Errorf("no store: %d", got)
	}
}

func TestEnrollmentIsOnlyForTheDoorsForPhones(t *testing.T) {
	st, _ := phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
	code, _, _ := st.NewCode()
	h := New(Options{AuthRequired: true, Token: ownerToken, Phones: st, Version: "t"}).Handler()
	try := func(remote string) int {
		req := httptest.NewRequest("POST", "/api/phones/enroll", strings.NewReader(`{"code":`+jsonString(code)+`,"name":"A"}`))
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := try("192.168.1.9:1"); got != 403 {
		t.Fatalf("enrollment from another door: %d", got)
	}
	if len(st.List()) != 0 {
		t.Fatal("enrolled from another door")
	}
	// ...and the code was not spent by the refusal.
	if got := try("127.0.0.1:1"); got != 201 {
		t.Fatalf("enrollment from this computer: %d", got)
	}
	// Over the private network too.
	code2, _, _ := st.NewCode()
	priv := privateDoor(t, h)
	resp, err := http.Post(priv+"/api/phones/enroll", "application/json", strings.NewReader(`{"code":`+jsonString(code2)+`,"name":"B"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("enrollment over the private network: %d", resp.StatusCode)
	}
}

func TestOnTheAssistantDoorOnlyAPhoneComesIn(t *testing.T) {
	st, _ := phoneaccess.Open(filepath.Join(t.TempDir(), "phones.json"))
	code, _, _ := st.NewCode()
	_, tok, _ := st.Enroll(code, "A")
	h := New(Options{AuthRequired: true, Token: ownerToken, Phones: st, AssistantPhonesOnly: true, Version: "t"}).Handler()
	priv := privateDoor(t, h)
	get := func(path, token string) int {
		req, _ := http.NewRequest("GET", priv+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get("/api/assistant/providers", ownerToken); got != 403 {
		t.Errorf("the controller's token: %d", got)
	}
	// 503 is the assistant not being configured in this test: the request got past the door.
	if got := get("/api/assistant/providers", tok); got != 503 {
		t.Errorf("a phone's token: %d", got)
	}
	if got := get("/api/assistant/sessions/x/events", tok); got == 401 || got == 403 {
		t.Errorf("a phone's assistant stream: %d", got)
	}
	if got := get("/api/projects", ownerToken); got == 403 || got == 401 {
		t.Errorf("the controller's token is still the owner's elsewhere: %d", got)
	}
}

// ---- what a phone may do ----

// The list of what a phone may do is exactly this, and every other route the controller has refuses it.
var reviewedPhoneRoutes = []string{
	"GET /api/deck", "PUT /api/deck", "GET /api/deck/history", "POST /api/deck/workflows",
	"GET /api/phones/self", "PATCH /api/phones/self", "POST /api/phones/self/signout",
	"GET /api/events", "GET /api/onboarding", "GET /api/settings", "GET /api/agents", "GET /api/agents/{}/options", "GET /api/runners", "GET /api/labels",
	"GET /api/control-center", "GET /api/analytics", "GET /api/workspace/v1/summary",
	"GET /api/projects", "GET /api/projects/{}", "GET /api/projects/{}/analytics", "GET /api/projects/{}/schedule", "GET /api/projects/{}/orchestration",
	"GET /api/projects/{}/timeline", "GET /api/projects/{}/recommendations", "GET /api/projects/{}/tasks", "GET /api/projects/{}/tasks/{}/runs", "GET /api/projects/{}/runs", "GET /api/projects/{}/activity",
	"GET /api/projects/{}/runs/{}", "GET /api/projects/{}/runs/{}/events", "GET /api/projects/{}/questions", "GET /api/projects/{}/questions/{}",
	"GET /api/projects/{}/worktrees/{}",
	"GET /api/projects/{}/git", "GET /api/projects/{}/git/pull-requests", "GET /api/projects/{}/git/commits", "GET /api/projects/{}/git/compare",
	"GET /api/projects/{}/git/diff", "GET /api/projects/{}/git/changes", "GET /api/projects/{}/git/changes/diff", "GET /api/projects/{}/git/health",
	"POST /api/projects/{}/refresh", "POST /api/projects/{}/tasks", "PATCH /api/projects/{}/tasks/{}", "POST /api/projects/{}/tasks/archive-done",
	"POST /api/projects/{}/tasks/{}/runs", "POST /api/projects/{}/runs/{}/input", "POST /api/projects/{}/runs/{}/finish", "POST /api/projects/{}/runs/{}/stop",
	"POST /api/projects/{}/runs/{}/handoff", "PUT /api/projects/{}/runs/{}/handoff", "POST /api/projects/{}/runs/{}/continue",
	"PATCH /api/projects/{}/runs/{}/assessment", "POST /api/projects/{}/questions/{}/answer",
	"POST /api/projects/{}/git/fetch", "POST /api/projects/{}/git/push", "POST /api/projects/{}/git/merge/plan", "POST /api/projects/{}/git/merge",
	"POST /api/projects/{}/git/branches/delete-plan", "POST /api/projects/{}/git/branches/delete", "POST /api/projects/{}/git/worktrees/clean-plan",
	"POST /api/projects/{}/git/worktrees/clean", "POST /api/projects/{}/git/pull-requests", "POST /api/projects/{}/git/health/refresh", "POST /api/projects/{}/git/health/findings/{}/dismiss", "POST /api/projects/{}/git/health/findings/{}/reopen",
	"GET /api/assistant/providers", "GET /api/assistant/sessions", "POST /api/assistant/sessions", "GET /api/assistant/sessions/{}", "PATCH /api/assistant/sessions/{}",
	"DELETE /api/assistant/sessions/{}", "POST /api/assistant/sessions/{}/messages", "POST /api/assistant/sessions/{}/attachments", "DELETE /api/assistant/sessions/{}/attachments/{}",
	"POST /api/assistant/sessions/{}/cancel", "GET /api/assistant/sessions/{}/events", "POST /api/assistant/sessions/{}/actions/{}",
	"POST /api/assistant/sessions/{}/workspaces", "GET /api/assistant/sessions/{}/workspace-requests/{}", "POST /api/assistant/sessions/{}/workspace-requests/{}",
	"GET /api/assistant/audit", "GET /api/assistant/audit/verify",
}

func TestAPhoneMayDoExactlyWhatWasReviewed(t *testing.T) {
	var have []string
	for _, r := range phoneRoutes {
		have = append(have, r.method+" "+r.pattern)
	}
	sort.Strings(have)
	want := append([]string(nil), reviewedPhoneRoutes...)
	sort.Strings(want)
	if strings.Join(have, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the routes a phone may use changed.\n got: %v\nwant: %v\nA new route here is a decision about what a phone may do to your computer: read it, and update this list in the same change.", have, want)
	}
	// The route lists name nothing that changes settings, repositories, runners' pairing, the token, or other phones,
	// and nothing in Git that cannot be undone.
	for _, r := range phoneRoutes {
		for _, bad := range []string{"/settings/", "pair", "revoke", "routing", "doctor", "update", "network", "github", "folders", "connections", "mcp", "local-access", "phones/codes", "phones/enroll", "/execution", "integration", "usage"} {
			if strings.Contains(r.pattern, bad) {
				t.Errorf("%s %s mentions %q", r.method, r.pattern, bad)
			}
		}
		if r.method == "POST" && strings.HasSuffix(r.pattern, "/tasks") || r.method == "PATCH" && strings.HasSuffix(r.pattern, "/tasks/{}") {
			for _, f := range r.fields {
				if f == "execution" {
					t.Errorf("%s %s lets a phone change a task's execution settings", r.method, r.pattern)
				}
			}
		}
		if strings.HasSuffix(r.pattern, "/tasks/{}/runs") && r.method == "POST" {
			for _, f := range r.fields {
				if f == "agentId" || f == "model" || f == "policy" || f == "runnerId" || f == "reasoning" {
					t.Errorf("starting a run from a phone may not choose %s", f)
				}
			}
		}
	}
}

func registeredLoosely(registered map[string]bool, route string) bool {
	norm := regexp.MustCompile(`\{[a-z]*\}`).ReplaceAllString(route, "{}")
	for r := range registered {
		if regexp.MustCompile(`\{[a-z]*\}`).ReplaceAllString(r, "{}") == norm {
			return true
		}
	}
	return false
}

func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`HandleFunc\("([A-Z]+) (/api/[^"]*)"`).FindAllStringSubmatch(string(src), -1) {
		out[m[1]+" "+m[2]] = true
	}
	return out
}

// Every route the controller registers that is not on the list refuses a phone with a 403, whatever the handler would have done.
// Every one that is on the list gets past the phone's door (what it then says is the handler's business).
func TestEveryOtherRouteRefusesAPhoneAndEveryListedRouteIsLetThrough(t *testing.T) {
	p := newPhoned(t)
	tok, _ := p.enrolled(t, "A")
	routes := registeredRoutes(t)
	if len(routes) < 80 {
		t.Fatalf("found only %d routes in server.go", len(routes))
	}
	refused, allowed := 0, 0
	for route := range routes {
		method, pattern, _ := strings.Cut(route, " ")
		path := regexp.MustCompile(`\{[a-z]+\}`).ReplaceAllString(pattern, "x")
		if pattern == "/api/health" || strings.HasPrefix(pattern, "/api/runner/") || pattern == "/api/phones/enroll" {
			continue
		}
		if _, ok := phoneRouteFor(method, path); ok {
			allowed++
			if pattern == "/api/phones/self/signout" || pattern == "/api/events" || strings.HasSuffix(pattern, "/events") && strings.Contains(pattern, "assistant") {
				continue // streams: covered by the stream test
			}
			if status, b := call(t, method, p.url+path, tok, ""); status == 403 && strings.Contains(string(b), "does not include") || status == 401 {
				t.Errorf("%s %s is on a phone's list but was refused: %d %s", method, pattern, status, b)
			}
			continue
		}
		refused++
		status, b := call(t, method, p.url+path, tok, `{}`)
		if status != 403 {
			t.Errorf("%s %s answered %d to a phone's token, want 403", method, pattern, status)
		}
		if !strings.Contains(string(b), "does not include") && pattern != "/api/connections/oauth/callback" {
			t.Errorf("%s %s: the refusal does not say what it is: %s", method, pattern, b)
		}
	}
	if refused < 60 || allowed < 55 {
		t.Fatalf("checked %d refused and %d allowed routes", refused, allowed)
	}
}

func TestAPhoneCannotDoTheOwnersThings(t *testing.T) {
	p := newPhoned(t)
	tok, _ := p.enrolled(t, "A")
	pid := p.project.ID
	for name, c := range map[string]struct{ method, path, body string }{
		"change the settings":         {"PUT", "/api/settings/execution", `{}`},
		"register a repository":       {"POST", "/api/projects", `{"path":"/tmp"}`},
		"list the computer's folders": {"GET", "/api/folders", ""},
		"change a project's agents":   {"PUT", "/api/projects/" + pid + "/execution", `{}`},
		"pair a runner":               {"POST", "/api/runners/pair", `{}`},
		"revoke a runner":             {"POST", "/api/runners/r1/revoke", `{}`},
		"read the routing rules":      {"GET", "/api/routing-rules", ""},
		"turn the network off":        {"POST", "/api/network/disable", `{}`},
		"make an enrollment code":     {"POST", "/api/phones/codes", `{}`},
		"list phones":                 {"GET", "/api/phones", ""},
		"revoke another phone":        {"DELETE", "/api/phones/ph_other", ""},
		"mint a program's access":     {"POST", "/api/local-access", `{"name":"x"}`},
		"see program access":          {"GET", "/api/local-access", ""},
		"manage MCP clients":          {"POST", "/api/mcp/grants", `{}`},
		"use the connections":         {"GET", "/api/connections", ""},
		"approve an execution":        {"POST", "/api/execution/v1/approvals", `{}`},
		"sign in to GitHub":           {"POST", "/api/github/login", `{}`},
		"read the doctor":             {"GET", "/api/doctor", ""},
	} {
		if status, b := call(t, c.method, p.url+c.path, tok, c.body); status != 403 {
			t.Errorf("a phone could %s: %s %s = %d %s", name, c.method, c.path, status, b)
		}
	}
	// The controller's token is nowhere in anything a phone can read.
	if status, b := call(t, "GET", p.url+"/api/settings", tok, ""); status != 200 || strings.Contains(string(b), ownerToken) {
		t.Fatalf("settings: %d %s", status, b)
	}
}

func TestAPhoneWorksWithTasksAndRunsButDoesNotChooseHowTheyRun(t *testing.T) {
	p := newPhoned(t)
	tok, _ := p.enrolled(t, "A")
	pid := p.project.ID
	status, b := call(t, "POST", p.url+"/api/projects/"+pid+"/tasks", tok, `{"title":"From my phone","description":"d","workMode":"agent","labelIds":[]}`)
	if status != 201 {
		t.Fatalf("create: %d %s", status, b)
	}
	var task struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	_ = json.Unmarshal(b, &task)
	for name, body := range map[string]string{
		"a task with execution settings": `{"title":"x","execution":{"agentId":"fake"}}`,
		"a task of any other field":      `{"title":"x","id":"t1"}`,
	} {
		if status, b := call(t, "POST", p.url+"/api/projects/"+pid+"/tasks", tok, body); status != 403 || !strings.Contains(string(b), "this phone's access") {
			t.Errorf("%s: %d %s", name, status, b)
		}
	}
	patch := p.url + "/api/projects/" + pid + "/tasks/" + task.ID
	if status, b := call(t, "PATCH", patch, tok, `{"state":"doing","version":`+jsonNumber(task.Version)+`}`); status != 200 {
		t.Fatalf("move: %d %s", status, b)
	}
	if status, _ := call(t, "PATCH", patch, tok, `{"execution":{"agentId":"fake"},"version":1}`); status != 403 {
		t.Fatalf("a phone changed a task's execution settings: %d", status)
	}
	runs := patch + "/runs"
	for name, body := range map[string]string{
		"an agent": `{"agentId":"fake"}`, "a model": `{"model":"x"}`, "a policy": `{"policy":{"interaction":"autonomous"}}`, "a runner": `{"runnerId":"r"}`, "a reasoning level": `{"reasoning":"high"}`,
	} {
		if status, _ := call(t, "POST", runs, tok, body); status != 403 {
			t.Errorf("a start with %s: %d", name, status)
		}
	}
	// With nothing chosen, or only words for the agent, it starts with the task's own settings.
	if status, b := call(t, "POST", runs, tok, `{"instructions":"be careful","resume":false}`); status == 403 {
		t.Fatalf("a plain start was refused by the phone's scope: %d %s", status, b)
	}
	if status, _ := call(t, "POST", p.url+"/api/projects/"+pid+"/runs/r/continue", tok, `{"agentId":"x"}`); status != 403 {
		t.Fatalf("a continue that names an agent: %d", status)
	}
}

func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

// ---- revoking ends what is open ----

func TestRevokingAPhoneClosesItsOpenStream(t *testing.T) {
	p := newPhoned(t)
	tok, id := p.enrolled(t, "A")
	req, _ := http.NewRequest("GET", p.url+"/api/events?access_token="+tok, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// A second phone's stream must not be touched by the first one's revocation.
	otherTok, _ := p.enrolled(t, "B")
	req2, _ := http.NewRequest("GET", p.url+"/api/events?access_token="+otherTok, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	ended := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, bufio.NewReader(resp.Body))
		ended <- err
	}()
	select {
	case <-ended:
		t.Fatal("the stream ended before it was revoked")
	case <-time.After(200 * time.Millisecond):
	}
	if status, _ := call(t, "DELETE", p.url+"/api/phones/"+id, ownerToken, ""); status != 204 {
		t.Fatalf("revoke: %d", status)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream was still open 3 seconds after its phone was revoked")
	}
	// A new stream with the revoked token is refused, and the other phone's stream is still open.
	if status, _ := call(t, "GET", p.url+"/api/events?access_token="+tok, "", ""); status != 401 {
		t.Fatalf("a stream with a revoked token: %d", status)
	}
	done2 := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp2.Body); close(done2) }()
	select {
	case <-done2:
		t.Fatal("revoking one phone closed another's stream")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestAPhoneTokenWorksInTheStreamsQueryOnlyOnTheStream(t *testing.T) {
	p := newPhoned(t)
	tok, _ := p.enrolled(t, "A")
	if status, _ := call(t, "GET", p.url+"/api/projects?access_token="+tok, "", ""); status != 401 {
		t.Fatalf("a token in the query of another route: %d", status)
	}
}

func TestAPhoneAndTheOwnerCanRenameAPhone(t *testing.T) {
	p := newPhoned(t)
	tok, id := p.enrolled(t, "iPhone")
	other, otherID := p.enrolled(t, "iPad")
	if status, b := call(t, "PATCH", p.url+"/api/phones/self", tok, `{"name":"Michel's iPhone"}`); status != 200 || !strings.Contains(string(b), "Michel's iPhone") {
		t.Fatalf("a phone renaming itself: %d %s", status, b)
	}
	if status, _ := call(t, "PATCH", p.url+"/api/phones/self", tok, `{"name":"x","id":"ph_other"}`); status != 403 {
		t.Fatalf("a phone renaming with another field: %d", status)
	}
	if status, _ := call(t, "PATCH", p.url+"/api/phones/"+otherID, tok, `{"name":"mine now"}`); status != 403 {
		t.Fatalf("a phone renaming another phone: %d", status)
	}
	if status, _ := call(t, "PATCH", p.url+"/api/phones/self", tok, `{"name":""}`); status != 400 {
		t.Fatalf("an empty name: %d", status)
	}
	if status, b := call(t, "PATCH", p.url+"/api/phones/"+otherID, ownerToken, `{"name":"Desk iPad"}`); status != 200 || !strings.Contains(string(b), "Desk iPad") {
		t.Fatalf("the owner renaming: %d %s", status, b)
	}
	if status, _ := call(t, "PATCH", p.url+"/api/phones/self", ownerToken, `{"name":"x"}`); status != 404 {
		t.Fatalf("the owner's token renaming 'self': %d", status)
	}
	if status, _ := call(t, "PATCH", p.url+"/api/phones/ph_nope", ownerToken, `{"name":"x"}`); status != 404 {
		t.Fatalf("renaming what is not there: %d", status)
	}
	_, _ = other, id
}
