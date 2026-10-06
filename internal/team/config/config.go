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
	"strconv"
	"strings"
	"time"
)

// DefaultAddr listens on this computer only. A team server is reached by other
// people, so the person running it chooses a wider address on purpose; see
// docs/TEAM.md for putting it behind HTTPS.
const DefaultAddr = "127.0.0.1:7430"

// Defaults of the private network.
const (
	// DefaultBootstrapAddr is where a Workspace Host answers new devices that are
	// joining the workspace, over TLS, with the workspace's own identity.
	DefaultBootstrapAddr = "0.0.0.0:7440"
	// DefaultNetworkPort is the UDP port the private network's node listens on.
	DefaultNetworkPort = 4242
)

// Config holds Team's settings.
type Config struct {
	Addr            string
	DataDir         string
	LogLevel        string // debug, info, warn, error
	LogFormat       string // text or json
	ShutdownTimeout time.Duration

	// The customer-owned private network. Every setting here names a machine the
	// customer owns or a file on this one: none is a service, an account, a URL or a
	// key from anyone else (internal/archtest holds that line).
	//
	// BootstrapAddr is where this host answers devices that are joining; Endpoints are
	// the hosts (addresses or DNS names) at which this machine can be reached from outside
	// its own network, if it can, which the invitations and the network's discovery
	// advertise; NetworkPort is the UDP port the network node listens on. RunNode says
	// whether this host runs the network node itself (it needs the privileges to create a
	// network interface); NebulaDirs are extra directories the pinned program may be in.
	// PassphraseFile, if set, names a file holding the passphrase the workspace's keys are
	// sealed with, instead of a key kept beside the data.
	BootstrapAddr  string
	Endpoints      []string
	NetworkPort    int
	RunNode        bool
	NebulaDirs     []string
	PassphraseFile string
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{Addr: DefaultAddr, DataDir: defaultDataDir(), LogLevel: "info", LogFormat: "text", ShutdownTimeout: 10 * time.Second,
		BootstrapAddr: DefaultBootstrapAddr, NetworkPort: DefaultNetworkPort, RunNode: true}
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
	set("WERKBORD_TEAM_BOOTSTRAP_ADDR", &c.BootstrapAddr)
	set("WERKBORD_TEAM_PKI_PASSPHRASE_FILE", &c.PassphraseFile)
	if v := os.Getenv("WERKBORD_TEAM_ENDPOINTS"); v != "" {
		c.Endpoints = splitList(v)
	}
	if v := os.Getenv("WERKBORD_TEAM_NEBULA_DIR"); v != "" {
		c.NebulaDirs = splitList(v)
	}
	if v := os.Getenv("WERKBORD_TEAM_NETWORK_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.NetworkPort = n
		} else {
			c.NetworkPort = -1 // Validate reports it
		}
	}
	if v := os.Getenv("WERKBORD_TEAM_NETWORK_NODE"); v == "off" || v == "0" || v == "false" {
		c.RunNode = false
	}
	return c
}

func splitList(v string) []string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
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
	if _, _, err := net.SplitHostPort(c.BootstrapAddr); err != nil {
		return fmt.Errorf("bootstrap addr %q: %w", c.BootstrapAddr, err)
	}
	if c.NetworkPort < 1 || c.NetworkPort > 65535 {
		return fmt.Errorf("network port %d: want 1-65535", c.NetworkPort)
	}
	for _, e := range c.Endpoints {
		if e == "" || strings.ContainsAny(e, " \t\r\n/\\\"") || strings.Contains(e, "://") || (strings.Contains(e, ":") && net.ParseIP(strings.Trim(e, "[]")) == nil) {
			return fmt.Errorf("endpoint %q: give a host (an IP address or a DNS name) at which this machine can be reached, without a scheme or a port", e)
		}
	}
	return nil
}

// PKIDir is where this host keeps the workspace's keys, sealed (internal/team/infra/pki).
// It is never replicated with the database.
func (c Config) PKIDir() string { return filepath.Join(c.DataDir, "pki") }

// SealingKeyPath is the key the workspace's keys are sealed with, unless a passphrase is
// used. It is kept apart from the sealed files.
func (c Config) SealingKeyPath() string { return filepath.Join(c.DataDir, "secrets", "sealing.key") }

// NodeDir is the directory the network node's own files live in.
func (c Config) NodeDir() string { return filepath.Join(c.DataDir, "network") }

// BootstrapPort is the port of BootstrapAddr.
func (c Config) BootstrapPort() int {
	_, p, err := net.SplitHostPort(c.BootstrapAddr)
	n, _ := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
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
