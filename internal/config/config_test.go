package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA_DIR", dir)
	t.Setenv("DEVBOARD_LOG_LEVEL", "debug")
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"addr":"127.0.0.1:9999","logLevel":"warn","shutdownTimeout":"3s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:9999" || c.LogLevel != "debug" || c.ShutdownTimeout.Duration != 3*time.Second || c.DataDir != dir {
		t.Fatalf("unexpected config: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthRequiredOffLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7420": false,
		"localhost:7420": false,
		"[::1]:7420":     false,
		"0.0.0.0:7420":   true,
		"192.168.1.2:80": true,
		":7420":          true,
	}
	for addr, want := range cases {
		c := Default()
		c.Addr = addr
		if got := c.AuthRequired(); got != want {
			t.Errorf("AuthRequired(%s) = %v, want %v", addr, got, want)
		}
	}
	c := Default()
	c.RequireToken = true
	if !c.AuthRequired() {
		t.Error("RequireToken should force auth on loopback")
	}
}

func TestClientAddr(t *testing.T) {
	c := Default()
	c.Addr = "0.0.0.0:7420"
	if got := c.ClientAddr(); got != "127.0.0.1:7420" {
		t.Fatalf("ClientAddr = %s", got)
	}
}

func TestResolveTokenGeneratesOnce(t *testing.T) {
	c := Default()
	c.DataDir = t.TempDir()
	if tok, err := c.ResolveToken(false); err != nil || tok != "" {
		t.Fatalf("without create: %q, %v", tok, err)
	}
	a, err := c.ResolveToken(true)
	if err != nil || len(a) != 64 {
		t.Fatalf("generated %q, %v", a, err)
	}
	b, _ := c.ResolveToken(true)
	if a != b {
		t.Fatal("token should be stable once generated")
	}
	info, _ := os.Stat(c.TokenPath())
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v", info.Mode().Perm())
	}
}
