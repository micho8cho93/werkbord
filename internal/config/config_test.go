package config

import (
	"os"
	"path/filepath"
	"strings"
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

var (
	loopbackAddrs    = []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420"}
	nonLoopbackAddrs = []string{"0.0.0.0:7420", "192.168.1.2:80", ":7420"}
)

// The API can start processes on this computer, so it is authenticated unless
// someone deliberately turns that off.
func TestTokenIsRequiredByDefault(t *testing.T) {
	if !Default().RequireToken {
		t.Error("Default().RequireToken = false")
	}
	for _, addr := range append(append([]string{}, loopbackAddrs...), nonLoopbackAddrs...) {
		c := Default()
		c.Addr = addr
		if !c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = false with the default configuration", addr)
		}
	}
}

// Opting out is for loopback only: a controller reachable from the network
// is never unauthenticated, whatever the setting says.
func TestOptingOutNeverAppliesOffLoopback(t *testing.T) {
	for _, addr := range loopbackAddrs {
		c := Default()
		c.Addr, c.RequireToken = addr, false
		if c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = true after opting out", addr)
		}
	}
	for _, addr := range nonLoopbackAddrs {
		c := Default()
		c.Addr, c.RequireToken = addr, false
		if !c.AuthRequired() {
			t.Errorf("AuthRequired(%s) = false: opting out must not expose the network", addr)
		}
	}
}

func TestRequireTokenSources(t *testing.T) {
	cases := []struct {
		name    string
		file    string // contents of config.json, "" for none
		env     string
		want    bool
		wantErr bool
	}{
		{"nothing set", "", "", true, false},
		{"file false", `{"requireToken": false}`, "", false, false},
		{"file true", `{"requireToken": true}`, "", true, false},
		{"file without the key", `{"logLevel": "warn"}`, "", true, false},
		{"env false", "", "false", false, false},
		{"env 0", "", "0", false, false},
		{"env true", "", "true", true, false},
		{"env 1", "", "1", true, false},
		{"env beats file: on", `{"requireToken": false}`, "true", true, false},
		{"env beats file: off", `{"requireToken": true}`, "false", false, false},
		// Not understanding a setting must never quietly mean "unauthenticated".
		{"env nonsense", "", "off", true, true},
		{"env nonsense 2", "", "disabled", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DEVBOARD_DATA_DIR", dir)
			t.Setenv("DEVBOARD_REQUIRE_TOKEN", c.env)
			if c.file != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(c.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if c.wantErr {
				if err == nil || !strings.Contains(err.Error(), "DEVBOARD_REQUIRE_TOKEN") {
					t.Fatalf("Load err = %v, want an error naming DEVBOARD_REQUIRE_TOKEN", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequireToken != c.want {
				t.Errorf("RequireToken = %v, want %v", cfg.RequireToken, c.want)
			}
		})
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
