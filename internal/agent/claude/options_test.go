package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

func ids(opts []domain.AgentOption) []string {
	var out []string
	for _, o := range opts {
		out = append(out, o.ID)
	}
	return out
}

func TestOptionsAreTheAliasesAndTheLevelsTheCLIReports(t *testing.T) {
	o := New(Config{Command: fakeClaude(t)}).Options(context.Background())
	if got := strings.Join(ids(o.Models), ","); got != "sonnet,opus,haiku" || o.ModelsSource != domain.OptionsBuiltIn || !o.CustomModels {
		t.Fatalf("models = %s (%s), custom=%v", got, o.ModelsSource, o.CustomModels)
	}
	if got := strings.Join(ids(o.Reasoning), ","); got != "low,medium,high,xhigh,max" || o.ReasoningSource != domain.OptionsFromAgent {
		t.Fatalf("reasoning = %s (%s): it should be whatever --help says", got, o.ReasoningSource)
	}
}

func TestOptionsForACLIWithoutEffort(t *testing.T) {
	t.Setenv("FAKE_CLAUDE_NO_EFFORT", "1")
	a := New(Config{Command: fakeClaude(t)})
	o := a.Options(context.Background())
	if len(o.Reasoning) != 0 || o.Note == "" {
		t.Fatalf("reasoning = %+v, note %q", o.Reasoning, o.Note)
	}
	// Asking for a level it cannot honour is an error that says what to do, not a silent no-op.
	_, err := a.Start(context.Background(), agent.StartRequest{WorkDir: t.TempDir(), Prompt: "hello", Reasoning: "high"})
	if err == nil || !strings.Contains(err.Error(), "--effort") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfiguredOptionsReplaceTheBuiltInOnes(t *testing.T) {
	o := New(Config{Command: fakeClaude(t),
		Models:    []domain.AgentOption{{ID: "my-model", Name: "Mine"}},
		Reasoning: []string{"quick", "slow"},
	}).Options(context.Background())
	if strings.Join(ids(o.Models), ",") != "my-model" || o.ModelsSource != domain.OptionsConfigured ||
		strings.Join(ids(o.Reasoning), ",") != "quick,slow" || o.ReasoningSource != domain.OptionsConfigured {
		t.Fatalf("options = %+v", o)
	}
}

func TestOptionsForAMissingCLIStillOfferTheAliases(t *testing.T) {
	o := New(Config{Command: "definitely-not-claude"}).Options(context.Background())
	if len(o.Models) == 0 || len(o.Reasoning) != 0 {
		t.Fatalf("options = %+v", o)
	}
}

func startArgs(t *testing.T, cfg Config, req agent.StartRequest) string {
	t.Helper()
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_CLAUDE_ARGS_FILE", argsFile)
	if cfg.Command == "" {
		cfg.Command = fakeClaude(t)
	}
	req.WorkDir, req.Prompt = t.TempDir(), "hello"
	s, err := New(cfg).Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	b, _ := os.ReadFile(argsFile)
	return string(b)
}

func TestTheRunsModelAndReasoningReachTheCLI(t *testing.T) {
	args := startArgs(t, Config{Model: "sonnet"}, agent.StartRequest{Model: "opus", Reasoning: "high"})
	lines := strings.Split(args, "\n")
	pair := func(flag string) string {
		for i, l := range lines {
			if l == flag && i+1 < len(lines) {
				return lines[i+1]
			}
		}
		return ""
	}
	if pair("--model") != "opus" || pair("--effort") != "high" {
		t.Fatalf("a run's choice must win over the configured model:\n%s", args)
	}
}

func TestAgentDefaultPassesNothingButAConfiguredModelStillApplies(t *testing.T) {
	args := startArgs(t, Config{}, agent.StartRequest{})
	if strings.Contains(args, "--model") || strings.Contains(args, "--effort") {
		t.Fatalf("nothing was chosen, nothing should be passed:\n%s", args)
	}
	// config.json's model is what "Agent default" means on this computer.
	args = startArgs(t, Config{Model: "haiku"}, agent.StartRequest{})
	if !strings.Contains(args, "--model\nhaiku") || strings.Contains(args, "--effort") {
		t.Fatalf("args:\n%s", args)
	}
}

func TestDetectReportsInstalledAndSignIn(t *testing.T) {
	ok := New(Config{Command: fakeClaude(t)}).Detect(context.Background())
	if !ok.Installed || ok.SignIn != domain.SignedIn || !ok.Available || ok.Guidance != "" {
		t.Fatalf("signed in = %+v", ok)
	}
	missing := New(Config{Command: "definitely-not-claude"}).Detect(context.Background())
	if missing.Installed || missing.SignIn != "" || missing.Available || !strings.Contains(missing.Guidance, "Install Claude Code") || missing.DocsURL == "" {
		t.Fatalf("missing = %+v: it should say how to install it", missing)
	}
	t.Setenv("FAKE_CLAUDE_LOGGED_OUT", "1")
	out := New(Config{Command: fakeClaude(t)}).Detect(context.Background())
	if !out.Installed || out.SignIn != domain.SignedOut || out.Available || !strings.Contains(out.Guidance, "auth login") {
		t.Fatalf("signed out = %+v", out)
	}
}
