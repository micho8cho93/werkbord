package controller

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"werkbord/internal/config"
	"werkbord/internal/netprivate"
	"werkbord/internal/relay/relaytest"
)

// fakeNode is a private network node that listens on loopback instead of a tailnet.
type fakeNode struct {
	mu     sync.Mutex
	st     netprivate.BackendStatus
	starts int
	lns    map[string]net.Listener
}

func newFakeNode() *fakeNode {
	return &fakeNode{lns: map[string]net.Listener{}, st: netprivate.BackendStatus{
		State: "Running", DNSName: "werkbord-test.tail1234.ts.net.", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.9")}, Tailnet: "me@example.com",
	}}
}

func (f *fakeNode) Start(context.Context) error { f.mu.Lock(); f.starts++; f.mu.Unlock(); return nil }
func (f *fakeNode) Status(context.Context) (netprivate.BackendStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st, nil
}
func (f *fakeNode) Listen(addr string, _ bool) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	f.mu.Lock()
	f.lns[addr] = l
	f.mu.Unlock()
	return l, err
}
func (f *fakeNode) Close() error { return nil }
func (f *fakeNode) addr() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l := f.lns[":80"]; l != nil {
		return l.Addr().String()
	}
	return ""
}

func startWith(t *testing.T, cfg config.Config, node *fakeNode) *Controller {
	t.Helper()
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	c.SetNetworkBackend(node)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Shutdown(ctx)
	})
	return c
}

func get(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func post(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader("{}"))
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
	return resp.StatusCode, string(b)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPrivateNetworkIsOffByDefault(t *testing.T) {
	node := newFakeNode()
	c := startWith(t, defaultConfig(t), node)
	tok, _ := c.cfg.ResolveToken(false)
	code, body := get(t, "http://"+c.Addr()+"/api/network", tok)
	var st netprivate.Status
	_ = json.Unmarshal([]byte(body), &st)
	if code != 200 || st.State != netprivate.StateOff || st.Enabled || st.URL != "" || node.starts != 0 {
		t.Fatalf("status = %d %+v (starts %d): nothing may join a network the user did not ask for", code, st, node.starts)
	}
}

func TestEnablingThePrivateNetworkServesOnlyWhatRemoteRunnersCall(t *testing.T) {
	node := newFakeNode()
	c := startWith(t, defaultConfig(t), node)
	base := "http://" + c.Addr()
	tok, _ := c.cfg.ResolveToken(false)

	if code, _ := post(t, base+"/api/network/enable", ""); code != 401 {
		t.Fatalf("enable without a token = %d", code)
	}
	if code, body := post(t, base+"/api/network/enable", tok); code != 200 {
		t.Fatalf("enable = %d %s", code, body)
	}
	waitFor(t, "the node to be served", func() bool { return node.addr() != "" })
	var st netprivate.Status
	waitFor(t, "connected", func() bool {
		_, body := get(t, base+"/api/network", tok)
		_ = json.Unmarshal([]byte(body), &st)
		return st.State == netprivate.StateConnected
	})
	if st.URL != "http://werkbord-test.tail1234.ts.net/" {
		t.Fatalf("status = %+v", st)
	}
	if strings.Contains(mustJSON(st), tok) {
		t.Fatal("the status contains the access token")
	}

	// The private network is for the machines that run for this controller. The app, the rest of the API, a phone's
	// enrollment and the controller's own token are not served there, whatever is presented.
	priv := "http://" + node.addr()
	for _, path := range []string{"/", "/api/projects", "/api/update", "/api/settings", "/api/phones/enroll", "/api/phones", "/api/runners", "/api/runners/pair", "/api/network", "/api/health"} {
		for _, token := range []string{"", "wrong", tok} {
			if code, _ := get(t, priv+path, token); code != 404 {
				t.Errorf("%s over the private network (token %q) = %d, want 404", path, token, code)
			}
		}
	}
	// What a remote runner calls to join and to sync reaches the API, which answers it itself.
	// (An unknown runner is answered 404 by the API in its own JSON; the listener's own refusal is plain text.)
	for _, path := range []string{"/api/runner/join", "/api/runner/sync"} {
		if _, body := post(t, priv+path, ""); strings.HasPrefix(body, "404 page not found") {
			t.Errorf("%s was not served over the private network: %q", path, body)
		}
	}
	// The same paths on this computer still work for its owner.
	if code, _ := get(t, base+"/api/projects", tok); code != 200 {
		t.Fatalf("API on this computer = %d", code)
	}
}

func TestPrivateNetworkNeverTrustsTheNetworkEvenWhenLoopbackIsOpen(t *testing.T) {
	node := newFakeNode()
	cfg := testConfig(t) // requireToken=false: loopback needs no token
	on := true
	cfg.Network.Enabled = &on
	c := startWith(t, cfg, node)
	waitFor(t, "the node to be served", func() bool { return node.addr() != "" })

	if code, _ := get(t, "http://"+c.Addr()+"/api/projects", ""); code != 200 {
		t.Fatalf("loopback, opened on purpose, = %d", code)
	}
	// The open loopback is this computer's alone: the private network serves none of it, with the token or without.
	priv := "http://" + node.addr()
	tok, _ := c.cfg.ResolveToken(false)
	for _, token := range []string{"", tok} {
		if code, _ := get(t, priv+"/api/projects", token); code != 404 {
			t.Fatalf("the private network answered /api/projects (token %q): %d", token, code)
		}
		if code, _ := get(t, priv+"/api/update", token); code != 404 {
			t.Fatalf("the private network answered /api/update (token %q): %d", token, code)
		}
	}
	if code, _ := get(t, "http://"+c.Addr()+"/api/update", ""); code != 200 {
		t.Fatalf("update status on the open loopback = %d", code)
	}
}

func TestPrivateNetworkChoiceSurvivesARestartAndConfigCanPinIt(t *testing.T) {
	cfg := defaultConfig(t)
	node := newFakeNode()
	c := startWith(t, cfg, node)
	base := "http://" + c.Addr()
	tok, _ := c.cfg.ResolveToken(false)
	if code, body := post(t, base+"/api/network/enable", tok); code != 200 {
		t.Fatalf("enable = %d %s", code, body)
	}
	waitFor(t, "connected", func() bool { return node.addr() != "" })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// A new controller on the same data directory brings it up by itself.
	node2 := newFakeNode()
	c2 := startWith(t, cfg, node2)
	waitFor(t, "the network to come back after a restart", func() bool { return node2.addr() != "" })
	base2 := "http://" + c2.Addr()
	if code, body := post(t, base2+"/api/network/disable", tok); code != 200 {
		t.Fatalf("disable = %d %s", code, body)
	}
	var st netprivate.Status
	_, body := get(t, base2+"/api/network", tok)
	_ = json.Unmarshal([]byte(body), &st)
	if st.State != netprivate.StateOff {
		t.Fatalf("after disable = %+v", st)
	}
	if err := c2.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// Disabled is remembered too.
	node3 := newFakeNode()
	c3 := startWith(t, cfg, node3)
	time.Sleep(100 * time.Millisecond)
	if node3.starts != 0 {
		t.Fatalf("a network the user turned off was started")
	}

	// config.json can pin it on or off over what the app stored.
	_ = c3.Shutdown(ctx)
	off := false
	pinned := cfg
	pinned.Network.Enabled = &off
	c4 := startWith(t, pinned, newFakeNode())
	if code, _ := post(t, "http://"+c4.Addr()+"/api/network/enable", tok); code != 409 {
		t.Fatalf("enabling what config.json forbids = %d", code)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// withRelay gives cfg a relay to connect to, for tests of what a phone may do. The server is closed with the test.
func withRelay(t *testing.T, cfg *config.Config) {
	t.Helper()
	rl := relaytest.New()
	t.Cleanup(rl.Close)
	cfg.Relay.URL, cfg.Relay.ConnectorURL = rl.URL, "https://connect.example.pages.dev"
}

// phoneDoor serves, on a local port, what a phone meets through the relay: the controller's phone-side handler, whose
// requests are marked private and whose enrollment binds the phone's secret. The encrypted tunnel in front of it is
// the relay tests' business; the tests that use this are about what a phone may do once it is in.
func phoneDoor(t *testing.T, c *Controller) string {
	t.Helper()
	if c.relay == nil {
		t.Fatal("the controller has no relay: give the config one with withRelay")
	}
	s := httptest.NewServer(c.relay.srv.Handler)
	t.Cleanup(s.Close)
	return s.URL
}

// enrollPhone makes a code on the controller and enrolls a phone with it at door, and returns the phone's token.
func enrollPhone(t *testing.T, base, ownerToken, door, name string) string {
	t.Helper()
	code, body := post(t, base+"/api/phones/codes", ownerToken)
	var made struct{ Code string }
	_ = json.Unmarshal([]byte(body), &made)
	if code != 201 || made.Code == "" {
		t.Fatalf("code = %d %.200s", code, body)
	}
	resp, err := http.Post(door+"/api/phones/enroll", "application/json", strings.NewReader(`{"code":"`+made.Code+`","name":"`+name+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var enrolled struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&enrolled)
	if resp.StatusCode != 201 || enrolled.Token == "" {
		t.Fatalf("enroll = %d", resp.StatusCode)
	}
	return enrolled.Token
}
