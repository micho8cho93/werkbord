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
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

// ID is the adapter's stable key, stored on runs.
const ID = "codex"

// Config is the user's choices for the adapter.
type Config struct {
	// Command is the executable; default "codex", found on PATH.
	Command string
	// Model overrides Codex's configured model when set.
	Model string
	// ApprovalPolicy is when Codex asks before acting: "untrusted", "on-request"
	// (the default) or "never". Asking is how a command outside the sandbox, or
	// a change outside the worktree, reaches the user as a question.
	ApprovalPolicy string
	// Sandbox is what commands may touch: "read-only", "workspace-write" (the
	// default: the worktree, no network) or "danger-full-access".
	Sandbox string
}

// Adapter implements agent.Adapter for Codex.
type Adapter struct {
	cfg Config

	mu       sync.Mutex
	detected domain.Agent
	detectAt time.Time
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
	return &Adapter{cfg: cfg}
}

// ID implements agent.Adapter.
func (a *Adapter) ID() string { return ID }

const detectTTL = 30 * time.Second

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Detect implements agent.Adapter.
func (a *Adapter) Detect(ctx context.Context) domain.Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.detectAt.IsZero() && time.Since(a.detectAt) < detectTTL {
		return a.detected
	}
	a.detected, a.detectAt = a.detect(ctx), time.Now()
	return a.detected
}

func (a *Adapter) detect(ctx context.Context) domain.Agent {
	info := domain.Agent{ID: ID, Name: "Codex"}
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		info.Detail = fmt.Sprintf("%q was not found on PATH", a.cfg.Command)
		return info
	}
	out, err := run(ctx, path, "--version")
	if err != nil {
		info.Detail = "could not run `" + a.cfg.Command + " --version`: " + err.Error()
		return info
	}
	info.Version = versionRE.FindString(out)

	// `login status` exits non-zero when nobody is signed in. Any other failure
	// (an older version without the command) is not evidence of anything.
	if out, err := run(ctx, path, "login", "status"); err != nil && strings.Contains(strings.ToLower(out), "not logged in") {
		info.Detail = "not signed in: run `" + a.cfg.Command + " login`"
		return info
	}
	info.Available = true
	return info
}

func run(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = agent.SanitizedEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// startTimeout bounds the handshake with a new app-server.
var startTimeout = 60 * time.Second

// Start implements agent.Adapter.
func (a *Adapter) Start(ctx context.Context, req agent.StartRequest) (agent.Session, error) {
	s := newSession(agent.ProcSpec{
		Command: a.cfg.Command, Args: []string{"app-server"}, Dir: req.WorkDir, Env: agent.SanitizedEnv(os.Environ()),
	}, req.WorkDir, a.cfg)
	if err := s.Launch(); err != nil {
		return nil, err
	}
	fail := func(err error) (agent.Session, error) {
		if tail := s.StderrTail(); tail != "" {
			err = fmt.Errorf("%w (codex said: %s)", err, lastLine(tail))
		}
		_ = s.Stop(context.Background())
		return nil, err
	}

	hctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := s.handshake(hctx, req.ResumeRef); err != nil {
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
