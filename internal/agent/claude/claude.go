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
}

// Adapter implements agent.Adapter for Claude Code.
type Adapter struct {
	cfg Config

	mu       sync.Mutex
	detected domain.Agent
	detectAt time.Time

	effortMu sync.Mutex
	effort   []string // reasoning levels the installed CLI says --effort takes; nil if it has no such flag
	effortAt time.Time
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

// docsURL is where Claude Code's own installation instructions live.
const docsURL = "https://docs.anthropic.com/en/docs/claude-code/setup"

func (a *Adapter) detect(ctx context.Context) domain.Agent {
	info := domain.Agent{ID: ID, Name: "Claude Code", DocsURL: docsURL}
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		info.Detail = fmt.Sprintf("%q was not found on PATH", a.cfg.Command)
		info.Guidance = "Install Claude Code (https://claude.ai/install.sh, or `npm install -g @anthropic-ai/claude-code`), then run `claude` once to sign in."
		return info
	}
	out, err := run(ctx, path, "--version")
	if err != nil {
		info.Detail = "could not run `" + a.cfg.Command + " --version`: " + err.Error()
		info.Guidance = "Reinstall Claude Code: " + docsURL
		return info
	}
	info.Installed = true
	info.Version = versionRE.FindString(out)

	// Signed in? Older versions have no `auth status`; then assume yes and let a
	// real failure show up when a session starts.
	info.SignIn = domain.SignInUnknown
	if out, err := run(ctx, path, "auth", "status"); err == nil {
		var st struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal([]byte(out), &st) == nil && st.LoggedIn != nil {
			if *st.LoggedIn {
				info.SignIn = domain.SignedIn
			} else {
				info.SignIn = domain.SignedOut
				info.Detail = "not signed in: run `" + a.cfg.Command + " auth login`"
				info.Guidance = "Run `" + a.cfg.Command + " auth login` in a terminal and follow the prompts. Werkbord uses your own Claude account and never asks for a key."
				return info
			}
		}
	}
	info.Available = true
	return info
}

// effortRE finds the reasoning levels in the CLI's own help for --effort, e.g.
// "--effort <level>  Effort level for the current session (low, medium, high)".
var effortRE = regexp.MustCompile(`(?s)--effort\s+<[^>]+>.*?\(([a-z, ]+)\)`)

// effortLevels asks the installed CLI which levels --effort takes: the CLI
// knows, and its list changes. nil means it has no such flag (an older version).
// It costs a process, so it is only asked when someone wants the options, and
// remembered for as long as a detection is.
func (a *Adapter) effortLevels(ctx context.Context) []string {
	a.effortMu.Lock()
	defer a.effortMu.Unlock()
	if !a.effortAt.IsZero() && time.Since(a.effortAt) < detectTTL {
		return a.effort
	}
	a.effort, a.effortAt = a.askEffortLevels(ctx), time.Now()
	return a.effort
}

func (a *Adapter) askEffortLevels(ctx context.Context) []string {
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		return nil
	}
	out, err := run(ctx, path, "--help")
	if err != nil && out == "" {
		return nil
	}
	m := effortRE.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	var levels []string
	for _, l := range strings.Split(m[1], ",") {
		if l = strings.TrimSpace(l); l != "" {
			levels = append(levels, l)
		}
	}
	return levels
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
