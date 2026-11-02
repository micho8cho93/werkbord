package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"werkbord/internal/agent/fake"
	"werkbord/internal/domain"
)

// quotaFailed runs the first agent to a provider quota failure and lets its process end, as a CLI that exits on the error does.
func quotaFailed(t *testing.T, e *env, edit bool) (*domain.Task, *domain.Run) {
	t.Helper()
	task := e.task("Build the thing")
	run := e.start(task)
	s := e.session()
	if edit {
		// Work the next agent can continue from.
		if err := os.WriteFile(filepath.Join(e.worktreeOf(run).Path, "partial.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.FailTurn("insufficient_quota")
	eventually(t, "run blocked on the provider", func() bool { r, _ := e.runs.Get(ctx, run.ID); return r.State == domain.RunBlocked })
	s.Exit(1, "quota")
	eventually(t, "run ended", func() bool { r, _ := e.runs.Get(ctx, run.ID); return r.State == domain.RunFailed && r.Blocker != nil })
	return task, run
}

func withSecondAgent(t *testing.T, e *env) *fake.Adapter {
	t.Helper()
	second := &fake.Adapter{Name: "fake-two"}
	if err := e.agents.Register(second); err != nil {
		t.Fatal(err)
	}
	return second
}

func TestFallbackContinuesOnceWithAnotherAgentFromAVerifiedHandoff(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	second := withSecondAgent(t, e)
	task, failed := quotaFailed(t, e, true)
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(second.Sessions()) != 0 {
		t.Fatal("fallback ran although the project never opted in")
	}
	if _, err := e.mgr.opt.Scheduler.SetFallbackSettings(ctx, e.project.ID, domain.FallbackSettings{Enabled: true, Providers: []string{"fake-two"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(second.Sessions()) != 1 {
		t.Fatalf("expected exactly one continuation, got %d", len(second.Sessions()))
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	next := runs[len(runs)-1]
	if len(runs) != 2 || next.AgentID != "fake-two" || next.ParentRunID != failed.ID || domain.FallbackParent(next.Origin) != failed.ID {
		t.Fatalf("%+v", runs)
	}
	l, _ := e.mgr.opt.Scheduler.FallbackLedger(ctx, e.project.ID)
	if len(l.Entries) != 1 || l.Entries[0].Result != domain.FallbackStarted || l.Entries[0].NewRun != next.ID {
		t.Fatalf("%+v", l)
	}
	// A restart and more polls start nothing further.
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	e.orchestrate(&now)
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if runs, _ := e.runs.ListByTask(ctx, task.ID); len(runs) != 2 {
		t.Fatalf("a restart started another continuation: %d runs", len(runs))
	}
}

func TestFallbackDeclinesWithoutRecordedWorkAndNeverRetries(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	second := withSecondAgent(t, e)
	task, failed := quotaFailed(t, e, false)
	if _, err := e.mgr.opt.Scheduler.SetFallbackSettings(ctx, e.project.ID, domain.FallbackSettings{Enabled: true, Providers: []string{"fake-two"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(second.Sessions()) != 0 {
		t.Fatal("started without a verified handoff")
	}
	l, _ := e.mgr.opt.Scheduler.FallbackLedger(ctx, e.project.ID)
	if len(l.Entries) != 1 || l.Entries[0].RunID != failed.ID || l.Entries[0].Result != domain.FallbackDeclined || l.Entries[0].Reason == "" {
		t.Fatalf("the refusal must be on the record: %+v", l)
	}
	if runs, _ := e.runs.ListByTask(ctx, task.ID); len(runs) != 1 {
		t.Fatal("a declined fallback created a run")
	}
}
