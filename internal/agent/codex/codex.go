// Package codex adapts the Codex CLI to agent.Adapter.
//
// It runs `codex app-server`, Codex's JSON-RPC protocol over stdio, which is
// what Codex's own IDE integrations use. One process holds one conversation
// (a "thread"): the controller starts it, sends each user message as a turn,
// and receives structured notifications as the turn progresses. Approvals and
// the agent's own questions arrive as requests from the server and become
// Questions the user answers from wherever they are.
package codex

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"werkbord/internal/agent"
	"werkbord/internal/agentkit"
	"werkbord/internal/domain"
)

// ID is the adapter's stable key, stored on runs.
const ID = "codex"

// Config is the user's choices for the adapter.
type Config struct {
	// Command is the executable; default "codex", found on PATH.
	Command string
	// Model overrides Codex's configured model when set. It is what "Agent
	// default" means for this agent on this computer: a run that chooses no model
	// of its own gets it.
	Model string
	// Models and Reasoning are the choices the user listed in config.json. They
	// replace what Codex reports about itself.
	Models    []domain.AgentOption
	Reasoning []string
	// ApprovalPolicy is when Codex asks before acting: "untrusted", "on-request"
	// (the default) or "never". Asking is how a command outside the sandbox, or
	// a change outside the worktree, reaches the user as a question.
	ApprovalPolicy string
	// Sandbox is what commands may touch: "read-only", "workspace-write" (the
	// default: the worktree, no network) or "danger-full-access".
	Sandbox string
	// Prober answers what the CLI says about itself. The controller shares one with the assistant's provider, so Codex
	// is asked once for both; nil gives the adapter one of its own.
	Prober *agentkit.Prober
}

// Adapter implements agent.Adapter for Codex.
type Adapter struct {
	cfg   Config
	probe *agentkit.Prober

	optMu   sync.Mutex
	optCach domain.AgentOptions
	optAt   time.Time
}

var _ agent.Adapter = (*Adapter)(nil)

// New returns an adapter with defaults filled in.
func New(cfg Config) *Adapter {
	if cfg.Command == "" {
		cfg.Command = "codex"
	}
	if cfg.ApprovalPolicy == "" {
		cfg.ApprovalPolicy = "on-request"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "workspace-write"
	}
	probe := cfg.Prober
	if probe == nil {
		probe = agentkit.NewProber()
	}
	return &Adapter{cfg: cfg, probe: probe}
}

// ID implements agent.Adapter.
func (a *Adapter) ID() string { return ID }

// Detect implements agent.Adapter. agentkit finds the executable, its version and whether it is signed in, and
// remembers the answer briefly (and shares it with the assistant's provider).
func (a *Adapter) Detect(ctx context.Context) domain.Agent {
	return agent.FromFacts(a.probe.Codex(ctx, a.cfg.Command))
}

// failDrainWait is how long a failed start waits for a dead server's stderr.
const failDrainWait = time.Second

// startTimeout bounds the handshake with a new app-server.
var startTimeout = 60 * time.Second

// Start implements agent.Adapter.
func (a *Adapter) Start(ctx context.Context, req agent.StartRequest) (agent.Session, error) {
	s := newSession(agent.ProcSpec{
		Command: a.cfg.Command, Args: []string{"app-server"}, Dir: req.WorkDir, Env: agent.SanitizedEnv(os.Environ()),
	}, req.WorkDir, a.cfg, req.Model, req.Reasoning)
	s.resumedUsage = req.ResumeRef != ""
	if err := s.Launch(); err != nil {
		return nil, err
	}
	fail := func(err error) (agent.Session, error) {
		// A server that died is reaped, and its last words read, a moment after the write
		// to it failed; give that a moment so the error says why instead of just "ended".
		select {
		case <-s.Done():
		case <-time.After(failDrainWait):
		}
		if tail := s.StderrTail(); tail != "" {
			err = fmt.Errorf("%w (codex said: %s)", err, lastLine(tail))
		}
		_ = s.Stop(context.Background())
		return nil, err
	}

	hctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := s.handshake(hctx, req.ResumeRef, agent.Instructions(req.Policy)); err != nil {
		return fail(err)
	}
	s.Emit(agent.Event{Kind: agent.KindSessionRef, SessionRef: s.thread()})
	if err := s.Send(hctx, req.Prompt); err != nil {
		return fail(fmt.Errorf("send the first message: %w", err))
	}
	return s, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func (a *Adapter) ExecutionPolicy() map[string]string {
	return map[string]string{"sandbox": a.cfg.Sandbox, "approvalPolicy": a.cfg.ApprovalPolicy, "defaultModel": a.cfg.Model}
}
