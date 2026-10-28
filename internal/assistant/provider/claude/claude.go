// Package claude is the assistant's provider for Claude Code, signed in the way the person signed it in (a Claude
// subscription through `claude auth login`).
//
// A turn is one `claude -p` process with every tool of its own turned off (--tools "", no MCP servers, no skills, no
// hooks, none of the person's settings), in an empty directory, with the assistant's instructions as the system
// prompt. The reply is read as stream-json with partial messages, so it arrives a few words at a time. The
// conversation is continued with --resume. A turn in which Claude Code announces a tool, or lists any, is ended.
// Verified against Claude Code 2.1.285; see docs/ASSISTANT.md for what was checked.
package claude

import (
	"context"
	"crypto/rand"
	"fmt"
	"os/exec"
	"strings"

	"werkbord/internal/agentkit"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/assistant/provider/cli"
)

// ID is the provider's key, the same one the coding-agent adapter uses.
const ID = "claude-code"

// Config is the person's choices for the provider.
type Config struct {
	// Command is the executable; default "claude", found on PATH.
	Command string
	// Model is what a conversation that chose no model gets: the person's default for Claude Code on this computer.
	Model string
	// Models replaces the built-in aliases; Reasoning the levels the CLI reports.
	Models    []provider.Model
	Reasoning []string
	// Prober answers what the CLI says about itself. The controller shares one with the coding-agent adapter, so Claude
	// Code is asked once for both; nil gives the provider one of its own.
	Prober *agentkit.Prober
}

// Provider implements provider.Provider.
type Provider struct {
	cfg   Config
	probe *agentkit.Prober
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider with defaults filled in.
func New(cfg Config) *Provider {
	if cfg.Command == "" {
		cfg.Command = "claude"
	}
	probe := cfg.Prober
	if probe == nil {
		probe = agentkit.NewProber()
	}
	return &Provider{cfg: cfg, probe: probe}
}

// ID implements provider.Provider.
func (p *Provider) ID() string { return ID }

// Options the assistant cannot do without. An older Claude Code that lacks one is reported as unusable, with the
// reason, rather than run with fewer protections.
var required = []string{"--tools", "--system-prompt", "--include-partial-messages", "--session-id", "--resume"}

// Detect implements provider.Provider. agentkit finds the command, its version and whether it is signed in (and shares
// the answer with the coding-agent adapter); the provider adds the options it cannot run without.
func (p *Provider) Detect(ctx context.Context) provider.Info {
	facts := p.probe.Claude(ctx, p.cfg.Command)
	info := provider.InfoFromFacts(facts)
	if !facts.Installed {
		info.Capabilities = p.capabilities("")
		return info
	}
	help := p.probe.Help(ctx, p.cfg.Command)
	info.Capabilities = p.capabilities(help)
	for _, flag := range required {
		if !strings.Contains(help, flag) {
			info.Available, info.SignedIn, info.Auth, info.UsesAPIKey = false, nil, "", false
			info.Detail = "this version of Claude Code has no " + flag + ", which the assistant needs to run it without tools"
			info.Guidance = "Update Claude Code (`claude update`)."
			return info
		}
	}
	return info
}

func (p *Provider) capabilities(help string) provider.Capabilities {
	c := provider.Capabilities{
		Isolated:  strings.Contains(help, "--safe-mode"),
		Streaming: provider.StreamTokens, Resume: true, Cancel: true, ModelChoice: true,
		ReasoningChoice: strings.Contains(help, "--effort"), NativeTools: "mcp", Voice: "none",
		Notes: []string{
			"Claude Code is run without any of its tools. Its conversation is kept by Claude Code itself, in its own storage; Werkbord keeps only the handle.",
			"Claude Code has no voice input or output when run this way.",
		},
	}
	return c
}

// Models implements provider.Provider. Claude Code cannot list its models, so the list is the person's own or its
// aliases (which always mean the latest of a family and so do not go stale); a full model name can still be typed in.
func (p *Provider) Models(ctx context.Context) provider.Models {
	m := provider.Models{ProviderID: ID, Custom: true, Source: "builtin"}
	for _, a := range agentkit.ClaudeModels() {
		m.Models = append(m.Models, provider.Model{ID: a.ID, Name: a.Name, Description: a.Description})
	}
	if len(p.cfg.Models) > 0 {
		m.Models, m.Source = p.cfg.Models, "configured"
	}
	m.Reasoning = agentkit.EffortLevels(p.probe.Help(ctx, p.cfg.Command))
	if len(p.cfg.Reasoning) > 0 {
		m.Reasoning = p.cfg.Reasoning
	}
	if len(m.Reasoning) == 0 {
		m.Note = "This version of Claude Code does not list reasoning levels."
	}
	return m
}

// args builds the command line for a turn. Options that only some versions have are used when the CLI says it has
// them; the ones in `required` are checked by Detect.
func (p *Provider) args(t provider.Turn, ref string, fresh bool, help string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--tools", "", "--system-prompt", t.System}
	for _, opt := range []struct {
		flag string
		args []string
	}{
		{"--safe-mode", nil},                          // no CLAUDE.md, skills, plugins, hooks or MCP servers
		{"--strict-mcp-config", nil},                  // and no MCP servers from anywhere else
		{"--disable-slash-commands", nil},             // no skills
		{"--setting-sources", []string{""}},           // none of the person's settings files
		{"--system-prompt-snapshot", []string{"off"}}, // the instructions are re-sent as they are now, not as recorded on turn one
	} {
		if strings.Contains(help, opt.flag) {
			args = append(args, opt.flag)
			args = append(args, opt.args...)
		}
	}
	if model := firstNonEmpty(t.Model, p.cfg.Model); model != "" {
		args = append(args, "--model", model)
	}
	if t.Reasoning != "" {
		args = append(args, "--effort", t.Reasoning)
	}
	if fresh {
		return append(args, "--session-id", ref)
	}
	return append(args, "--resume", ref)
}

// Run implements provider.Provider.
func (p *Provider) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	info := p.Detect(ctx)
	if !info.Installed {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%s. %s", info.Detail, info.Guidance)
	}
	if !info.Available && info.SignedIn != nil && !*info.SignedIn {
		return provider.Result{}, provider.Errorf(provider.KindNotSignedIn, false, "Claude Code is not signed in. %s", info.Guidance)
	}
	if !info.Available {
		return provider.Result{}, provider.Errorf(provider.KindFailed, false, "Claude Code cannot be used: %s. %s", info.Detail, info.Guidance)
	}
	path, err := exec.LookPath(p.cfg.Command)
	if err != nil {
		return provider.Result{}, provider.Errorf(provider.KindNotInstalled, false, "%q was not found on PATH", p.cfg.Command)
	}
	help := p.probe.Help(ctx, p.cfg.Command)

	ref, fresh := t.Ref, t.Ref == ""
	if fresh {
		// Choosing the id ourselves means it is known, and can be stored, before Claude Code has said a word.
		ref = newUUID()
	}
	emit(provider.Event{Kind: provider.EventRef, Ref: ref})

	st := &turnState{emit: emit, ref: ref}
	exit, err := cli.Run(ctx, cli.Spec{Path: path, Args: p.args(t, ref, fresh, help), Dir: t.WorkDir, Stdin: t.Prompt}, st.line)
	if err != nil {
		return provider.Result{}, err // the context ended it, or the pipe broke
	}
	return st.finish(exit)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("claude: crypto/rand failed: " + err.Error())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
