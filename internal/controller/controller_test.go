package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"devboard/internal/config"
)

func testConfig(t *testing.T) config.Config {
	c := config.Default()
	c.Addr = "127.0.0.1:0"
	c.DataDir = t.TempDir()
	return c
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

func TestRequireTokenGeneratesAndEnforcesToken(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.RequireToken = true
	c := New(cfg, slog.New(slog.DiscardHandler), "test")
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(ctx)

	resp, err := http.Get("http://" + c.Addr() + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without token = %d", resp.StatusCode)
	}
	tok, err := cfg.ResolveToken(false)
	if err != nil || tok == "" {
		t.Fatalf("token not generated: %q %v", tok, err)
	}
	req, _ := http.NewRequest("GET", "http://"+c.Addr()+"/api/projects", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with token = %d", resp.StatusCode)
	}
}
