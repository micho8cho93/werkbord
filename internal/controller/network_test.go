package controller

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"werkbord/internal/config"
	"werkbord/internal/netprivate"
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

func TestEnablingThePrivateNetworkServesTheAppWithTheTokenRequired(t *testing.T) {
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

	// On the private network the app shell is public and the API needs the token.
	priv := "http://" + node.addr()
	if code, _ := get(t, priv+"/", ""); code != 200 {
		t.Fatalf("app shell over the private network = %d", code)
	}
	if code, _ := get(t, priv+"/api/projects", ""); code != 401 {
		t.Fatalf("API without a token over the private network = %d", code)
	}
	if code, _ := get(t, priv+"/api/projects", "wrong"); code != 401 {
		t.Fatalf("API with a wrong token = %d", code)
	}
	if code, _ := get(t, priv+"/api/projects", tok); code != 200 {
		t.Fatalf("API with the token = %d", code)
	}
	// A phone can be told an update exists, with its token and not without; it can only be told: there is no way to
	// ask the controller to install anything from here.
	if code, _ := get(t, priv+"/api/update", ""); code != 401 {
		t.Fatalf("update status over the private network without a token = %d", code)
	}
	if code, body := get(t, priv+"/api/update", tok); code != 200 || !strings.Contains(body, `"current"`) {
		t.Fatalf("update status over the private network with the token = %d %s", code, body)
	}
	if code, _ := post(t, priv+"/api/update", tok); code == 200 {
		t.Fatalf("POST /api/update was accepted over the private network")
	}

	// Where a phone reaches the controller carries no credential: a phone is let in with its own, by an enrollment code.
	code, body := get(t, base+"/api/network/phone", tok)
	var phone struct {
		URL   string
		Link  string
		QRSVG string
		HTTPS bool
	}
	_ = json.Unmarshal([]byte(body), &phone)
	if code != 200 || phone.URL != st.URL || phone.Link != "" || phone.QRSVG != "" || strings.Contains(body, tok) {
		t.Fatalf("phone link = %d %.200s", code, body)
	}
	if code, _ := get(t, base+"/api/network/phone", ""); code != 401 {
		t.Fatalf("the phone link without a token = %d", code)
	}

	// Scanning the code enrolls a phone over the private network, with no token of the controller's, and the phone's own
	// token then works there on its reviewed routes and on no others. Revoking it ends it.
	code, body = post(t, base+"/api/phones/codes", tok)
	var enrollment struct {
		Link  string `json:"link"`
		QRSVG string `json:"qrSvg"`
	}
	_ = json.Unmarshal([]byte(body), &enrollment)
	if code != 201 || !strings.HasPrefix(enrollment.Link, "http://werkbord-test.tail1234.ts.net/#enroll=") || !strings.HasPrefix(enrollment.QRSVG, "<svg") || strings.Contains(body, tok) {
		t.Fatalf("enrollment code = %d %.200s", code, body)
	}
	secret := strings.TrimPrefix(enrollment.Link, "http://werkbord-test.tail1234.ts.net/#enroll=")
	enroll := func(code string) (int, string) {
		resp, err := http.Post(priv+"/api/phones/enroll", "application/json", strings.NewReader(`{"code":"`+code+`","name":"Test phone"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	code, body = enroll(secret)
	var phoneTok struct{ ID, Token string }
	_ = json.Unmarshal([]byte(body), &phoneTok)
	if code != 201 || !strings.HasPrefix(phoneTok.Token, "wbp_") {
		t.Fatalf("enroll = %d %s", code, body)
	}
	if code, _ := enroll(secret); code != 403 {
		t.Fatalf("a code used twice = %d", code)
	}
	if code, _ := get(t, priv+"/api/projects", phoneTok.Token); code != 200 {
		t.Fatalf("a phone's token over the private network = %d", code)
	}
	if code, _ := get(t, priv+"/api/phones", phoneTok.Token); code != 403 {
		t.Fatalf("a phone listing phones = %d", code)
	}
	if code, _ := get(t, priv+"/api/update", phoneTok.Token); code != 403 {
		t.Fatalf("a phone reading what is not on its list = %d", code)
	}
	if code, body := get(t, base+"/api/phones", tok); code != 200 || !strings.Contains(body, "Test phone") || strings.Contains(body, phoneTok.Token) {
		t.Fatalf("the owner's list of phones = %d %s", code, body)
	}
	req, _ := http.NewRequest("DELETE", base+"/api/phones/"+phoneTok.ID, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 204 {
		t.Fatalf("revoke: %v %v", err, resp)
	}
	if code, _ := get(t, priv+"/api/projects", phoneTok.Token); code != 401 {
		t.Fatalf("a revoked phone = %d", code)
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
	priv := "http://" + node.addr()
	if code, _ := get(t, priv+"/api/projects", ""); code != 401 {
		t.Fatalf("the private network inherited the open loopback: %d", code)
	}
	tok, _ := c.cfg.ResolveToken(false)
	if code, _ := get(t, priv+"/api/projects", tok); code != 200 {
		t.Fatalf("with the token = %d", code)
	}
	// Nor does what is new: the update status is the API's, and is behind the token there as well.
	if code, _ := get(t, "http://"+c.Addr()+"/api/update", ""); code != 200 {
		t.Fatalf("update status on the open loopback = %d", code)
	}
	if code, _ := get(t, priv+"/api/update", ""); code != 401 {
		t.Fatalf("the private network answered /api/update without the token: %d", code)
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

func TestPhoneLinkNeedsAConnectedNetwork(t *testing.T) {
	c := startWith(t, defaultConfig(t), newFakeNode())
	tok, _ := c.cfg.ResolveToken(false)
	if code, _ := get(t, "http://"+c.Addr()+"/api/network/phone", tok); code != 409 {
		t.Fatalf("a link before there is a network = %d", code)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
