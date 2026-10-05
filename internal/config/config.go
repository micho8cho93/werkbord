// Package config loads controller settings. Precedence, lowest to highest:
// built-in defaults, <data dir>/config.json, WERKBORD_* environment
// variables, then command-line flags (applied by the caller).
//
// Werkbord was called Dev Board, and its variables DEVBOARD_*. Every DEVBOARD_
// name is still read, as a fallback for the WERKBORD_ one, so nothing an
// existing install or script sets stops working.
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
	"regexp"
	"strconv"
	"strings"
	"time"

	"devboard/internal/domain"
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
	// RequireToken makes the API demand the token on loopback too. It is on by
	// default, because the API can start processes on this computer and anything
	// local, including other users' programs, can reach a loopback port.
	// Setting it to false is for people who accept that; it never applies to a
	// non-loopback Addr, where the token is always required.
	RequireToken bool `json:"requireToken"`
	// AllowedHosts are extra Host header values accepted in loopback mode.
	AllowedHosts []string `json:"allowedHosts,omitempty"`

	ShutdownTimeout Duration `json:"shutdownTimeout"`

	// WorktreesDir is where the Git worktrees agents work in are created.
	// Default: <data dir>/worktrees.
	WorktreesDir string `json:"worktreesDir,omitempty"`
	// Agents tunes the coding agents, by adapter ID ("claude-code", "codex").
	// Agents not mentioned use their defaults.
	Agents map[string]AgentConfig `json:"agents,omitempty"`
	// Network tunes the private network (see internal/netprivate).
	Network NetworkConfig `json:"network,omitempty"`
	// GitHub tunes the optional GitHub integration, which runs the user's own
	// GitHub CLI. Dev Board has no GitHub account or token of its own.
	GitHub GitHubConfig `json:"github,omitempty"`
}

// NetworkConfig is the user's choices for the private network that lets a phone
// reach the controller. Whether it is on is normally chosen in the app (and by
// `werkbord setup`); the settings here are for pinning it from outside the app.
type NetworkConfig struct {
	// Enabled, if set, decides whether the private network is on, whatever the app
	// has stored: true for a headless server that must always join, false to forbid
	// it. Leave it out to let the app decide.
	Enabled *bool `json:"enabled,omitempty"`
	// Hostname is this controller's name on the tailnet. Default: "werkbord-" and
	// this computer's name.
	Hostname string `json:"hostname,omitempty"`
	// ControlURL is a self-hosted coordination server (Headscale) to use instead
	// of Tailscale's.
	ControlURL string `json:"controlUrl,omitempty"`
	// AuthKey is never read from this file: a key written to disk is a key that
	// leaks. Set WERKBORD_TS_AUTHKEY (or TS_AUTHKEY) to sign in without a browser.
}

// GitHubConfig is the user's choices for the GitHub integration.
type GitHubConfig struct {
	// Command is the GitHub CLI executable, if it is not on PATH as "gh".
	Command string `json:"command,omitempty"`
	// Disabled turns the integration off: no pull requests are shown or opened.
	// Everything local keeps working either way.
	Disabled bool `json:"disabled,omitempty"`
}

// AgentConfig is the user's choices for one coding agent. Fields that do not
// apply to an agent are rejected, so a typo or a misplaced setting is an error
// rather than something silently ignored.
type AgentConfig struct {
	// Command is the executable, if it is not on PATH under its usual name.
	Command string `json:"command,omitempty"`
	// Model overrides the agent's own default model: it is what "Agent default"
	// means for this agent on this computer.
	Model string `json:"model,omitempty"`
	// Models lists the models to offer when the agent cannot list its own, or
	// when you want a shorter list. Leave it out to use what the agent reports.
	// A model that is not listed can still be typed in.
	Models []ModelChoice `json:"models,omitempty"`
	// Reasoning lists the reasoning levels to offer, replacing what the agent reports.
	Reasoning []string `json:"reasoning,omitempty"`
	// PermissionMode (Claude Code): acceptEdits (the default), manual, plan,
	// auto, dontAsk or bypassPermissions.
	PermissionMode string `json:"permissionMode,omitempty"`
	// ApprovalPolicy (Codex): untrusted, on-request (the default) or never.
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	// Sandbox (Codex): read-only, workspace-write (the default) or danger-full-access.
	Sandbox string `json:"sandbox,omitempty"`
}

// ModelChoice is a model to offer in the pickers.
type ModelChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Agent IDs the configuration knows about; they match the adapters' IDs.
const (
	AgentClaudeCode = "claude-code"
	AgentCodex      = "codex"
)

var (
	claudePermissionModes = []string{"acceptEdits", "manual", "plan", "auto", "dontAsk", "bypassPermissions"}
	codexApprovalPolicies = []string{"untrusted", "on-request", "never"}
	codexSandboxes        = []string{"read-only", "workspace-write", "danger-full-access"}
)

var hostnameRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func oneOf(v string, allowed []string) bool {
	if v == "" {
		return true
	}
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// validateAgents checks the agent settings.
func (c Config) validateAgents() error {
	for id, a := range c.Agents {
		for _, m := range a.Models {
			if !domain.ValidModelName(m.ID) {
				return fmt.Errorf("agents.%s.models: %q is not a usable model name", id, m.ID)
			}
		}
		for _, r := range a.Reasoning {
			if !domain.ValidReasoning(r) {
				return fmt.Errorf("agents.%s.reasoning: %q is not a usable reasoning level", id, r)
			}
		}
		if a.Model != "" && !domain.ValidModelName(a.Model) {
			return fmt.Errorf("agents.%s.model: %q is not a usable model name", id, a.Model)
		}
		switch id {
		case AgentClaudeCode:
			if a.ApprovalPolicy != "" || a.Sandbox != "" {
				return fmt.Errorf("agents.%s: approvalPolicy and sandbox are Codex settings; use permissionMode", id)
			}
			if !oneOf(a.PermissionMode, claudePermissionModes) {
				return fmt.Errorf("agents.%s.permissionMode %q: want one of %s", id, a.PermissionMode, strings.Join(claudePermissionModes, ", "))
			}
		case AgentCodex:
			if a.PermissionMode != "" {
				return fmt.Errorf("agents.%s: permissionMode is a Claude Code setting; use approvalPolicy and sandbox", id)
			}
			if !oneOf(a.ApprovalPolicy, codexApprovalPolicies) {
				return fmt.Errorf("agents.%s.approvalPolicy %q: want one of %s", id, a.ApprovalPolicy, strings.Join(codexApprovalPolicies, ", "))
			}
			if !oneOf(a.Sandbox, codexSandboxes) {
				return fmt.Errorf("agents.%s.sandbox %q: want one of %s", id, a.Sandbox, strings.Join(codexSandboxes, ", "))
			}
		default:
			return fmt.Errorf("agents.%s: unknown agent (known: %s, %s)", id, AgentClaudeCode, AgentCodex)
		}
	}
	return nil
}

// RiskyAgentSettings lists settings that let an agent act without asking, for
// the controller to warn about at start-up.
func (c Config) RiskyAgentSettings() []string {
	var out []string
	if a, ok := c.Agents[AgentClaudeCode]; ok && a.PermissionMode == "bypassPermissions" {
		out = append(out, "agents.claude-code.permissionMode=bypassPermissions: Claude Code runs every tool without asking")
	}
	if a, ok := c.Agents[AgentCodex]; ok {
		if a.Sandbox == "danger-full-access" {
			out = append(out, "agents.codex.sandbox=danger-full-access: Codex commands are not sandboxed")
		}
		if a.ApprovalPolicy == "never" {
			out = append(out, "agents.codex.approvalPolicy=never: Codex never asks before acting")
		}
	}
	return out
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
		RequireToken:    true,
		ShutdownTimeout: Duration{10 * time.Second},
	}
}

// Env returns the value of WERKBORD_<name>, else of the older DEVBOARD_<name>,
// and the variable it came from ("" if neither is set).
func Env(name string) (value, from string) {
	for _, prefix := range []string{"WERKBORD_", "DEVBOARD_"} {
		if v := os.Getenv(prefix + name); v != "" {
			return v, prefix + name
		}
	}
	return "", ""
}

// Getenv is Env without saying where the value came from.
func Getenv(name string) string {
	v, _ := Env(name)
	return v
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return DataDirIn(dir)
	}
	return ".werkbord"
}

// DataDirIn is the data directory under a user configuration directory: werkbord,
// unless an install from before the rename keeps its data in devboard and there
// is no werkbord yet, in which case that one is used as it is. Nothing is moved.
func DataDirIn(configDir string) string {
	current := filepath.Join(configDir, "werkbord")
	legacy := filepath.Join(configDir, "devboard")
	if _, err := os.Stat(current); err != nil {
		if fi, err := os.Stat(legacy); err == nil && fi.IsDir() {
			return legacy
		}
	}
	return current
}

// Load returns defaults overlaid with the config file and environment.
// WERKBORD_DATA_DIR is read first because it locates the config file.
func Load() (Config, error) {
	c := Default()
	if v := Getenv("DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if err := c.loadFile(filepath.Join(c.DataDir, "config.json")); err != nil {
		return c, err
	}
	if err := c.loadEnv(); err != nil {
		return c, err
	}
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

func (c *Config) loadEnv() error {
	set := func(key string, dst *string) {
		if v := Getenv(key); v != "" {
			*dst = v
		}
	}
	set("ADDR", &c.Addr)
	set("DATA_DIR", &c.DataDir)
	set("LOG_LEVEL", &c.LogLevel)
	set("LOG_FORMAT", &c.LogFormat)
	set("TOKEN", &c.Token)
	set("WORKTREES_DIR", &c.WorktreesDir)
	set("NETWORK_HOSTNAME", &c.Network.Hostname)
	set("NETWORK_CONTROL_URL", &c.Network.ControlURL)
	if v, from := Env("NETWORK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", from, v)
		}
		c.Network.Enabled = &b
	}
	// A security setting must not be guessed at: "off" or "no" could mean
	// either, so anything that is not a plain boolean is an error rather than
	// a silent default.
	if v, from := Env("REQUIRE_TOKEN"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", from, v)
		}
		c.RequireToken = b
	}
	return nil
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
	if c.WorktreesDir != "" && !filepath.IsAbs(c.WorktreesDir) {
		return fmt.Errorf("worktreesDir %q must be an absolute path", c.WorktreesDir)
	}
	if h := c.Network.Hostname; h != "" && !hostnameRE.MatchString(h) {
		return fmt.Errorf("network.hostname %q: want a DNS label (letters, digits and hyphens, up to 63 characters)", h)
	}
	if u := c.Network.ControlURL; u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return fmt.Errorf("network.controlUrl %q: want an http(s) URL", u)
	}
	if strings.ContainsAny(c.GitHub.Command, "\x00\n") {
		return errors.New("github.command contains a control character")
	}
	return c.validateAgents()
}

// WorktreesPath is where agent worktrees are created.
func (c Config) WorktreesPath() string {
	if c.WorktreesDir != "" {
		return filepath.Clean(c.WorktreesDir)
	}
	return filepath.Join(c.DataDir, "worktrees")
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

// AuthRequired reports whether API requests must carry the token: always off
// loopback, and on loopback unless RequireToken has been turned off.
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
	if err := writeToken(c.TokenPath(), tok); err != nil {
		return "", err
	}
	return tok, nil
}

// RotateToken atomically replaces a file-managed credential. Configured tokens
// are explicit operator configuration and cannot be silently overridden.
func (c Config) RotateToken() (string, error) {
	if c.Token != "" {
		return "", fmt.Errorf("the API token is fixed in configuration; change that credential at its source and restart the controller")
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return "", err
	}
	return tok, writeToken(c.TokenPath(), tok)
}
func writeToken(path, token string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.WriteString(token + "\n")
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
