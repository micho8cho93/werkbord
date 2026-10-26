package agentkit

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// TTL is how long an answer is remembered. Asking costs a process or two, and a person who has just signed in or
// installed something waits at most this long to be believed.
const TTL = 30 * time.Second

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Version finds the version number in a command's `--version` output.
func Version(out string) string { return versionRE.FindString(out) }

// Problem says why an agent cannot be used, when it cannot.
type Problem string

const (
	// NoProblem: the agent is installed and not known to be signed out.
	NoProblem Problem = ""
	// NotFound: the command is not on this computer.
	NotFound Problem = "not_found"
	// CannotRun: the command is there but did not answer `--version`.
	CannotRun Problem = "cannot_run"
	// SignedOut: the agent says nobody is signed in.
	SignedOut Problem = "signed_out"
)

// Facts is what is known about one agent's command line on this computer. Detail and Guidance are written here once,
// for both families, and say what is wrong and how to fix it when Problem is not NoProblem.
type Facts struct {
	ID      string
	Name    string
	DocsURL string
	// Command is the command that was asked about, as configured (or the default).
	Command   string
	Installed bool
	Version   string
	// SignedIn is true, false, or nil when the agent cannot say (an older version without a status command).
	SignedIn *bool
	// Auth says how the person is signed in, as the agent reports it ("claude.ai", "ChatGPT"). It is never a secret.
	Auth string
	// UsesAPIKey is true when the agent will bill an API key found in the person's environment instead of their plan.
	UsesAPIKey bool
	Problem    Problem
	Detail     string
	Guidance   string
}

// Available reports whether the agent can be started: installed and not known to be signed out.
func (f Facts) Available() bool { return f.Installed && f.Problem == NoProblem }

// base is the part of Facts that does not depend on what the command said.
type base struct {
	id, name, docs, defaultCommand string
	// install is the guidance for a command that is not there, reinstall for one that does not run.
	install, reinstall string
}

// Prober asks agents about themselves and remembers the answers for TTL. One Prober is shared by the adapters and the
// providers, so the CLI is asked once, not once per family. The zero Prober is not usable; use NewProber.
type Prober struct {
	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	mu    sync.Mutex
	facts Facts
	help  string
	at    time.Time
	helpA time.Time
}

// NewProber returns an empty Prober.
func NewProber() *Prober { return &Prober{entries: map[string]*entry{}} }

func (p *Prober) entry(kind, command string) *entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := kind + "\x00" + command
	e := p.entries[key]
	if e == nil {
		e = &entry{}
		p.entries[key] = e
	}
	return e
}

// Claude asks Claude Code's command line (default "claude") about itself.
func (p *Prober) Claude(ctx context.Context, command string) Facts {
	if command == "" {
		command = "claude"
	}
	e := p.entry("claude", command)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.at.IsZero() && time.Since(e.at) < TTL {
		return e.facts
	}
	e.facts, e.at = probeClaude(ctx, command), time.Now()
	return e.facts
}

// Codex asks Codex's command line (default "codex") about itself.
func (p *Prober) Codex(ctx context.Context, command string) Facts {
	if command == "" {
		command = "codex"
	}
	e := p.entry("codex", command)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.at.IsZero() && time.Since(e.at) < TTL {
		return e.facts
	}
	e.facts, e.at = probeCodex(ctx, command), time.Now()
	return e.facts
}

// Help returns the command's own `--help` text, which says which options it has. It is "" when the command cannot be
// found or run. Claude Code only: the other agents are asked for their options another way.
func (p *Prober) Help(ctx context.Context, command string) string {
	if command == "" {
		command = "claude"
	}
	e := p.entry("help", command)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.helpA.IsZero() && time.Since(e.helpA) < TTL {
		return e.help
	}
	e.help, e.helpA = "", time.Now()
	if path, err := exec.LookPath(command); err == nil {
		e.help, _ = Run(ctx, path, "--help")
	}
	return e.help
}

// locate finds the command and its version, or says why it cannot.
func locate(ctx context.Context, b base, command string, combined bool) (Facts, string, bool) {
	f := Facts{ID: b.id, Name: b.name, DocsURL: b.docs, Command: command}
	path, err := exec.LookPath(command)
	if err != nil {
		f.Problem = NotFound
		f.Detail = fmt.Sprintf("%q was not found on PATH", command)
		f.Guidance = b.install
		return f, "", false
	}
	run := Run
	if combined {
		run = RunCombined
	}
	out, err := run(ctx, path, "--version")
	if err != nil {
		f.Problem = CannotRun
		f.Detail = "could not run `" + command + " --version`: " + err.Error()
		f.Guidance = b.reinstall
		return f, "", false
	}
	f.Installed = true
	f.Version = Version(out)
	return f, path, true
}

func signedOut(f *Facts, command, login, who string) {
	f.Problem = SignedOut
	f.Detail = "not signed in: run `" + filepath.Base(command) + " " + login + "`"
	f.Guidance = "Run `" + filepath.Base(command) + " " + login + "` in a terminal and follow the prompts. Werkbord uses your own " + who + " and never asks for a key."
}

// SignedOutIfUnknown is for a caller that cannot go on without a sign-in it can see: an agent whose CLI has no status
// command (an older version) is then reported as not signed in, with the way to sign in, instead of assumed usable.
func (f Facts) SignedOutIfUnknown() Facts {
	if !f.Installed || f.SignedIn != nil {
		return f
	}
	no := false
	f.SignedIn = &no
	switch f.ID {
	case codex.id:
		signedOut(&f, f.Command, "login", "ChatGPT account")
	default:
		signedOut(&f, f.Command, "auth login", "Claude account")
	}
	return f
}
