// Package claude adapts Claude Code to agent.Adapter.
//
// It runs `claude -p --input-format stream-json --output-format stream-json`,
// which keeps one conversation open over stdio: the controller writes user
// messages as JSON lines and reads the agent's messages back. The same channel
// carries permission requests (`--permission-prompt-tool stdio`), so a tool
// approval or an AskUserQuestion becomes a Question the user answers from
// wherever they are, instead of being denied because nobody is at a terminal.
package claude

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"werkbord/internal/agent"
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

// ID is the adapter's stable key, stored on runs.
const ID = "claude-code"

// Config is the user's choices for the adapter.
type Config struct {
	// Command is the executable; default "claude", found on PATH.
	Command string
	// Model is passed as --model when set (an alias like "sonnet" or a full name).
	// It is what "Agent default" means for this agent on this computer: a run that
	// chooses no model of its own gets it.
	Model string
	// Models and Reasoning are the choices the user listed in config.json. They
	// replace the built-in aliases and the levels the CLI reports about itself.
	Models    []domain.AgentOption
	Reasoning []string
	// PermissionMode is passed as --permission-mode; default "acceptEdits",
	// which lets the agent edit files in its worktree without asking while
	// commands and everything else still go to the user as questions.
	PermissionMode string
	// Prober answers what the CLI says about itself. The controller shares one with the assistant's provider, so Claude
	// Code is asked once for both; nil gives the adapter one of its own.
	Prober *agentkit.Prober
}

// Adapter implements agent.Adapter for Claude Code.
type Adapter struct {
	cfg   Config
	probe *agentkit.Prober
}

var _ agent.Adapter = (*Adapter)(nil)

// New returns an adapter with defaults filled in.
func New(cfg Config) *Adapter {
	if cfg.Command == "" {
		cfg.Command = "claude"
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = "acceptEdits"
	}
	probe := cfg.Prober
	if probe == nil {
		probe = agentkit.NewProber()
	}
	return &Adapter{cfg: cfg, probe: probe}
}

// ID implements agent.Adapter.
func (a *Adapter) ID() string { return ID }

// settle is how long Start watches a new process for dying at once.
var settle = 300 * time.Millisecond

// Detect implements agent.Adapter. It looks for the executable, asks it for its version and whether it is signed in
// (agentkit does the asking and remembers the answer briefly), and reports what it found.
func (a *Adapter) Detect(ctx context.Context) domain.Agent {
	return agent.FromFacts(a.probe.Claude(ctx, a.cfg.Command))
}

// effortLevels is the reasoning levels the installed CLI says --effort takes: the CLI knows, and its list changes. nil
// means it has no such flag (an older version).
func (a *Adapter) effortLevels(ctx context.Context) []string {
	return agentkit.EffortLevels(a.probe.Help(ctx, a.cfg.Command))
}

// Start implements agent.Adapter.
func (a *Adapter) Start(ctx context.Context, req agent.StartRequest) (agent.Session, error) {
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--permission-prompt-tool", "stdio",
	}
	if a.cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", a.cfg.PermissionMode)
	}
	// A model chosen for the run wins; otherwise the one configured as this agent's
	// default; otherwise Claude Code's own.
	model := req.Model
	if model == "" {
		model = a.cfg.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if req.Reasoning != "" {
		if !a.supportsEffort(ctx) {
			return nil, fmt.Errorf("this version of Claude Code has no --effort option, so the reasoning level %q cannot be set: update Claude Code, or choose \"Agent default\"", req.Reasoning)
		}
		args = append(args, "--effort", req.Reasoning)
	}
	// Standing instructions go in the system prompt, where they stay in force
	// across turns (and resumes), not into a message that scrolls away.
	if text := agent.Instructions(req.Policy); text != "" {
		args = append(args, "--append-system-prompt", text)
	}
	ref := req.ResumeRef
	if ref != "" {
		args = append(args, "--resume", ref)
	} else {
		// Choosing the session ID ourselves means it is known, and can be stored,
		// before the agent has said a word.
		ref = newUUID()
		args = append(args, "--session-id", ref)
	}

	s := newSession(agent.ProcSpec{
		Command: a.cfg.Command, Args: args, Dir: req.WorkDir, Env: agent.SanitizedEnv(os.Environ()),
	}, req.WorkDir)
	s.resumedUsage = req.ResumeRef != ""
	if err := s.Launch(); err != nil {
		return nil, err
	}
	s.Emit(agent.Event{Kind: agent.KindSessionRef, SessionRef: ref})

	if err := s.Send(ctx, req.Prompt); err != nil {
		if res, ended := s.ExitedWithin(time.Second); ended {
			return nil, fmt.Errorf("claude exited immediately: %s", res.Reason)
		}
		_ = s.Stop(context.Background())
		return nil, fmt.Errorf("send the first message: %w", err)
	}
	if res, ended := s.ExitedWithin(settle); ended && res.State == domain.RunFailed {
		return nil, fmt.Errorf("claude exited immediately: %s", res.Reason)
	}
	return s, nil
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

func (a *Adapter) ExecutionPolicy() map[string]string {
	return map[string]string{"permissionMode": a.cfg.PermissionMode, "defaultModel": a.cfg.Model}
}
