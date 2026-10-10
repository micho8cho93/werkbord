// Package live checks the providers against the real Claude Code and Codex on this computer, signed in as the person is.
// It uses a little of their allowance (a handful of one-word turns), so it only runs when asked:
//
//	WERKBORD_LIVE_ASSISTANT=1 go test ./internal/assistant/provider/live -v
//
// It is how the claims in docs/ASSISTANT.md were checked, and how to check them again after either CLI updates.
package live

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"devboard/internal/assistant/provider"
	"devboard/internal/assistant/provider/claude"
	"devboard/internal/assistant/provider/codex"
)

func providers() []provider.Provider {
	return []provider.Provider{claude.New(claude.Config{}), codex.New(codex.Config{})}
}

func TestLiveProviders(t *testing.T) {
	if os.Getenv("WERKBORD_LIVE_ASSISTANT") == "" {
		t.Skip("set WERKBORD_LIVE_ASSISTANT=1 to run against the real CLIs")
	}
	for _, p := range providers() {
		t.Run(p.ID(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			info := p.Detect(ctx)
			t.Logf("detect: %+v", info)
			if !info.Available {
				t.Skipf("%s is not available here: %s", p.ID(), info.Detail)
			}
			models := p.Models(ctx)
			t.Logf("models (%s): %+v", models.Source, models)
			model := ""
			for _, m := range models.Models {
				if m.Default {
					model = m.ID
				}
			}
			if p.ID() == claude.ID {
				model = "haiku" // the cheapest, for a one-word answer
			}
			dir := t.TempDir()
			system := "You are a terse test assistant. Answer with a single word."

			var pieces []string
			var ref string
			first, err := p.Run(ctx, provider.Turn{System: system, Prompt: "Remember the code word AZURE. Reply with just: ok", Model: model, WorkDir: dir}, func(e provider.Event) {
				switch e.Kind {
				case provider.EventText:
					pieces = append(pieces, e.Text)
				case provider.EventRef:
					ref = e.Ref
				}
			})
			if err != nil {
				t.Fatalf("first turn: %v", err)
			}
			t.Logf("first turn: %q in %d pieces, ref %s, usage %+v, model %s", first.Text, len(pieces), first.Ref, first.Usage, first.Model)
			if first.Ref == "" || first.Ref != ref || !strings.Contains(strings.ToLower(first.Text), "ok") {
				t.Fatalf("first = %+v ref=%q", first, ref)
			}

			second, err := p.Run(ctx, provider.Turn{Ref: first.Ref, System: system, Prompt: "What was the code word? One word.", Model: model, WorkDir: dir}, func(provider.Event) {})
			if err != nil {
				t.Fatalf("second turn: %v", err)
			}
			if !strings.Contains(strings.ToUpper(second.Text), "AZURE") {
				t.Fatalf("the conversation was not continued: %q", second.Text)
			}

			// A handle the provider does not have.
			bad := "00000000-0000-4000-8000-000000000000"
			_, err = p.Run(ctx, provider.Turn{Ref: bad, System: system, Prompt: "hi", Model: model, WorkDir: dir}, func(provider.Event) {})
			t.Logf("unknown handle: kind=%s err=%v", provider.KindOf(err), err)
			if provider.KindOf(err) != provider.KindSessionLost {
				t.Errorf("an unknown conversation must be session_lost, got %v", err)
			}

			// A model that does not exist.
			_, err = p.Run(ctx, provider.Turn{System: system, Prompt: "hi", Model: "no-such-model-xyz", WorkDir: dir}, func(provider.Event) {})
			t.Logf("unknown model: kind=%s err=%v", provider.KindOf(err), err)
			if provider.KindOf(err) != provider.KindModelUnavailable {
				t.Errorf("an unknown model must be model_unavailable, got %v", err)
			}

			// Cancelling a long reply.
			cctx, ccancel := context.WithCancel(ctx)
			started := make(chan struct{}, 1)
			done := make(chan error, 1)
			go func() {
				_, err := p.Run(cctx, provider.Turn{System: "You write long answers.", Prompt: "Count from 1 to 400, one number per line, no other text.", Model: model, WorkDir: dir}, func(e provider.Event) {
					if e.Kind == provider.EventText {
						select {
						case started <- struct{}{}:
						default:
						}
					}
				})
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("the long turn ended before it could be cancelled: %v", err)
			case <-time.After(90 * time.Second):
				t.Fatal("no text arrived")
			}
			t0 := time.Now()
			ccancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("cancel: %v", err)
				}
				t.Logf("cancelled in %s", time.Since(t0))
			case <-time.After(20 * time.Second):
				t.Fatal("cancelling did not stop the turn")
			}
		})
	}
}
