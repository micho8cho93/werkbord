package codex

import (
	"context"
	"encoding/json"
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

func TestOptionsComeFromTheAgentItself(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	o := New(Config{Command: fakeCodex(t)}).Options(context.Background())
	if o.ModelsSource != domain.OptionsFromAgent || o.ReasoningSource != domain.OptionsFromAgent {
		t.Fatalf("sources = %s / %s, note %q", o.ModelsSource, o.ReasoningSource, o.Note)
	}
	// Both pages are read; the hidden model is not offered; a model with no display name is still named.
	if got := strings.Join(ids(o.Models), ","); got != "big-model,small-model" {
		t.Fatalf("models = %s", got)
	}
	if o.Models[0].Name != "Big" || !o.Models[0].Default || o.Models[0].Description != "the big one" || o.Models[1].Name != "small-model" {
		t.Fatalf("models = %+v", o.Models)
	}
	if got := strings.Join(o.Models[0].Reasoning, ","); got != "low,medium,high" {
		t.Fatalf("the model should say which levels it supports: %s", got)
	}
	if got := strings.Join(ids(o.Reasoning), ","); got != "low,medium,high,xhigh" {
		t.Fatalf("reasoning = %s", got)
	}
	if o.Reasoning[1].Description != "balanced" {
		t.Fatalf("reasoning = %+v", o.Reasoning)
	}
	b, _ := os.ReadFile(logFile)
	if !strings.Contains(string(b), `"cursor":"page-2"`) {
		t.Fatalf("the second page was never asked for:\n%s", b)
	}
}

func TestOptionsAreRememberedBriefly(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	a := New(Config{Command: fakeCodex(t)})
	for i := 0; i < 4; i++ {
		a.Options(context.Background())
	}
	b, _ := os.ReadFile(logFile)
	if n := strings.Count(string(b), `"method":"initialize"`); n != 1 {
		t.Fatalf("Codex was started %d times for 4 requests; each would spawn a process", n)
	}
}

func TestOptionsFallBackWhenCodexCannotListModels(t *testing.T) {
	t.Setenv("FAKE_CODEX_NO_MODELS", "1")
	o := New(Config{Command: fakeCodex(t)}).Options(context.Background())
	if o.ModelsSource != domain.OptionsBuiltIn || len(o.Models) != 0 || !strings.Contains(o.Note, "model/list") || !o.CustomModels {
		t.Fatalf("options = %+v: it should say it could not ask, and still let a model be typed in", o)
	}
	missing := New(Config{Command: "definitely-not-codex"}).Options(context.Background())
	if !strings.Contains(missing.Note, "not installed") {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestConfiguredOptionsAreUsedWithoutAskingCodex(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	o := New(Config{Command: fakeCodex(t), Models: []domain.AgentOption{{ID: "mine", Name: "Mine"}}, Reasoning: []string{"quick"}}).Options(context.Background())
	if strings.Join(ids(o.Models), ",") != "mine" || o.ModelsSource != domain.OptionsConfigured || strings.Join(ids(o.Reasoning), ",") != "quick" {
		t.Fatalf("options = %+v", o)
	}
	if _, err := os.Stat(logFile); err == nil {
		t.Fatal("Codex was started though the user configured the list")
	}
}

func TestTheRunsModelAndReasoningReachCodex(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	s, err := New(Config{Command: fakeCodex(t), Model: "configured"}).Start(context.Background(),
		agent.StartRequest{WorkDir: dir, Prompt: "hello", Model: "run-model", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, s)
	got := map[string]map[string]any{}
	b, _ := os.ReadFile(logFile)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var rec struct {
			Method string
			Params map[string]any
		}
		_ = json.Unmarshal([]byte(line), &rec)
		got[rec.Method] = rec.Params
	}
	if got["thread/start"]["model"] != "run-model" {
		t.Errorf("thread/start = %v: a run's model wins over the configured one", got["thread/start"])
	}
	if got["turn/start"]["effort"] != "high" {
		t.Errorf("turn/start = %v", got["turn/start"])
	}
}

func TestAgentDefaultPassesNoModelOrEffortToCodex(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log")
	t.Setenv("FAKE_CODEX_LOG", logFile)
	s, _ := start(t, Config{}, "hello")
	collect(t, s)
	b, _ := os.ReadFile(logFile)
	if strings.Contains(string(b), `"model"`) || strings.Contains(string(b), `"effort"`) {
		t.Fatalf("nothing was chosen, nothing should be passed:\n%s", b)
	}
}

func TestDetectReportsInstalledAndSignIn(t *testing.T) {
	ok := New(Config{Command: fakeCodex(t)}).Detect(context.Background())
	if !ok.Installed || ok.SignIn != domain.SignedIn || !ok.Available {
		t.Fatalf("signed in = %+v", ok)
	}
	missing := New(Config{Command: "definitely-not-codex"}).Detect(context.Background())
	if missing.Installed || missing.Available || !strings.Contains(missing.Guidance, "Install Codex") {
		t.Fatalf("missing = %+v", missing)
	}
	t.Setenv("FAKE_CODEX_LOGGED_OUT", "1")
	out := New(Config{Command: fakeCodex(t)}).Detect(context.Background())
	if !out.Installed || out.SignIn != domain.SignedOut || out.Available || !strings.Contains(out.Guidance, "codex login") {
		t.Fatalf("signed out = %+v", out)
	}
}
