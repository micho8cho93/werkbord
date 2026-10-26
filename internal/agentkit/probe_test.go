//go:build unix

package agentkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var bg = context.Background()

// fake writes an executable shell script and returns its path.
func fake(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeIsNotFoundOrCannotRun(t *testing.T) {
	f := NewProber().Claude(bg, filepath.Join(t.TempDir(), "nothing-here"))
	if f.Installed || f.Available() || f.Problem != NotFound || !strings.Contains(f.Detail, "was not found on PATH") || !strings.Contains(f.Guidance, "Install Claude Code") {
		t.Fatalf("missing: %+v", f)
	}
	broken := NewProber().Claude(bg, fake(t, "exit 3"))
	if broken.Installed || broken.Available() || broken.Problem != CannotRun || !strings.Contains(broken.Guidance, "Reinstall Claude Code") {
		t.Fatalf("broken: %+v", broken)
	}
}

func TestClaudeSignInStates(t *testing.T) {
	in := NewProber().Claude(bg, fake(t, `case "$1" in --version) echo "2.1.285 (Claude Code)";; auth) echo '{"loggedIn":true,"authMethod":"claude.ai","apiKeySource":""}';; esac`))
	if !in.Available() || in.Version != "2.1.285" || in.SignedIn == nil || !*in.SignedIn || in.Auth != "claude.ai" || in.UsesAPIKey || in.Guidance != "" {
		t.Fatalf("signed in: %+v", in)
	}
	key := NewProber().Claude(bg, fake(t, `case "$1" in --version) echo 2.1.285;; auth) echo '{"loggedIn":true,"authMethod":"api_key","apiKeySource":"ANTHROPIC_API_KEY"}';; esac`))
	if !key.UsesAPIKey {
		t.Fatalf("an API key in the environment was not reported: %+v", key)
	}
	out := NewProber().Claude(bg, fake(t, `case "$1" in --version) echo 9.9.9;; auth) echo '{"loggedIn":false}';; esac`))
	if !out.Installed || out.Available() || out.Problem != SignedOut || out.SignedIn == nil || *out.SignedIn ||
		!strings.Contains(out.Detail, "not signed in") || !strings.Contains(out.Guidance, "auth login") || out.Version != "9.9.9" {
		t.Fatalf("signed out: %+v", out)
	}
	// An older version without `auth status` says nothing; the agent is assumed usable.
	old := NewProber().Claude(bg, fake(t, `case "$1" in --version) echo 1.0.0;; *) exit 2;; esac`))
	if !old.Available() || old.SignedIn != nil {
		t.Fatalf("unknown sign-in: %+v", old)
	}
}

func TestCodexSignInStates(t *testing.T) {
	in := NewProber().Codex(bg, fake(t, `case "$1" in --version) echo "codex-cli 7.7.7";; login) echo "Logged in using ChatGPT";; esac`))
	if !in.Available() || in.Version != "7.7.7" || in.SignedIn == nil || !*in.SignedIn || in.Auth != "ChatGPT" || in.UsesAPIKey {
		t.Fatalf("signed in: %+v", in)
	}
	key := NewProber().Codex(bg, fake(t, `case "$1" in --version) echo 7.7.7;; login) echo "Logged in using an API key";; esac`))
	if !key.UsesAPIKey {
		t.Fatalf("API key sign-in not reported: %+v", key)
	}
	out := NewProber().Codex(bg, fake(t, `case "$1" in --version) echo 7.7.7;; login) echo "Not logged in"; exit 1;; esac`))
	if out.Available() || out.Problem != SignedOut || out.SignedIn == nil || *out.SignedIn || !strings.Contains(out.Guidance, " login` in a terminal") {
		t.Fatalf("signed out: %+v", out)
	}
	// A failure that does not say "not logged in" is not evidence of anything.
	unknown := NewProber().Codex(bg, fake(t, `case "$1" in --version) echo 7.7.7;; login) echo "unknown command" >&2; exit 2;; esac`))
	if !unknown.Available() || unknown.SignedIn != nil {
		t.Fatalf("unknown sign-in: %+v", unknown)
	}
}

func TestAnswersAreRemembered(t *testing.T) {
	path := fake(t, `case "$1" in --version) echo 1.2.3;; auth) echo '{"loggedIn":true}';; esac`)
	p := NewProber()
	first := p.Claude(bg, path)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if again := p.Claude(bg, path); !again.Available() || again.Version != first.Version {
		t.Fatalf("not remembered: %+v", again)
	}
	if fresh := NewProber().Claude(bg, path); fresh.Installed {
		t.Fatalf("a new prober shares the old one's memory: %+v", fresh)
	}
}

func TestEffortLevelsAreReadFromTheHelp(t *testing.T) {
	help := "Options:\n  --effort <level>  Effort level for the current session (low, medium, high)\n  --model <m>  A model\n"
	got := EffortLevels(help)
	if strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("levels = %v", got)
	}
	if EffortLevels("Options:\n  --model <m>\n") != nil {
		t.Fatal("levels for a CLI with no --effort")
	}
}

func TestHelpIsAskedOnceAndEmptyWhenTheCommandIsMissing(t *testing.T) {
	p := NewProber()
	if h := p.Help(bg, filepath.Join(t.TempDir(), "nope")); h != "" {
		t.Fatalf("help = %q", h)
	}
	path := fake(t, `echo "--effort <level> (low, high)"`)
	if h := p.Help(bg, path); !strings.Contains(h, "--effort") {
		t.Fatalf("help = %q", h)
	}
}
