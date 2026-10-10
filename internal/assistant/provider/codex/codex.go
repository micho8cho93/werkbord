// Package codex is the assistant's provider for Codex, signed in the way the person signed it in (`codex login`, a
// ChatGPT plan).
//
// A turn is one `codex app-server` process, Codex's JSON-RPC protocol over stdio (what its own integrations use),
// because it is the only way Codex streams a reply token by token (`codex exec --json` delivers a message whole), and
// the only one that can stop a turn in progress. The process is started with the features that give the model tools
// turned off, in a read-only sandbox, never asking for approval, in an empty directory. Codex still lists a few tools
// that no setting removes; so every item the turn produces is checked, and a turn that produces anything but a message
// is interrupted and ended (an allow-list, not a list of what is known to be dangerous). The app-server protocol is
// marked experimental by Codex; this is written against 0.155.1, and what it does not recognise is a protocol error
// rather than a guess. See docs/ASSISTANT.md.
package codex

import (
	"context"
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
)

// ID is the provider's key, the same one the coding-agent adapter uses.
const ID = "codex"

// Config is the person's choices for the provider.
type Config struct {
	// Command is the executable; default "codex", found on PATH.
	Command string
	// Model is what a conversation that chose no model gets: the person's default for Codex on this computer.
	Model string
	// Models replaces what Codex reports about itself.
	Models []provider.Model
}

// Provider implements provider.Provider.
type Provider struct {
	cfg Config

	mu       sync.Mutex
	detected provider.Info
	detectAt time.Time
	features []string // the features to turn off that this Codex has
	featAt   time.Time

	modelMu  sync.Mutex
	models   provider.Models
	modelsAt time.Time
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider with defaults filled in.
func New(cfg Config) *Provider {
	if cfg.Command == "" {
		cfg.Command = "codex"
	}
	return &Provider{cfg: cfg}
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return ID }

const (
	detectTTL = 30 * time.Second
	modelsTTL = 5 * time.Minute
	docsURL   = "https://developers.openai.com/codex/cli"
)

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// toolFeatures are the Codex features that give the model something to act with. Only the ones this Codex has are
// passed: it exits on a feature it does not know.
var toolFeatures = []string{
	"shell_tool", "apps", "plugins", "hooks", "memories", "goals", "code_mode_host", "browser_use", "browser_use_external",
	"computer_use", "multi_agent", "multi_agent_v2", "image_generation", "skill_search", "tool_suggest", "sleep_tool", "in_app_browser",
}

// Detect implements provider.Provider.
func (p *Provider) Detect(ctx context.Context) provider.Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.detectAt.IsZero() && time.Since(p.detectAt) < detectTTL {
		return p.detected
	}
	p.detected, p.detectAt = p.detect(ctx), time.Now()
	return p.detected
}

func capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming: provider.StreamTokens, Resume: true, Cancel: true, ModelChoice: true, ReasoningChoice: true,
		NativeTools: "mcp", Voice: "experimental",
		Notes: []string{
			"Codex is run without its tools, in a read-only sandbox that never asks for approval. Its conversation is kept by Codex itself, in its own storage; Werkbord keeps only the handle.",
			"Codex's app-server protocol is marked experimental by Codex, so a Codex update can change it.",
			"Codex has an experimental realtime (audio) path in the same protocol. The assistant does not use it.",
		},
	}
}

func (p *Provider) detect(ctx context.Context) provider.Info {
	info := provider.Info{ID: ID, Name: "Codex", Capabilities: capabilities()}
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		info.Detail = fmt.Sprintf("%q was not found on PATH", p.cfg.Command)
		info.Guidance = "Install Codex (" + docsURL + "), then run `codex login`."
		return info
	}
	out, err := output(ctx, path, "--version")
	if err != nil {
		info.Detail = "could not run `" + p.cfg.Command + " --version`: " + err.Error()
		info.Guidance = "Reinstall Codex: " + docsURL
		return info
	}
	info.Installed, info.Version = true, versionRE.FindString(out)
	if out, err := output(ctx, path, "app-server", "--help"); err != nil || !strings.Contains(out, "app-server") {
		info.Detail = "this version of Codex has no app-server, which the assistant needs"
		info.Guidance = "Update Codex."
		return info
	}
	// `codex login status` says "Logged in using ChatGPT" and exits 0 when signed in.
	status, serr := output(ctx, path, "login", "status")
	signed := serr == nil && strings.Contains(strings.ToLower(status), "logged in")
	info.SignedIn = &signed
	if signed {
		if _, after, ok := strings.Cut(status, "using "); ok {
			info.Auth = strings.TrimSpace(after)
			info.UsesAPIKey = strings.Contains(strings.ToLower(info.Auth), "api key")
		}
	} else {
		info.Detail = "not signed in"
		info.Guidance = "Run `" + filepath.Base(p.cfg.Command) + " login` in a terminal and follow the prompts. Werkbord uses your own ChatGPT account and never asks for a key."
		return info
	}
	info.Available = true
	return info
}

// disabledFeatures lists the tool features this Codex has, from `codex features list`.
func (p *Provider) disabledFeatures(ctx context.Context, path string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.features != nil && time.Since(p.featAt) < detectTTL {
		return p.features
	}
	out, _ := output(ctx, path, "features", "list")
	have := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[1] != "removed" {
			have[f[0]] = true
		}
	}
	p.features = []string{}
	for _, f := range toolFeatures {
		if have[f] {
			p.features = append(p.features, f)
		}
	}
	p.featAt = time.Now()
	return p.features
}

func output(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = agent.SanitizedEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// serverArgs is the command line of every app-server the assistant starts.
func (p *Provider) serverArgs(ctx context.Context, path string) []string {
	args := []string{"app-server"}
	for _, f := range p.disabledFeatures(ctx, path) {
		args = append(args, "--disable", f)
	}
	return args
}

// Models implements provider.Provider. Codex lists its own models and the reasoning levels each takes.
func (p *Provider) Models(ctx context.Context) provider.Models {
	if len(p.cfg.Models) > 0 {
		return provider.Models{ProviderID: ID, Models: p.cfg.Models, Source: "configured", Custom: true}
	}
	p.modelMu.Lock()
	defer p.modelMu.Unlock()
	if p.models.Source != "" && time.Since(p.modelsAt) < modelsTTL {
		return p.models
	}
	m := provider.Models{ProviderID: ID, Custom: true, Source: "provider"}
	list, err := p.listModels(ctx)
	if err != nil || len(list) == 0 {
		m.Source = "builtin"
		m.Note = "Codex could not list its models just now. You can still type a model name, or leave it on Codex's own default."
		return m
	}
	m.Models = list
	p.models, p.modelsAt = m, time.Now()
	return m
}

// effortNames returns the reasoning levels Codex reports for a model.
type modelEntry struct {
	ID          string `json:"id"`
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
	Efforts     []struct {
		Effort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}

func (e modelEntry) model() provider.Model {
	id := e.Model
	if id == "" {
		id = e.ID
	}
	m := provider.Model{ID: id, Name: e.DisplayName, Description: e.Description, Default: e.IsDefault}
	if m.Name == "" {
		m.Name = id
	}
	for _, x := range e.Efforts {
		m.Reasoning = append(m.Reasoning, x.Effort)
	}
	return m
}

func (p *Provider) listModels(ctx context.Context) ([]provider.Model, error) {
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := dial(ctx, path, p.serverArgs(ctx, path), "")
	if err != nil {
		return nil, err
	}
	defer c.close()
	var resp struct {
		Data []modelEntry `json:"data"`
	}
	if err := c.call(ctx, "model/list", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	var out []provider.Model
	for _, e := range resp.Data {
		if !e.Hidden {
			out = append(out, e.model())
		}
	}
	return out, nil
}
