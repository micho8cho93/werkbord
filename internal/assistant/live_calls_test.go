package assistant

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"devboard/internal/appops"
	"devboard/internal/assistant/provider"
	"devboard/internal/assistant/provider/claude"
	"devboard/internal/assistant/provider/codex"
)

// TestLiveTheAssistantAsksForAnOperationInTheWayTheEngineReads checks, against the real command lines, that a model given the
// real system prompt writes a call the engine's reader understands: a fenced block, one JSON object, closed. A reply that
// ends on the closing fence with no newline after it is what models normally write, and the case that matters most. It uses a
// little of the person's allowance (one short turn each), so it only runs when asked:
//
//	WERKBORD_LIVE_ASSISTANT=1 go test ./internal/assistant -run TestLive -v
func TestLiveTheAssistantAsksForAnOperationInTheWayTheEngineReads(t *testing.T) {
	if os.Getenv("WERKBORD_LIVE_ASSISTANT") == "" {
		t.Skip("set WERKBORD_LIVE_ASSISTANT=1 to run against the real CLIs")
	}
	ops := appops.New(appops.Config{})
	system := SystemPrompt(ops.Catalog(appops.Principal{Grants: appops.AllPermissions}), time.Now())
	for _, p := range []provider.Provider{claude.New(claude.Config{}), codex.New(codex.Config{})} {
		t.Run(p.ID(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			if info := p.Detect(ctx); !info.Available {
				t.Skipf("%s is not available here: %s", p.ID(), info.Detail)
			}
			model := ""
			if p.ID() == claude.ID {
				model = "haiku"
			} else {
				for _, m := range p.Models(ctx).Models {
					if m.Default {
						model = m.ID
					}
				}
			}
			for _, ask := range []string{"Which projects do I have?", "What needs me today?"} {
				var raw strings.Builder
				_, err := p.Run(ctx, provider.Turn{System: system, Prompt: ask, Model: model, WorkDir: t.TempDir()}, func(e provider.Event) {
					if e.Kind == provider.EventText {
						raw.WriteString(e.Text)
					}
				})
				if err != nil {
					t.Fatalf("turn: %v", err)
				}
				var shown strings.Builder
				f := newFilter(func(s string) { shown.WriteString(s) })
				f.Write(raw.String())
				f.Flush()
				t.Logf("%q\n  the reply, exactly (%d bytes): %q\n  shown: %q\n  calls: %+v", ask, raw.Len(), raw.String(), shown.String(), f.calls)
				if len(f.calls) == 0 {
					t.Errorf("%q: the reply asked for nothing the engine could read", ask)
				}
				for _, c := range f.calls {
					if c.problem != "" {
						t.Errorf("%q: a call the engine could not read: %s", ask, c.problem)
					}
				}
			}
		})
	}
}
