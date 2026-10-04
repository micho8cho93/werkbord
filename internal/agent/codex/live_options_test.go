package codex

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestLiveOptions asks the installed Codex for its models. It only runs when
// DEVBOARD_LIVE_CODEX=1 and codex is installed.
func TestLiveOptions(t *testing.T) {
	if os.Getenv("DEVBOARD_LIVE_CODEX") != "1" {
		t.Skip("set DEVBOARD_LIVE_CODEX=1 to ask the installed Codex for its models")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex is not installed")
	}
	opts := New(Config{}).Options(context.Background())
	t.Logf("source=%s note=%q", opts.ModelsSource, opts.Note)
	for _, m := range opts.Models {
		t.Logf("model %s (%s) default=%v reasoning=%v", m.ID, m.Name, m.Default, m.Reasoning)
	}
	for _, r := range opts.Reasoning {
		t.Logf("reasoning %s: %s", r.ID, r.Description)
	}
	if opts.ModelsSource != "agent" || len(opts.Models) == 0 {
		t.Fatalf("expected models from the agent, got %+v", opts)
	}
}
