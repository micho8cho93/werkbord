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
	"encoding/json"
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
const ID = "claude-code"

// Config is the user's choices for the adapter.
type Config struct {
	// Command is the executable; default "claude", found on PATH.
	Command string
	// Model is passed as --model when set (an alias like "sonnet" or a full name).
	Model string
	// PermissionMode is passed as --permission-mode; default "acceptEdits",
	// which lets the agent edit files in its worktree without asking while
	// commands and everything else still go to the user as questions.
	PermissionMode string
}

// Adapter implements agent.Adapter for Claude Code.
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
		cfg.Command = "claude"
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = "acceptEdits"
	}
	return &Adapter{cfg: cfg}
}

// ID implements agent.Adapter.
func (a *Adapter) ID() string { return ID }

const detectTTL = 30 * time.Second

// settle is how long Start watches a new process for dying at once.
var settle = 300 * time.Millisecond

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Detect implements agent.Adapter. It looks for the executable, asks it for its
// version and whether it is signed in, and remembers the answer briefly.
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
	info := domain.Agent{ID: ID, Name: "Claude Code"}
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

	// Signed in? Older versions have no `auth status`; then assume yes and let a
	// real failure show up when a session starts.
	if out, err := run(ctx, path, "auth", "status"); err == nil {
		var st struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal([]byte(out), &st) == nil && st.LoggedIn != nil && !*st.LoggedIn {
			info.Detail = "not signed in: run `" + a.cfg.Command + " auth login`"
			return info
		}
	}
	info.Available = true
	return info
}

func run(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = agent.SanitizedEnv(os.Environ())
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
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
	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
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
