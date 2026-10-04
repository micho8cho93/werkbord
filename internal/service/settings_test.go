package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/domain"
)

type catalog map[string]domain.AgentOptions

func (c catalog) Known(id string) bool { _, ok := c[id]; return ok }
func (c catalog) Options(_ context.Context, id string) (domain.AgentOptions, bool) {
	o, ok := c[id]
	return o.WithAgentDefault(), ok
}

// watch returns a function that drains the events published since it was last called.
func watch(f *fixture) func() []domain.Event {
	sub := f.bus.Subscribe(100)
	return func() []domain.Event {
		var out []domain.Event
		for {
			select {
			case ev := <-sub.C:
				out = append(out, ev)
			default:
				return out
			}
		}
	}
}

func TestGlobalDefaultsPersistAndAreEventful(t *testing.T) {
	f := newFixture(t)
	drain := watch(f)
	s := &Settings{Deps: f.deps, Catalog: catalog{"codex": {}}}
	if got, err := s.Execution(bg); err != nil || !got.IsZero() {
		t.Fatalf("fresh = %+v, %v", got, err)
	}
	drain()
	want := domain.ExecutionConfig{Agent: "codex", Model: "gpt-x", Interaction: domain.InteractionAutonomous}
	if _, err := s.SetExecution(bg, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Execution(bg); got != want {
		t.Fatalf("stored = %+v", got)
	}
	wantTypes(t, drain(), domain.EventSettingsUpdated)

	// A refused change leaves the stored defaults alone.
	if _, err := s.SetExecution(bg, domain.ExecutionConfig{Agent: "gemini"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown agent: %v", err)
	}
	if got, _ := s.Execution(bg); got != want {
		t.Fatalf("a refused change altered the defaults: %+v", got)
	}
}

func TestProjectDefaultsAreStoredPerProject(t *testing.T) {
	f := newFixture(t)
	drain := watch(f)
	a, err := f.projects.Register(bg, "/repos/a", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.projects.Register(bg, "/repos/b", "b")
	if err != nil {
		t.Fatal(err)
	}
	drain()
	cfg := domain.ExecutionConfig{Agent: "claude-code", Model: "opus"}
	got, err := f.projects.SetExecution(bg, a.ID, cfg)
	if err != nil || got.Execution != cfg {
		t.Fatalf("set = %+v, %v", got, err)
	}
	wantTypes(t, drain(), domain.EventProjectUpdated)
	if other, _ := f.projects.Get(bg, b.ID); !other.Execution.IsZero() {
		t.Fatalf("another project picked up the defaults: %+v", other.Execution)
	}
	if _, err := f.projects.SetExecution(bg, "prj_missing", cfg); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
	// The levels above a task are read together.
	s := &Settings{Deps: f.deps}
	if _, err := s.SetExecution(bg, domain.ExecutionConfig{Interaction: domain.InteractionAutonomous}); err != nil {
		t.Fatal(err)
	}
	lv, err := s.Levels(bg, a.ID)
	if err != nil || lv.Project != cfg || lv.Global.Interaction != domain.InteractionAutonomous {
		t.Fatalf("levels = %+v, %v", lv, err)
	}
	r := lv.Resolve(domain.ExecutionConfig{Interaction: domain.InteractionInteractive}, domain.ExecutionConfig{})
	if r.Agent != "claude-code" || r.Model != "opus" || r.Interaction != domain.InteractionInteractive || r.Sources.Interaction != domain.SourceTask {
		t.Fatalf("resolved = %+v", r)
	}
}

func TestOnboardingIsRememberedAndNeverOpensOverExistingWork(t *testing.T) {
	f := newFixture(t)
	s := &Settings{Deps: f.deps}
	ob, err := s.Onboarding(bg)
	if err != nil || ob.CompletedAt != nil {
		t.Fatalf("a fresh install = %+v, %v", ob, err)
	}
	if _, err := s.CompleteOnboarding(bg, []string{"network", "nonsense"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("an unknown step: %v", err)
	}
	done, err := s.CompleteOnboarding(bg, []string{"github"})
	if err != nil || done.CompletedAt == nil || len(done.Skipped) != 1 {
		t.Fatalf("complete = %+v, %v", done, err)
	}
	if again, _ := s.Onboarding(bg); again.CompletedAt == nil || again.Skipped[0] != "github" {
		t.Fatalf("remembered = %+v", again)
	}
	if err := s.ResetOnboarding(bg); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Onboarding(bg); again.CompletedAt != nil {
		t.Fatalf("after reset = %+v", again)
	}

	// An existing installation has projects and no record: it is not asked to set up again.
	if _, err := f.projects.Register(bg, "/repos/old", "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetOnboarding(bg); err != nil {
		t.Fatal(err)
	}
	if ob, _ := s.Onboarding(bg); ob.CompletedAt == nil {
		t.Fatal("setup would open over a computer that already has projects")
	}
}

func TestThisComputerIsRegisteredOnceAsARunner(t *testing.T) {
	f := newFixture(t)
	s := &Settings{Deps: f.deps, Version: "1.2.3"}
	first, err := s.RegisterRunner(bg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != domain.RunnerLocal || first.OS == "" || first.Arch == "" || first.Version != "1.2.3" || !first.Online || first.Name == "" {
		t.Fatalf("runner = %+v", first)
	}
	// Every start registers again; it is the same runner.
	s.Version = "1.3.0"
	second, err := s.RegisterRunner(bg)
	if err != nil || second.ID != first.ID || second.Version != "1.3.0" {
		t.Fatalf("second = %+v, %v", second, err)
	}
	rs, err := s.Runners(bg)
	if err != nil || len(rs) != 1 || !rs[0].Online {
		t.Fatalf("runners = %+v, %v", rs, err)
	}
}
