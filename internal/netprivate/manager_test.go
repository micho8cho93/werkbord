package netprivate

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackend is a node whose state a test sets, listening on loopback.
type fakeBackend struct {
	mu        sync.Mutex
	st        BackendStatus
	startErr  error
	statusErr error
	listens   []string
	listenErr error
	closed    bool
	lns       map[string]net.Listener
}

func (f *fakeBackend) set(st BackendStatus) { f.mu.Lock(); f.st = st; f.mu.Unlock() }

func (f *fakeBackend) Start(context.Context) error { return f.startErr }
func (f *fakeBackend) Status(context.Context) (BackendStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st, f.statusErr
}
func (f *fakeBackend) Listen(addr string, tls bool) (net.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listenErr != nil {
		return nil, f.listenErr
	}
	f.listens = append(f.listens, addr+map[bool]string{true: " tls", false: ""}[tls])
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if f.lns == nil {
		f.lns = map[string]net.Listener{}
	}
	f.lns[addr] = l
	return l, err
}
func (f *fakeBackend) Close() error { f.mu.Lock(); f.closed = true; f.mu.Unlock(); return nil }
func (f *fakeBackend) listened() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.listens...)
}

func (f *fakeBackend) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeBackend) addr(port string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lns[port].Addr().String()
}

func newManager(f *fakeBackend, h http.Handler) *Manager {
	return New(Options{Backend: f, Handler: h, PollFast: 5 * time.Millisecond, PollSlow: 20 * time.Millisecond})
}

func waitStatus(t *testing.T, m *Manager, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status(); ok(s) {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; status = %+v", what, m.Status())
	return Status{}
}

var ip = netip.MustParseAddr("100.101.102.103")

func TestOffUntilStarted(t *testing.T) {
	m := newManager(&fakeBackend{}, nil)
	if s := m.Status(); s.State != StateOff || s.Enabled || s.URL != "" || m.Enabled() {
		t.Fatalf("status = %+v", s)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("stopping what is not running: %v", err)
	}
}

func TestSignInThenConnectedWithHTTPS(t *testing.T) {
	f := &fakeBackend{st: BackendStatus{State: backendNeedsLogin, AuthURL: "https://login.example/a/abc"}}
	m := newManager(f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "app") }))
	m.Start()
	defer m.Stop(context.Background())

	// The user is sent to sign in; there is no address yet, and nothing listens.
	s := waitStatus(t, m, "needs login", func(s Status) bool { return s.State == StateNeedsLogin })
	if s.AuthURL != "https://login.example/a/abc" || s.URL != "" || !s.Enabled {
		t.Fatalf("status = %+v", s)
	}
	if len(f.listened()) != 0 {
		t.Fatalf("listening before sign-in: %v", f.listened())
	}

	// They sign in, and the tailnet issues certificates: the app is on 443, and 80 redirects there.
	f.set(BackendStatus{State: backendRunning, DNSName: "devboard-mac.tail1234.ts.net.", IPs: []netip.Addr{ip}, Tailnet: "me@example.com", HTTPS: true})
	s = waitStatus(t, m, "connected", func(s Status) bool { return s.State == StateConnected && len(f.listened()) == 2 })
	if s.URL != "https://devboard-mac.tail1234.ts.net/" || !s.HTTPS || s.HTTPSHint != "" || s.AuthURL != "" || s.Hostname != "devboard-mac.tail1234.ts.net" ||
		len(s.IPs) != 1 || s.IPs[0] != "100.101.102.103" || s.Tailnet != "me@example.com" {
		t.Fatalf("status = %+v", s)
	}
	if strings.Join(f.listened(), ",") != ":80,:443 tls" {
		t.Fatalf("listeners = %v", f.listened())
	}

	get := func(addr, host string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", "http://"+addr+"/x?y=1", nil)
		req.Host = host
		c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	// By name on port 80: sent to https.
	if resp, _ := get(f.addr(":80"), "devboard-mac.tail1234.ts.net"); resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != "https://devboard-mac.tail1234.ts.net/x?y=1" {
		t.Fatalf("redirect = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// By address on port 80: served, since a certificate does not cover an address.
	if resp, body := get(f.addr(":80"), "100.101.102.103"); resp.StatusCode != 200 || body != "app" {
		t.Fatalf("by address = %d %q", resp.StatusCode, body)
	}
	// The listener the app is behind is the handler it was given.
	if resp, body := get(f.addr(":443"), "devboard-mac.tail1234.ts.net"); resp.StatusCode != 200 || body != "app" {
		t.Fatalf("app = %d %q", resp.StatusCode, body)
	}
}

func TestWithoutHTTPSTheAppIsServedOnPort80AndTheHintSaysHowToGetHTTPS(t *testing.T) {
	f := &fakeBackend{st: BackendStatus{State: backendRunning, DNSName: "devboard.tail1234.ts.net.", IPs: []netip.Addr{ip}}}
	m := newManager(f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "app") }))
	m.Start()
	defer m.Stop(context.Background())
	s := waitStatus(t, m, "connected", func(s Status) bool { return s.State == StateConnected && len(f.listened()) == 1 })
	if s.URL != "http://devboard.tail1234.ts.net/" || s.HTTPS || !strings.Contains(s.HTTPSHint, "HTTPS") {
		t.Fatalf("status = %+v", s)
	}
	if f.listened()[0] != ":80" {
		t.Fatalf("listeners = %v", f.listened())
	}
	resp, err := http.Get("http://" + f.addr(":80") + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "app" {
		t.Fatalf("body = %q", b)
	}
}

func TestAddressIsUsedWhenThereIsNoName(t *testing.T) {
	f := &fakeBackend{st: BackendStatus{State: backendRunning, IPs: []netip.Addr{ip}, HTTPS: true}}
	m := newManager(f, http.NotFoundHandler())
	m.Start()
	defer m.Stop(context.Background())
	s := waitStatus(t, m, "connected", func(s Status) bool { return s.State == StateConnected })
	// HTTPS is claimed only when there is a name for the certificate to cover.
	if s.URL != "http://100.101.102.103/" || s.HTTPS {
		t.Fatalf("status = %+v", s)
	}
}

func TestNeedsApprovalIsItsOwnState(t *testing.T) {
	f := &fakeBackend{st: BackendStatus{State: backendNeedsMachineAuth}}
	m := newManager(f, nil)
	m.Start()
	defer m.Stop(context.Background())
	s := waitStatus(t, m, "needs approval", func(s Status) bool { return s.State == StateNeedsApproval })
	if s.URL != "" || s.AuthURL != "" {
		t.Fatalf("status = %+v", s)
	}
}

func TestFailuresAreReportedNotHidden(t *testing.T) {
	f := &fakeBackend{startErr: errors.New("no state directory")}
	m := newManager(f, nil)
	m.Start()
	s := waitStatus(t, m, "error", func(s Status) bool { return s.State == StateError })
	if !strings.Contains(s.Error, "no state directory") {
		t.Fatalf("status = %+v", s)
	}
	_ = m.Stop(context.Background())

	// The node is up but the controller cannot listen on it.
	f = &fakeBackend{st: BackendStatus{State: backendRunning, DNSName: "a.ts.net."}, listenErr: errors.New("address in use")}
	m = newManager(f, nil)
	m.Start()
	defer m.Stop(context.Background())
	s = waitStatus(t, m, "listen error", func(s Status) bool { return s.State == StateError })
	if !strings.Contains(s.Error, "address in use") {
		t.Fatalf("status = %+v", s)
	}
}

func TestStopClosesEverythingAndStartingAgainWorks(t *testing.T) {
	f := &fakeBackend{st: BackendStatus{State: backendRunning, DNSName: "a.ts.net.", IPs: []netip.Addr{ip}}}
	m := newManager(f, http.NotFoundHandler())
	m.Start()
	m.Start() // a second start does nothing
	waitStatus(t, m, "connected", func(s Status) bool { return s.State == StateConnected && len(f.listened()) == 1 })
	addr := f.addr(":80")
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := m.Status(); s.State != StateOff || s.Enabled || s.URL != "" {
		t.Fatalf("after stop = %+v", s)
	}
	if !f.isClosed() {
		t.Fatal("the node was not closed")
	}
	if _, err := http.Get("http://" + addr + "/"); err == nil {
		t.Fatal("the listener still answers after Stop")
	}
}

func TestDefaultHostname(t *testing.T) {
	for in, want := range map[string]string{
		"Michels-MacBook-Pro.local": "werkbord-michels-macbook-pro",
		"DESKTOP_X1":                "werkbord-desktop-x1",
		"":                          "werkbord",
		"localhost":                 "werkbord",
		"  weird  host!! ":          "werkbord-weird-host",
		strings.Repeat("a", 100):    "werkbord-" + strings.Repeat("a", 54),
	} {
		got := DefaultHostname(in)
		if got != want || len(got) > 63 {
			t.Errorf("DefaultHostname(%q) = %q, want %q", in, got, want)
		}
	}
}

// A node that joined before the rename keeps its name: a phone opens that address.
func TestANodeKeepsTheNameItJoinedWith(t *testing.T) {
	fresh := filepath.Join(t.TempDir(), "tailscale")
	if got := HostnameFor(fresh, "mac"); got != "werkbord-mac" {
		t.Fatalf("new node: %s", got)
	}
	_ = os.WriteFile(filepath.Join(fresh, "tailscaled.state"), []byte("{}"), 0o600)
	if got := HostnameFor(fresh, "mac"); got != "werkbord-mac" {
		t.Fatalf("a new node's name changed once it had state: %s", got)
	}

	legacy := filepath.Join(t.TempDir(), "tailscale")
	_ = os.MkdirAll(legacy, 0o700)
	_ = os.WriteFile(filepath.Join(legacy, "tailscaled.state"), []byte("{}"), 0o600)
	if got := HostnameFor(legacy, "mac"); got != "devboard-mac" {
		t.Fatalf("node from before the rename: %s", got)
	}
	if got := HostnameFor(legacy, "mac"); got != "devboard-mac" {
		t.Fatalf("second start: %s", got)
	}
}

func TestQRCodes(t *testing.T) {
	svg, err := QRSVG("https://devboard.tail1234.ts.net/#token=abc")
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "viewBox") || strings.Contains(svg, "token") {
		t.Fatalf("svg = %.80s, %v", svg, err)
	}
	txt, err := QRText("https://devboard.tail1234.ts.net/")
	if err != nil || strings.Count(txt, "\n") < 10 {
		t.Fatalf("text = %q, %v", txt, err)
	}
	lines := strings.Split(strings.TrimSuffix(txt, "\n"), "\n")
	for _, l := range lines {
		if len([]rune(l)) != len([]rune(lines[0])) {
			t.Fatalf("ragged QR code")
		}
	}
}
