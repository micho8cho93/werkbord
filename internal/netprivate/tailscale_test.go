package netprivate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

// These tests run the real embedded Tailscale node against an in-process
// coordination server and DERP relay (Tailscale's own test harness), so they
// exercise what the fakes cannot: the sign-in link, the node coming up, the
// controller listening on the tailnet and another node on the tailnet
// reaching it. Nothing leaves this computer.

func startControl(t *testing.T, requireAuth, magicDNS bool) *testcontrol.Server {
	t.Helper()
	netns.SetEnabled(false) // the harness is on loopback
	t.Cleanup(func() { netns.SetEnabled(true) })
	derpMap := integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1")
	control := &testcontrol.Server{
		DERPMap:     derpMap,
		DNSConfig:   &tailcfg.DNSConfig{Proxied: true},
		RequireAuth: requireAuth,
		Logf:        t.Logf,
	}
	if magicDNS {
		// A tailnet with MagicDNS also lists the node's name as one it can get a certificate for.
		control.MagicDNSDomain = "tail-scale.ts.net"
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	return control
}

func serveApp() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "devboard-app") })
}

// peer is another device on the tailnet, standing in for the user's phone.
func peer(t *testing.T, control *testcontrol.Server) *tsnet.Server {
	t.Helper()
	s := &tsnet.Server{Dir: filepath.Join(t.TempDir(), "peer"), Hostname: "phone", AuthKey: "tskey-peer", ControlURL: control.HTTPTestServer.URL,
		Ephemeral: true, Logf: logger.Discard, UserLogf: logger.Discard}
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := s.Up(ctx); err != nil {
		t.Fatalf("peer up: %v", err)
	}
	return s
}

func TestRealNodeWithAnAuthKeyServesTheAppOnTheTailnet(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Tailscale nodes")
	}
	// A tailnet with no MagicDNS and no certificates: the app is reached by address, over http.
	control := startControl(t, false, false)
	b := NewTailscale(TailscaleOptions{Dir: filepath.Join(t.TempDir(), "ts"), Hostname: "devboard-test", AuthKey: "tskey-test", ControlURL: control.HTTPTestServer.URL})
	m := New(Options{Backend: b, Handler: serveApp()})
	m.Start()
	defer m.Stop(context.Background())

	s := waitStatusFor(t, m, 60*time.Second, "connected", func(s Status) bool { return s.State == StateConnected && s.URL != "" })
	t.Logf("status: %+v", s)
	if len(s.IPs) == 0 || s.HTTPS || s.HTTPSHint == "" || s.URL != "http://"+s.IPs[0]+"/" {
		t.Fatalf("status = %+v: without certificates it must say so and serve http", s)
	}

	// Another device on the tailnet reaches the controller.
	phone := peer(t, control)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+s.IPs[0]+"/", nil)
	resp, err := phone.HTTPClient().Do(req)
	if err != nil {
		t.Fatalf("GET from the phone: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "devboard-app" {
		t.Fatalf("GET = %d %q", resp.StatusCode, body)
	}
}

func TestRealNodeOnATailnetWithHTTPSUsesItsName(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Tailscale nodes")
	}
	control := startControl(t, false, true)
	b := NewTailscale(TailscaleOptions{Dir: filepath.Join(t.TempDir(), "ts"), Hostname: "devboard-test", AuthKey: "tskey-test", ControlURL: control.HTTPTestServer.URL})
	m := New(Options{Backend: b, Handler: serveApp()})
	m.Start()
	defer m.Stop(context.Background())

	s := waitStatusFor(t, m, 60*time.Second, "connected", func(s Status) bool { return s.State == StateConnected && s.URL != "" })
	if !s.HTTPS || s.URL != "https://devboard-test.tail-scale.ts.net/" || s.HTTPSHint != "" || s.Error != "" {
		t.Fatalf("status = %+v", s)
	}
	// A device that types the plain-http name is sent to https.
	phone := peer(t, control)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://devboard-test.tail-scale.ts.net/board", nil)
	client := phone.HTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET from the phone: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != "https://devboard-test.tail-scale.ts.net/board" {
		t.Fatalf("redirect = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestRealNodeAsksTheUserToSignInThenComesUp(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Tailscale nodes")
	}
	control := startControl(t, true, true)
	b := NewTailscale(TailscaleOptions{Dir: filepath.Join(t.TempDir(), "ts"), Hostname: "devboard-login", ControlURL: control.HTTPTestServer.URL})
	m := New(Options{Backend: b, Handler: serveApp()})
	m.Start()
	defer m.Stop(context.Background())

	// With no key and no earlier sign-in, the user is given a link, and nothing else happens.
	s := waitStatusFor(t, m, 60*time.Second, "needs login", func(s Status) bool { return s.State == StateNeedsLogin && s.AuthURL != "" })
	if s.URL != "" {
		t.Fatalf("an address before sign-in: %+v", s)
	}
	t.Logf("sign-in link: %s", s.AuthURL)

	// The user signs in in their browser; the node comes up without any further step.
	if !control.CompleteAuth(s.AuthURL) {
		t.Fatalf("the harness did not know the link %s", s.AuthURL)
	}
	s = waitStatusFor(t, m, 60*time.Second, "connected", func(s Status) bool { return s.State == StateConnected && s.URL != "" })
	if s.AuthURL != "" || !strings.HasPrefix(s.Hostname, "devboard-login.") {
		t.Fatalf("status = %+v", s)
	}
}

func TestRealNodeKeepsItsIdentityAcrossARestart(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Tailscale nodes")
	}
	control := startControl(t, true, true)
	dir := filepath.Join(t.TempDir(), "ts")
	opt := TailscaleOptions{Dir: dir, Hostname: "devboard-again", ControlURL: control.HTTPTestServer.URL}

	m := New(Options{Backend: NewTailscale(opt), Handler: serveApp()})
	m.Start()
	s := waitStatusFor(t, m, 60*time.Second, "needs login", func(s Status) bool { return s.State == StateNeedsLogin && s.AuthURL != "" })
	control.CompleteAuth(s.AuthURL)
	first := waitStatusFor(t, m, 60*time.Second, "connected", func(s Status) bool { return s.State == StateConnected && s.URL != "" })
	if err := m.Stop(context.Background()); err != nil {
		t.Logf("stop: %v", err)
	}

	// A new controller process over the same data directory is already signed in:
	// the user is not asked again, and the address is the same.
	m2 := New(Options{Backend: NewTailscale(opt), Handler: serveApp()})
	m2.Start()
	defer m2.Stop(context.Background())
	second := waitStatusFor(t, m2, 60*time.Second, "connected again", func(s Status) bool {
		if s.State == StateNeedsLogin {
			t.Fatalf("asked to sign in again: %+v", s)
		}
		return s.State == StateConnected && s.URL != ""
	})
	if second.URL != first.URL || second.IPs[0] != first.IPs[0] {
		t.Fatalf("identity changed: %+v then %+v", first, second)
	}
}

func waitStatusFor(t *testing.T, m *Manager, d time.Duration, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s := m.Status(); ok(s) {
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; status = %+v", what, m.Status())
	return Status{}
}
