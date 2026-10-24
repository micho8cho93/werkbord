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
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"devboard/internal/agentcatalog"
	"devboard/internal/domain"
	"devboard/internal/httpkit"
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
	// NoUpdateCheck stops the controller from ever asking GitHub whether a newer
	// release exists (what GET /api/update answers). It is the only thing the
	// controller would otherwise look up on its own; everything else it reaches
	// out for is something the user asked for.
	NoUpdateCheck bool `json:"noUpdateCheck,omitempty"`
	// EmbedOrigins are extra exact loopback origins (http://127.0.0.1:PORT) that may show the web app in a frame, besides
	// the desktop app's own window. It is for running the desktop shell from source and testing it in a browser; leave it
	// empty otherwise. Only loopback origins are accepted (httpkit.ParseEmbedOrigins).
	EmbedOrigins []string `json:"embedOrigins,omitempty"`

	ShutdownTimeout Duration `json:"shutdownTimeout"`

	// WorktreesDir is where the Git worktrees agents work in are created.
	// Default: <data dir>/worktrees.
	WorktreesDir string `json:"worktreesDir,omitempty"`
	// Agents tunes the coding agents, by adapter ID ("claude-code", "codex", "gemini-cli", "opencode").
	// Agents not mentioned use their defaults.
	Agents map[string]AgentConfig `json:"agents,omitempty"`
	// Network tunes the private network (see internal/netprivate).
	Network NetworkConfig `json:"network,omitempty"`
	// Relay is the optional way for a phone to reach this computer through a relay of your own, with no VPN app on the
	// phone (see docs/RELAY.md).
	Relay RelayConfig `json:"relay,omitempty"`
	// GitHub tunes the optional GitHub integration, which runs the user's own
	// GitHub CLI. Dev Board has no GitHub account or token of its own.
	GitHub GitHubConfig `json:"github,omitempty"`
	// Assistant tunes the conversational assistant (internal/assistant).
	Assistant AssistantConfig `json:"assistant,omitempty"`
}

// AssistantConfig is the user's choices for the assistant. The assistant borrows the sign-in of an installed CLI provider
// (Claude Code, Codex, Gemini CLI or OpenCode): it has no account or model of its own, and the commands, models and
// supported reasoning levels it offers are the ones set for those agents under "agents".
type AssistantConfig struct {
	// Disabled turns the assistant off: its API answers that it is unavailable, and nothing is ever started for it.
	Disabled bool `json:"disabled,omitempty"`
	// ReadOnly lets the assistant look at the board and nothing else: it is not even offered the operations that
	// propose changes.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Projects limits the assistant to these project IDs. Leave it out for every project.
	Projects []string `json:"projects,omitempty"`
	// TurnTimeoutSeconds bounds one message, from sending to the end of the reply. Default 300.
	TurnTimeoutSeconds int `json:"turnTimeoutSeconds,omitempty"`
	// IdleTimeoutSeconds is how long the provider may say nothing before an attempt is given up. Default 90.
	IdleTimeoutSeconds int `json:"idleTimeoutSeconds,omitempty"`
	// AuditRetentionDays is how long the record of what the assistant did is kept. Default 400; at least 30; -1 keeps it for ever.
	AuditRetentionDays int `json:"auditRetentionDays,omitempty"`
	// AttachmentRetentionMinutes is how long a file the person attached is kept if they do not send it. Default 30; 1 to 1440.
	AttachmentRetentionMinutes int `json:"attachmentRetentionMinutes,omitempty"`
	// AttachmentsDisabled stops the assistant accepting files at all.
	AttachmentsDisabled bool `json:"attachmentsDisabled,omitempty"`
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

// RelayConfig points at the two small things you deploy to your own Cloudflare account (docs/RELAY.md): the relay, a
// Worker that forwards encrypted bytes, and the connector, a static page a phone opens. Both empty means the relay is off.
type RelayConfig struct {
	// URL is the relay's address, such as https://werkbord-relay.<you>.workers.dev.
	URL string `json:"url,omitempty"`
	// ConnectorURL is the connector page's address, such as https://werkbord-connect.pages.dev. A QR code is a plain
	// link to it.
	ConnectorURL string `json:"connectorUrl,omitempty"`
	// Disabled turns the relay off, built-in addresses included. Phones can then only reach this computer over a
	// private network.
	Disabled bool `json:"disabled,omitempty"`
}

// The relay and connector that ship with Werkbord, so adding a phone works with nothing to configure. They run on the
// Cloudflare Workers Free plan and see only ciphertext (docs/RELAY.md). relay.url and relay.connectorUrl, or
// WERKBORD_RELAY_URL and WERKBORD_CONNECTOR_URL, replace them with your own; relay.disabled (or WERKBORD_RELAY=off)
// turns the relay off.
const (
	DefaultRelayURL     = "https://werkbord-relay.getwerkbord.workers.dev"
	DefaultConnectorURL = "https://werkbord-connect.pages.dev"
)

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
	AgentGeminiCLI  = "gemini-cli"
	AgentOpenCode   = "opencode"
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
		if agentcatalog.Known(id) {
			if a.PermissionMode != "" || a.ApprovalPolicy != "" || a.Sandbox != "" {
				return fmt.Errorf("agents.%s: runtime permission settings are managed by the ACP agent", id)
			}
			continue
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
		case AgentGeminiCLI:
			if a.PermissionMode != "" || a.ApprovalPolicy != "" || a.Sandbox != "" {
				return fmt.Errorf("agents.%s: permissionMode, approvalPolicy and sandbox are not supported settings for this ACP agent", id)
			}
			if len(a.Reasoning) > 0 {
				return fmt.Errorf("agents.%s: custom reasoning choices are not supported by Gemini CLI", id)
			}
		case AgentOpenCode:
			if a.PermissionMode != "" || a.ApprovalPolicy != "" || a.Sandbox != "" {
				return fmt.Errorf("agents.%s: permissionMode, approvalPolicy and sandbox are not supported settings for this ACP agent", id)
			}
		default:
			return fmt.Errorf("agents.%s: unknown agent (known: %s, %s, %s, %s)", id, AgentClaudeCode, AgentCodex, AgentGeminiCLI, AgentOpenCode)
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
func Load() (Config, error) { return load("") }

// LoadIn is Load for a data directory that is already known, which then wins over
// WERKBORD_DATA_DIR. The desktop app uses it: started from the Finder it has none
// of the environment a terminal gave `werkbord setup`, so it reads where the
// installed service keeps its data and uses that, rather than guessing.
func LoadIn(dataDir string) (Config, error) { return load(dataDir) }

func load(dataDir string) (Config, error) {
	c := Default()
	switch {
	case dataDir != "":
		c.DataDir = dataDir
	default:
		if v := Getenv("DATA_DIR"); v != "" {
			c.DataDir = v
		}
	}
	if err := c.loadFile(filepath.Join(c.DataDir, "config.json")); err != nil {
		return c, err
	}
	if err := c.loadEnv(); err != nil {
		return c, err
	}
	if dataDir != "" {
		c.DataDir = dataDir // the environment was read for everything else, but not for where the data is
	}
	c.Relay = c.Relay.withBuiltIn()
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
	set("RELAY_URL", &c.Relay.URL)
	set("CONNECTOR_URL", &c.Relay.ConnectorURL)
	if v, from := Env("RELAY"); v != "" {
		switch strings.ToLower(v) {
		case "off", "false", "0", "no":
			c.Relay.Disabled = true
		case "on", "true", "1", "yes":
			c.Relay.Disabled = false
		default:
			return fmt.Errorf("%s=%q: want on or off", from, v)
		}
	}
	if v, from := Env("NETWORK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", from, v)
		}
		c.Network.Enabled = &b
	}
	if v, from := Env("EMBED_ORIGINS"); v != "" {
		origins, err := httpkit.ParseEmbedOrigins(v)
		if err != nil {
			return fmt.Errorf("%s: %w", from, err)
		}
		c.EmbedOrigins = origins
	}
	if v, from := Env("NO_UPDATE_CHECK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", from, v)
		}
		c.NoUpdateCheck = b
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
	if err := c.Relay.validate(); err != nil {
		return err
	}
	if len(c.EmbedOrigins) > 0 {
		if _, err := httpkit.ParseEmbedOrigins(strings.Join(c.EmbedOrigins, ",")); err != nil {
			return fmt.Errorf("embedOrigins: %w", err)
		}
	}
	if strings.ContainsAny(c.GitHub.Command, "\x00\n") {
		return errors.New("github.command contains a control character")
	}
	if err := c.validateAssistant(); err != nil {
		return err
	}
	return c.validateAgents()
}

func (c Config) validateAssistant() error {
	a := c.Assistant
	for _, id := range a.Projects {
		if !strings.HasPrefix(id, domain.PrefixProject+"_") || len(id) > 80 || strings.ContainsAny(id, " \t\n\x00") {
			return fmt.Errorf("assistant.projects: %q is not a project id", id)
		}
	}
	if a.TurnTimeoutSeconds < 0 || a.TurnTimeoutSeconds > 3600 {
		return errors.New("assistant.turnTimeoutSeconds: want 1 to 3600")
	}
	if a.IdleTimeoutSeconds < 0 || a.IdleTimeoutSeconds > 3600 {
		return errors.New("assistant.idleTimeoutSeconds: want 1 to 3600")
	}
	if a.AuditRetentionDays != 0 && a.AuditRetentionDays != -1 && (a.AuditRetentionDays < 30 || a.AuditRetentionDays > 36500) {
		return errors.New("assistant.auditRetentionDays: want 30 or more, or -1 to keep it for ever")
	}
	if a.AttachmentRetentionMinutes < 0 || a.AttachmentRetentionMinutes > 1440 {
		return errors.New("assistant.attachmentRetentionMinutes: want 1 to 1440")
	}
	return nil
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

// LogPath is where the service writes the controller's log.
func (c Config) LogPath() string { return filepath.Join(c.DataDir, "logs", "controller.log") }

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

// ControllerURL is where this computer reaches the controller.
func (c Config) ControllerURL() string { return "http://" + c.ClientAddr() }

// SignInURL is a link that opens the web app on this computer already signed in:
// the token rides in the URL fragment, which a browser never sends to a server, and
// the app removes it from the address bar as soon as it has kept it. It is what
// `werkbord open` opens and what the desktop app loads. Without a token to present
// (authentication is off on loopback) it is just the address.
//
// It reads the token and creates nothing: the controller makes it on first start.
func (c Config) SignInURL() (string, error) {
	tok, err := c.ResolveToken(false)
	if err != nil {
		return "", err
	}
	if tok == "" || !c.AuthRequired() {
		return c.ControllerURL() + "/", nil
	}
	return c.ControllerURL() + "/#token=" + tok, nil
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

// Enabled reports whether the relay is set up: both addresses are needed, one without the other cannot make a link.
func (r RelayConfig) Enabled() bool { return !r.Disabled && r.URL != "" && r.ConnectorURL != "" }

// withBuiltIn fills in the relay that ships with Werkbord when neither address is set and the relay is not turned off.
// Setting only one address is left alone so validation can say they go together. A test binary never gets it: a unit
// test must not connect to a real relay.
func (r RelayConfig) withBuiltIn() RelayConfig { return r.builtIn(testing.Testing()) }

func (r RelayConfig) builtIn(inTest bool) RelayConfig {
	if r.Disabled || r.URL != "" || r.ConnectorURL != "" || inTest {
		return r
	}
	r.URL, r.ConnectorURL = DefaultRelayURL, DefaultConnectorURL
	return r
}

func (r RelayConfig) validate() error {
	for _, f := range []struct{ name, v string }{{"relay.url", r.URL}, {"relay.connectorUrl", r.ConnectorURL}} {
		if f.v == "" {
			continue
		}
		u, err := url.Parse(f.v)
		if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%s %q: want an address like https://name.example.com", f.name, f.v)
		}
		// A phone's browser only runs the connector over https. http is for trying it on this computer.
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return fmt.Errorf("%s %q: want https (http only for localhost)", f.name, f.v)
		}
	}
	if (r.URL == "") != (r.ConnectorURL == "") {
		return errors.New("relay.url and relay.connectorUrl go together: set both, or neither")
	}
	return nil
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
