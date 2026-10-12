// Package acpcli adapts ACP-compatible command line agents for assistant
// conversations. It deliberately advertises no filesystem or terminal
// capabilities: the assistant can use only Werkbord's board protocol.
package acpcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/agent"
	"devboard/internal/assistant/provider"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

// Config describes one CLI's ACP entry point and model configuration.
type Config struct {
	ID, Name, Command, DocsURL string
	Args                       []string
	ModelFlag                  string
	// OpenCode accepts its model and reasoning choice as ACP config options.
	SessionOptions bool
	Models         []provider.Model
	Reasoning      []string
	CustomModels   bool
	ListModels     bool
}

type Provider struct {
	cfg      Config
	mu       sync.Mutex
	detected provider.Info
	detectAt time.Time
}

var _ provider.Provider = (*Provider)(nil)

func New(cfg Config) *Provider { return &Provider{cfg: cfg} }

func (p *Provider) ID() string { return p.cfg.ID }

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+[^\s]*`)

func (p *Provider) Detect(ctx context.Context) provider.Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.detectAt.IsZero() && time.Since(p.detectAt) < 30*time.Second {
		return p.detected
	}
	info := provider.Info{ID: p.cfg.ID, Name: p.cfg.Name, Capabilities: p.capabilities()}
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		info.Detail = fmt.Sprintf("%q was not found on PATH", p.cfg.Command)
		info.Guidance = p.installGuidance()
		p.detected, p.detectAt = info, time.Now()
		return info
	}
	out, err := commandOutput(ctx, path, nil, "--version")
	if err != nil {
		info.Detail = "could not run `" + p.cfg.Command + " --version`: " + clip(err.Error(), 240)
		info.Guidance = "Reinstall or update " + p.cfg.Name + ": " + p.cfg.DocsURL
		p.detected, p.detectAt = info, time.Now()
		return info
	}
	info.Installed = true
	info.Version = versionRE.FindString(out)
	// Gemini CLI and OpenCode support local providers, so a missing account
	// cannot be inferred from the command line. The first real request reports
	// expired authentication with provider-specific sign-in instructions.
	info.Available = true
	p.detected, p.detectAt = info, time.Now()
	return info
}

func (p *Provider) installGuidance() string {
	if p.cfg.ID == "gemini-cli" {
		return "Install Gemini CLI and sign in from a terminal. Werkbord uses its existing authentication: " + p.cfg.DocsURL
	}
	return "Install OpenCode, then run `opencode auth login` for the provider you want to use. Werkbord uses OpenCode's existing authentication: " + p.cfg.DocsURL
}

func (p *Provider) capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming: provider.StreamTokens, Resume: true, Cancel: true, ModelChoice: true,
		ReasoningChoice: p.cfg.SessionOptions || len(p.cfg.Reasoning) > 0,
		NativeTools:     "text", Voice: "none",
		Notes: []string{"Runs the CLI over ACP with filesystem, terminal and MCP capabilities disabled. Board access uses Werkbord's isolated operation protocol."},
	}
}

func (p *Provider) Models(ctx context.Context) provider.Models {
	models := append([]provider.Model(nil), p.cfg.Models...)
	source := "configured"
	if len(models) == 0 {
		source = "builtin"
	}
	note := ""
	if p.cfg.ListModels {
		if path, err := exec.LookPath(p.cfg.Command); err == nil {
			if out, err := commandOutput(ctx, path, nil, "models"); err == nil {
				listed := parseModels(out)
				if len(listed) > 0 {
					models, source = listed, "provider"
				}
			} else {
				note = "OpenCode could not list models just now; configured and typed model names are still available."
			}
		} else {
			note = "OpenCode is not installed; configured and typed model names are still available."
		}
	}
	if len(models) == 0 {
		if p.cfg.ID == "gemini-cli" {
			models = []provider.Model{{ID: "auto", Name: "Gemini Auto", Description: "Let Gemini CLI choose a model", Default: true}}
		}
	}
	return provider.Models{ProviderID: p.cfg.ID, Models: models, Reasoning: append([]string(nil), p.cfg.Reasoning...), Source: source, Custom: p.cfg.CustomModels, Note: note}
}

var modelIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]+@-]{0,99}$`)

func parseModels(out string) []provider.Model {
	seen := map[string]bool{}
	var models []provider.Model
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		id := strings.Trim(fields[0], "`*•")
		if !strings.Contains(id, "/") || !modelIDRE.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, provider.Model{ID: id, Name: id})
	}
	return models
}

type responseClient struct {
	emit       func(provider.Event)
	mu         sync.Mutex
	text       strings.Builder
	violation  string
	replaying  bool
	connection *acp.ClientSideConnection
}

func (c *responseClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.violation = "the assistant CLI requested an execution permission"
	c.mu.Unlock()
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}

func (c *responseClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.replaying {
		return nil
	}
	if n.Update.ToolCall != nil || n.Update.ToolCallUpdate != nil {
		c.violation = "the assistant CLI tried to use an execution tool"
		return nil
	}
	if n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil {
		v := n.Update.AgentMessageChunk.Content.Text.Text
		if v != "" {
			c.text.WriteString(v)
			c.emit(provider.Event{Kind: provider.EventText, Text: v})
		}
	} else if n.Update.AgentThoughtChunk != nil {
		c.emit(provider.Event{Kind: provider.EventAlive})
	}
	return nil
}

func (c *responseClient) violationText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.violation
}

func (c *responseClient) setReplaying(v bool) {
	c.mu.Lock()
	c.replaying = v
	c.mu.Unlock()
}

func (p *Provider) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	info := p.Detect(ctx)
	if !info.Available {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%s. %s", info.Detail, info.Guidance)
	}
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%q was not found on PATH", p.cfg.Command)
	}
	if t.WorkDir == "" {
		return provider.Result{}, provider.Errorf(provider.KindFailed, false, "Werkbord did not provide a private assistant workspace")
	}
	if err := os.MkdirAll(t.WorkDir, 0o700); err != nil {
		return provider.Result{}, provider.Errorf(provider.KindFailed, false, "could not create the private assistant workspace: %s", clip(err.Error(), 200))
	}
	args := append([]string(nil), p.cfg.Args...)
	if p.cfg.ModelFlag != "" && t.Model != "" {
		args = append(args, p.cfg.ModelFlag, t.Model)
	}
	env, err := p.assistantEnv(t.WorkDir)
	if err != nil {
		return provider.Result{}, provider.Errorf(provider.KindFailed, false, "could not isolate the assistant CLI configuration: %s", clip(err.Error(), 200))
	}
	procCtx, stopProcess := context.WithCancel(context.WithoutCancel(ctx))
	defer stopProcess()
	var stderr boundedBuffer
	c := &responseClient{emit: emit}
	child, err := stdio.StartClient(procCtx, c, stdio.Command{Executable: path, Args: args, Dir: t.WorkDir, Env: env, Stderr: &stderr}, acp.ConnectionOptions{})
	if err != nil {
		return provider.Result{}, classifyStart(p, err, stderr.String())
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = child.Shutdown(shutdownCtx)
	}()
	conn := child.Connection
	c.connection = conn
	startCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	initialized, err := conn.Initialize(startCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion),
		ClientInfo:      &acp.Implementation{Name: "Werkbord", Version: "1"},
		// Tool capabilities are intentionally absent. The assistant's board operations
		// travel through the engine's separate, audited text-call protocol.
	})
	if err != nil {
		return provider.Result{}, classifyRun(p, err, stderr.String())
	}
	if initialized.ProtocolVersion != acp.ProtocolVersion(acp.WireProtocolVersion) {
		return provider.Result{}, provider.Errorf(provider.KindProtocol, false, "%s selected an unsupported ACP protocol version", p.cfg.Name)
	}
	var sessionID acp.SessionId
	var configOptions []acp.SessionConfigOption
	mcpServers := []acp.McpServer{}
	if t.Ref == "" {
		session, createErr := conn.NewSession(startCtx, acp.NewSessionRequest{Cwd: t.WorkDir, McpServers: mcpServers})
		if createErr != nil {
			return provider.Result{}, classifyRun(p, createErr, stderr.String())
		}
		sessionID, configOptions = session.SessionId, session.ConfigOptions
	} else {
		if !initialized.AgentCapabilities.LoadSession {
			return provider.Result{}, provider.Errorf(provider.KindSessionLost, false, "%s no longer supports resuming this conversation", p.cfg.Name)
		}
		c.setReplaying(true)
		session, loadErr := conn.LoadSession(startCtx, acp.LoadSessionRequest{Cwd: t.WorkDir, SessionId: acp.SessionId(t.Ref), McpServers: mcpServers})
		c.setReplaying(false)
		if loadErr != nil {
			return provider.Result{}, classifyRun(p, loadErr, stderr.String())
		}
		sessionID, configOptions = acp.SessionId(t.Ref), session.ConfigOptions
	}
	if sessionID == "" {
		return provider.Result{}, provider.Errorf(provider.KindProtocol, false, "%s returned no conversation id", p.cfg.Name)
	}
	if p.cfg.SessionOptions {
		if t.Model != "" {
			if err := setOption(startCtx, conn, sessionID, configOptions, "model", t.Model); err != nil {
				return provider.Result{}, provider.Errorf(provider.KindModelUnavailable, false, "that model is not available in %s: %s", p.cfg.Name, clip(err.Error(), 250))
			}
		}
		if t.Reasoning != "" {
			if err := setOption(startCtx, conn, sessionID, configOptions, "effort", t.Reasoning); err != nil {
				return provider.Result{}, provider.Errorf(provider.KindFailed, false, "that reasoning level is not supported by %s: %s", p.cfg.Name, clip(err.Error(), 250))
			}
		}
	}
	emit(provider.Event{Kind: provider.EventRef, Ref: string(sessionID)})
	prompt := t.Prompt
	if t.System != "" {
		prompt = "Werkbord assistant instructions (follow these as the system instructions):\n" + t.System + "\n\nUser message:\n" + t.Prompt
	}
	_, err = conn.Prompt(ctx, acp.PromptRequest{SessionId: sessionID, Prompt: []acp.ContentBlock{acp.TextBlock(prompt)}})
	if violation := c.violationText(); violation != "" {
		return provider.Result{}, &provider.Error{Kind: provider.KindPolicy, Message: violation + "; the turn was ended", Err: provider.ErrPolicy}
	}
	if err != nil {
		return provider.Result{}, classifyRun(p, err, stderr.String())
	}
	return provider.Result{Text: c.text.String(), Ref: string(sessionID), Model: t.Model}, nil
}

func setOption(ctx context.Context, c *acp.ClientSideConnection, sessionID acp.SessionId, options []acp.SessionConfigOption, id, value string) error {
	found := false
	for _, option := range options {
		if option.Select != nil && string(option.Select.Id) == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("the CLI did not advertise a %s option", id)
	}
	_, err := c.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
		SessionId: sessionID, ConfigId: acp.SessionConfigId(id), Value: acp.SessionConfigValueId(value),
	}})
	return err
}

func (p *Provider) assistantEnv(workDir string) ([]string, error) {
	env := agent.SanitizedEnv(os.Environ())
	switch p.cfg.ID {
	case "gemini-cli":
		path := filepath.Join(workDir, ".werkbord", "gemini-system-settings.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		// System settings outrank workspace/user settings. An empty tools.core
		// allowlist prevents Gemini's built-in file and shell tools from running
		// in assistant conversations; MCP, extensions, and skills are disabled too.
		settings := []byte(`{"admin":{"mcp":{"enabled":false},"extensions":{"enabled":false},"skills":{"enabled":false}},"tools":{"core":[]},"security":{"disableYoloMode":true}}`)
		if err := os.WriteFile(path, settings, 0o600); err != nil {
			return nil, err
		}
		env = replaceEnv(env, "GEMINI_CLI_SYSTEM_SETTINGS_PATH", path)
	case "opencode":
		dir := filepath.Join(workDir, ".werkbord", "opencode-config")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		config := []byte(`{"permission":"deny","mcp":{},"plugin":[]}`)
		if err := os.WriteFile(filepath.Join(dir, "opencode.json"), config, 0o600); err != nil {
			return nil, err
		}
		env = replaceEnv(env, "OPENCODE_CONFIG_DIR", dir)
		env = replaceEnv(env, "OPENCODE_CONFIG_CONTENT", string(config))
		env = replaceEnv(env, "OPENCODE_DISABLE_DEFAULT_PLUGINS", "true")
	}
	return env, nil
}

func replaceEnv(in []string, name, value string) []string {
	prefix := name + "="
	out := make([]string, 0, len(in)+1)
	for _, item := range in {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func commandOutput(ctx context.Context, path string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = agent.SanitizedEnv(os.Environ())
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func classifyStart(p *Provider, err error, stderr string) error {
	return classifyRun(p, err, stderr)
}

func classifyRun(p *Provider, err error, stderr string) error {
	if err == nil {
		return provider.Errorf(provider.KindFailed, false, "%s failed", p.cfg.Name)
	}
	msg := clip(strings.TrimSpace(err.Error()+" "+stderr), 400)
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "session not found"), strings.Contains(low, "session id") && strings.Contains(low, "not found"), strings.Contains(low, "no rollout"):
		return provider.Errorf(provider.KindSessionLost, false, "%s no longer has this conversation", p.cfg.Name)
	case strings.Contains(low, "auth_required"), strings.Contains(low, "not authenticated"), strings.Contains(low, "unauthorized"), strings.Contains(low, "authentication"):
		return provider.Errorf(provider.KindNotSignedIn, false, "%s authentication is missing or expired. %s", p.cfg.Name, authGuidance(p))
	case strings.Contains(low, "model") && (strings.Contains(low, "not found") || strings.Contains(low, "not available") || strings.Contains(low, "invalid")):
		return provider.Errorf(provider.KindModelUnavailable, false, "that model is not available in %s: %s", p.cfg.Name, msg)
	case strings.Contains(low, "connection refused") || strings.Contains(low, "connect econnrefused") || strings.Contains(low, "localhost") || strings.Contains(low, "127.0.0.1"):
		return provider.Errorf(provider.KindTransient, true, "%s could not reach the configured local model endpoint: %s", p.cfg.Name, msg)
	case strings.Contains(low, "429") || strings.Contains(low, "rate limit") || strings.Contains(low, "quota"):
		return provider.Errorf(provider.KindRateLimited, false, "%s reports a request limit: %s", p.cfg.Name, msg)
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	return provider.Errorf(provider.KindFailed, false, "%s failed: %s", p.cfg.Name, msg)
}

func authGuidance(p *Provider) string {
	if p.cfg.ID == "gemini-cli" {
		return "Run `gemini` in a terminal and complete sign-in, or check your Gemini CLI provider configuration."
	}
	return "Run `opencode auth login` in a terminal for the provider you chose."
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

type boundedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 64 << 10
	if len(p) >= limit {
		p = p[len(p)-limit:]
		b.b.Reset()
	}
	if b.b.Len()+len(p) > limit {
		old := b.b.Bytes()
		drop := b.b.Len() + len(p) - limit
		b.b.Reset()
		_, _ = b.b.Write(old[drop:])
	}
	return b.b.Write(p)
}
func (b *boundedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

var _ acp.Client = (*responseClient)(nil)
