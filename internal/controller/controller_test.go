package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/config"
)

// defaultConfig is the shipped configuration, relocated to a temporary
// directory and a free port, and otherwise untouched.
func defaultConfig(t *testing.T) config.Config {
	c := config.Default()
	c.Addr = "127.0.0.1:0"
	c.DataDir = t.TempDir()
	return c
}

// testConfig is for tests of the controller's lifecycle, which are not about
// authentication; they opt out of it so they can talk to the API plainly.
func testConfig(t *testing.T) config.Config {
	c := defaultConfig(t)
	c.RequireToken = false
	return c
}

// syncBuffer is a log destination safe to read while the controller runs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func status(t *testing.T, method, url, token string, header map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestStartServeShutdown(t *testing.T) {
	ctx := context.Background()
	c := New(testConfig(t), slog.New(slog.DiscardHandler), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	base := "http://" + c.Addr()

	resp, err := http.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	var h struct{ Status, Version, Database string }
	_ = json.NewDecoder(resp.Body).Decode(&h)
	resp.Body.Close()
	if resp.StatusCode != 200 || h.Status != "ok" || h.Version != "test" || h.Database != "ok" {
		t.Fatalf("health = %d %+v", resp.StatusCode, h)
	}

	// An open SSE stream must not prevent graceful shutdown.
	stream, err := http.Get(base + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if line, _ := bufio.NewReader(stream.Body).ReadString('\n'); !strings.HasPrefix(line, "retry:") {
		t.Fatalf("unexpected first SSE line %q", line)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := c.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("shutdown took %v; SSE stream was not closed", time.Since(start))
	}
}

func TestSecondControllerIsRefused(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	a := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown(ctx)

	b := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := b.Start(ctx); err == nil || !strings.Contains(err.Error(), "already using") {
		b.Shutdown(ctx)
		t.Fatalf("second controller on same data dir: err = %v", err)
	}
}

// The regression test for the default: nothing is configured, and the API on
// loopback still refuses requests without the token.
func TestDefaultConfigurationRequiresAToken(t *testing.T) {
	ctx := context.Background()
	cfg := defaultConfig(t)
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)
	base := "http://" + c.Addr()

	for _, path := range []string{"/api/projects", "/api/control-center", "/api/events", "/api/agents"} {
		if got := status(t, "GET", base+path, "", nil); got != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, got)
		}
	}
	if got := status(t, "POST", base+"/api/projects", "", nil); got != http.StatusUnauthorized {
		t.Errorf("POST /api/projects without a token = %d, want 401", got)
	}
	if got := status(t, "GET", base+"/api/projects", "not-the-token", nil); got != http.StatusUnauthorized {
		t.Errorf("GET with a wrong token = %d, want 401", got)
	}
	if got := status(t, "GET", base+"/api/health", "", nil); got != http.StatusOK {
		t.Errorf("/api/health = %d, want 200 (it is public)", got)
	}

	tok, err := cfg.ResolveToken(false)
	if err != nil || tok == "" {
		t.Fatalf("token not generated: %q %v", tok, err)
	}
	info, err := os.Stat(cfg.TokenPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v, %v; want mode 0600", info, err)
	}
	if got := status(t, "GET", base+"/api/projects", tok, nil); got != http.StatusOK {
		t.Errorf("GET with the token = %d, want 200", got)
	}
	// The event stream cannot send headers, so it takes the token in the query.
	if got := status(t, "GET", base+"/api/events?access_token="+tok, "", nil); got != http.StatusOK {
		t.Errorf("GET /api/events?access_token = %d, want 200", got)
	}
}

// A page the attacker controls can be made to resolve to 127.0.0.1 (DNS
// rebinding), so it reaches the API with a Host of its own choosing. The Host
// allowlist is skipped when a token is required, because the token is what
// stops it: a page on another origin cannot read this origin's token.
func TestRebindingStyleRequestsAreRefusedWithoutTheToken(t *testing.T) {
	ctx := context.Background()
	c := New(defaultConfig(t), slog.New(slog.DiscardHandler), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)

	hdr := map[string]string{"Host": "attacker.example:7420", "Origin": "http://attacker.example:7420"}
	for _, method := range []string{"GET", "POST"} {
		if got := status(t, method, "http://"+c.Addr()+"/api/projects", "", hdr); got != http.StatusUnauthorized {
			t.Errorf("%s with a foreign Host and no token = %d, want 401", method, got)
		}
	}
}

func TestOptingOutOfTheTokenOnLoopbackIsAllowedAndSaidAloud(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t) // RequireToken = false
	logs := &syncBuffer{}
	c := New(cfg, slog.New(slog.NewTextHandler(logs, nil)), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)

	if got := status(t, "GET", "http://"+c.Addr()+"/api/projects", "", nil); got != http.StatusOK {
		t.Errorf("GET without a token after opting out = %d, want 200", got)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "authentication is disabled") {
		t.Errorf("opting out must be logged as a warning; log was:\n%s", out)
	}
}

func TestAuthenticatedStartDoesNotWarn(t *testing.T) {
	ctx := context.Background()
	logs := &syncBuffer{}
	c := New(defaultConfig(t), slog.New(slog.NewTextHandler(logs, nil)), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)
	if strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("an authenticated controller logged a warning:\n%s", logs.String())
	}
}

// Opting out is for loopback only: bind to the network and the token comes back.
func TestOptOutDoesNotExposeANetworkListener(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.Addr = "0.0.0.0:0"
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)
	_, port, err := net.SplitHostPort(c.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if got := status(t, "GET", "http://127.0.0.1:"+port+"/api/projects", "", nil); got != http.StatusUnauthorized {
		t.Errorf("GET on a network listener with RequireToken=false and no token = %d, want 401", got)
	}
}
