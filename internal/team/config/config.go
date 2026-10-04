// Package config holds Werkbord Team's settings. Precedence, lowest to highest:
// built-in defaults, WERKBORD_TEAM_* environment variables, then command-line flags
// (applied by the caller). Team has its own settings, data directory and
// variables so that it and the individual product can run side by side on one
// computer without sharing anything.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// DefaultAddr listens on this computer only. A team server is reached by other
// people, so the person running it chooses a wider address on purpose; see
// docs/TEAM.md for putting it behind HTTPS.
const DefaultAddr = "127.0.0.1:7430"

// Config holds Team's settings.
type Config struct {
	Addr            string
	DataDir         string
	LogLevel        string // debug, info, warn, error
	LogFormat       string // text or json
	ShutdownTimeout time.Duration
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{Addr: DefaultAddr, DataDir: defaultDataDir(), LogLevel: "info", LogFormat: "text", ShutdownTimeout: 10 * time.Second}
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "werkbord-team")
	}
	return ".werkbord-team"
}

// Load returns the defaults overlaid with the environment.
func Load() Config {
	c := Default()
	set := func(key string, dst *string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	set("WERKBORD_TEAM_ADDR", &c.Addr)
	set("WERKBORD_TEAM_DATA_DIR", &c.DataDir)
	set("WERKBORD_TEAM_LOG_LEVEL", &c.LogLevel)
	set("WERKBORD_TEAM_LOG_FORMAT", &c.LogFormat)
	return c
}

// Validate checks the settings for mistakes.
func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("addr %q: %w", c.Addr, err)
	}
	if c.DataDir == "" {
		return fmt.Errorf("the data directory is required")
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("log format %q: want text or json", c.LogFormat)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("the shutdown timeout must be positive")
	}
	return nil
}

// DBPath is the SQLite database.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "team.db") }

// IsLoopback reports whether Addr only listens on this computer.
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

// ClientAddr is the address to open in a browser on this computer.
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
