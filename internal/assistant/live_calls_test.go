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
	"devboard/internal/domain"
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

// Exercise the complete task-creation conversation, including the real provider's separate commentary and final
// messages, lookup, four proposals, confirmation and persisted cards. All data belongs to the disposable fixture.
func TestLiveCodexCreatesPortfolioTasksAfterConfirmation(t *testing.T) {
	liveCreatesPortfolioTasks(t, codex.New(codex.Config{}), codex.ID, os.Getenv("WERKBORD_LIVE_ASSISTANT_MODEL"))
}

// The same conversation with Claude Code, which asks for operations in text rather than through registered functions: the way
// the person who reported "went round in circles" was using it. WERKBORD_LIVE_ASSISTANT_CLAUDE_MODEL picks the model (sonnet).
func TestLiveClaudeCreatesPortfolioTasksAfterConfirmation(t *testing.T) {
	model := os.Getenv("WERKBORD_LIVE_ASSISTANT_CLAUDE_MODEL")
	if model == "" {
		model = "sonnet"
	}
	liveCreatesPortfolioTasks(t, claude.New(claude.Config{}), claude.ID, model)
}

func liveCreatesPortfolioTasks(t *testing.T, p provider.Provider, id, model string) {
	t.Helper()
	if os.Getenv("WERKBORD_LIVE_ASSISTANT") == "" {
		t.Skip("set WERKBORD_LIVE_ASSISTANT=1 to run against the real CLI")
	}
	ctx, cancel := context.WithTimeout(bg, 4*time.Minute)
	defer cancel()
	if info := p.Detect(ctx); !info.Available {
		t.Skipf("%s is not available here: %s", id, info.Detail)
	}
	f := newEfx(t, func(c *Config) {
		c.Now = time.Now
		c.Providers = provider.NewRegistry()
		if err := c.Providers.Register(p); err != nil {
			t.Fatal(err)
		}
		c.TurnTimeout = 2 * time.Minute
	})
	a := f.project("personal portfolio")
	s, err := f.eng.CreateSession(ctx, CreateRequest{Provider: id, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := f.eng.Subscribe(ctx, s.ID, s.LastEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Send(ctx, s.ID, "OK I had the following tasks to my personal portfolio project number one Extract career information from LinkedIn number two Extract coding project information from GitHub number three create a design system for the website number four Create the website in HTML JavaScript and CSS"); err != nil {
		t.Fatal(err)
	}
	var evs []Event
events:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("the stream closed before the turn ended")
			}
			evs = append(evs, ev)
			if isTerminal(ev.Type) {
				break events
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if end(evs).Type != EventCompleted {
		t.Fatalf("turn ended: %+v; error: %+v; reply: %q", end(evs), end(evs).Error, shown(evs))
	}
	if strings.Contains(shown(evs), "werkbord-call") {
		t.Fatalf("operation calls reached the conversation: %q", shown(evs))
	}
	v, err := f.eng.Session(ctx, s.ID)
	if err != nil || len(v.Pending) != 4 {
		t.Fatalf("expected four task proposals: %+v; error %v; reply %q", v, err, shown(evs))
	}
	if len(f.tickets(a.ID)) != 0 {
		t.Fatal("tasks were created before confirmation")
	}
	for _, action := range v.Pending {
		if _, err := f.eng.Resolve(ctx, s.ID, action.ID, true); err != nil {
			t.Fatalf("confirm %s: %v", action.Summary, err)
		}
	}
	tasks := f.tickets(a.ID)
	if len(tasks) != 4 {
		t.Fatalf("expected four persisted tasks, got %+v", tasks)
	}
	var titles []string
	for _, task := range tasks {
		if task.State != domain.TaskBacklog || task.ProjectID != a.ID {
			t.Fatalf("task is on the wrong board or column: %+v", task)
		}
		titles = append(titles, strings.ToLower(task.Title))
	}
	for _, want := range []string{"linkedin", "github", "design system", "website"} {
		if !strings.Contains(strings.Join(titles, "\n"), want) {
			t.Fatalf("missing task %q: %v", want, titles)
		}
	}
	t.Logf("confirmed and persisted four tasks: %v", titles)
	// Continue the same native-tool conversation and verify existing cards prevent duplicate proposals.
	ev := f.send(s.ID, "Check those four tasks on the portfolio board. If they are already there, do not add duplicates.")
	if end(ev).Type != EventCompleted {
		t.Fatalf("resumed turn: %+v", end(ev).Error)
	}
	v, err = f.eng.Session(ctx, s.ID)
	if err != nil || len(v.Pending) != 0 || len(f.tickets(a.ID)) != 4 {
		t.Fatalf("resumed conversation created duplicate proposals: %+v; %v", v, err)
	}
}
