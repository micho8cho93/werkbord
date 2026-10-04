// Package config loads controller settings. Precedence, lowest to highest:
// built-in defaults, <data dir>/config.json, DEVBOARD_* environment
// variables, then command-line flags (applied by the caller).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultAddr is loopback-only on purpose; see docs/ARCHITECTURE.md, Security.
const DefaultAddr = "127.0.0.1:7420"

// Config holds controller settings.
type Config struct {
	Addr      string `json:"addr"`
	DataDir   string `json:"dataDir"`
	LogLevel  string `json:"logLevel"`  // debug, info, warn, error
	LogFormat string `json:"logFormat"` // text or json

	// Token protects the API. It is required whenever Addr is not loopback
	// and is generated into <data dir>/token on first use if not set.
	Token string `json:"token,omitempty"`
	// RequireToken forces token auth even on loopback (e.g. behind a local
	// reverse proxy such as `tailscale serve`).
	RequireToken bool `json:"requireToken,omitempty"`
	// AllowedHosts are extra Host header values accepted in loopback mode.
	AllowedHosts []string `json:"allowedHosts,omitempty"`

	ShutdownTimeout Duration `json:"shutdownTimeout"`
}

// Duration is a time.Duration that marshals as a string like "10s".
type Duration struct{ time.Duration }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Default returns built-in defaults.
func Default() Config {
	return Config{
		Addr:            DefaultAddr,
		DataDir:         defaultDataDir(),
		LogLevel:        "info",
		LogFormat:       "text",
		ShutdownTimeout: Duration{10 * time.Second},
	}
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "devboard")
	}
	return ".devboard"
}

// Load returns defaults overlaid with the config file and environment.
// DEVBOARD_DATA_DIR is read first because it locates the config file.
func Load() (Config, error) {
	c := Default()
	if v := os.Getenv("DEVBOARD_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if err := c.loadFile(filepath.Join(c.DataDir, "config.json")); err != nil {
		return c, err
	}
	c.loadEnv()
	return c, nil
}

func (c *Config) loadFile(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	dataDir := c.DataDir
	if err := json.Unmarshal(b, c); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if c.DataDir == "" {
		c.DataDir = dataDir
	}
	return nil
}

func (c *Config) loadEnv() {
	set := func(key string, dst *string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	set("DEVBOARD_ADDR", &c.Addr)
	set("DEVBOARD_DATA_DIR", &c.DataDir)
	set("DEVBOARD_LOG_LEVEL", &c.LogLevel)
	set("DEVBOARD_LOG_FORMAT", &c.LogFormat)
	set("DEVBOARD_TOKEN", &c.Token)
	if v := os.Getenv("DEVBOARD_REQUIRE_TOKEN"); v == "1" || strings.EqualFold(v, "true") {
		c.RequireToken = true
	}
}

// Validate checks the configuration for mistakes.
func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("addr %q: %w", c.Addr, err)
	}
	if c.DataDir == "" {
		return errors.New("dataDir is required")
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("logFormat %q: want text or json", c.LogFormat)
	}
	if c.ShutdownTimeout.Duration <= 0 {
		return errors.New("shutdownTimeout must be positive")
	}
	return nil
}

// DBPath is the SQLite database location.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "devboard.db") }

// LockPath is the file that prevents two controllers sharing a data dir.
func (c Config) LockPath() string { return filepath.Join(c.DataDir, "controller.lock") }

// TokenPath is where a generated API token is stored.
func (c Config) TokenPath() string { return filepath.Join(c.DataDir, "token") }

// IsLoopback reports whether Addr only listens on the local machine.
func (c Config) IsLoopback() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// AuthRequired reports whether API requests must carry the token.
func (c Config) AuthRequired() bool { return c.RequireToken || !c.IsLoopback() }

// ClientAddr is the address a local CLI should dial to reach the controller.
func (c Config) ClientAddr() string {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return c.Addr
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// ResolveToken returns the configured token, or reads <data dir>/token. If
// create is true and neither exists, a new token is generated and saved with
// mode 0600.
func (c Config) ResolveToken(create bool) (string, error) {
	if c.Token != "" {
		return c.Token, nil
	}
	b, err := os.ReadFile(c.TokenPath())
	switch {
	case err == nil:
		return strings.TrimSpace(string(b)), nil
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	case !create:
		return "", nil
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(c.TokenPath(), []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}
