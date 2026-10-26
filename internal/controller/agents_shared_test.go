//go:build unix

package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/agentkit"
	"werkbord/internal/config"
)

// The coding-agent adapter and the assistant's provider for the same CLI are built over one prober, so the CLI is asked
// once, not once for each.
func TestTheAdapterAndTheProviderShareWhatTheCLISaid(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$1\" in --version) echo '2.1.285 (Claude Code)';; auth) echo '{\"loggedIn\":true}';; --help) echo '--tools --system-prompt --include-partial-messages --session-id --resume --effort <level> (low, high)';; esac\n"
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Agents: map[string]config.AgentConfig{config.AgentClaudeCode: {Command: cli}}}
	probe := agentkit.NewProber()
	agents, err := newAgents(cfg, probe)
	if err != nil {
		t.Fatal(err)
	}
	providers, err := newAssistantProviders(cfg, probe)
	if err != nil {
		t.Fatal(err)
	}
	a, err := agents.Get("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	p, ok := providers.Get("claude-code")
	if !ok {
		t.Fatal("no claude-code provider")
	}
	if got := a.Detect(context.Background()); !got.Available || got.Version != "2.1.285" {
		t.Fatalf("adapter: %+v", got)
	}
	if got := p.Detect(context.Background()); !got.Available || got.Version != "2.1.285" {
		t.Fatalf("provider: %+v", got)
	}
	raw, _ := os.ReadFile(log)
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var versions int
	for _, c := range calls {
		if c == "--version" {
			versions++
		}
	}
	if versions != 1 || len(calls) != 3 { // --version, auth status, --help: each once
		t.Fatalf("the CLI was asked %d times (%v), want each question once", len(calls), calls)
	}
}
