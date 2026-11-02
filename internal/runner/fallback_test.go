package runner

import (
	"encoding/json"
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

// A rate limit that ends a turn leaves the process open and idle, so the run is blocked, not failed. Fallback waits out the
// grace period, then ends that session itself and continues with the next agent.
func TestFallbackStopsASessionLeftOpenByAProviderLimitAfterTheGracePeriod(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	second := withSecondAgent(t, e)
	task := e.task("Build the thing")
	run := e.start(task)
	s := e.session()
	if err := os.WriteFile(filepath.Join(e.worktreeOf(run).Path, "partial.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.FailTurn("rate_limit_error: 429")
	eventually(t, "run blocked on the provider", func() bool { r, _ := e.runs.Get(ctx, run.ID); return r.State == domain.RunBlocked })
	if _, err := e.mgr.opt.Scheduler.SetFallbackSettings(ctx, e.project.ID, domain.FallbackSettings{Enabled: true, Providers: []string{"fake-two"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.runs.Get(ctx, run.ID); r.State != domain.RunBlocked || len(second.Sessions()) != 0 {
		t.Fatalf("a session just stuck on a limit is left to the person: %s", r.State)
	}
	now = now.Add(domain.FallbackGrace + time.Second)
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := e.runs.Get(ctx, run.ID); r.State != domain.RunStopped {
		t.Fatalf("the stuck session should have been stopped, it is %s", r.State)
	}
	if len(second.Sessions()) != 1 {
		t.Fatalf("expected one continuation, got %d", len(second.Sessions()))
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 2 || runs[1].AgentID != "fake-two" || domain.FallbackParent(runs[1].Origin) != run.ID {
		t.Fatalf("%+v", runs)
	}
	// Fallback acts while nobody is looking, so it says what it did on the run it acted on.
	var notice *domain.FallbackNotice
	for _, ev := range e.log(run.ID) {
		if ev.Type == domain.EventAgentFallback {
			notice = &domain.FallbackNotice{}
			if err := json.Unmarshal(ev.Payload, notice); err != nil {
				t.Fatal(err)
			}
		}
	}
	if notice == nil || notice.Result != domain.FallbackStarted || notice.Agent != "fake-two" || notice.NewRun != runs[1].ID {
		t.Fatalf("no fallback notice on the run: %+v", notice)
	}
}

func TestFallbackSaysWhyItDidNotContinue(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	withSecondAgent(t, e)
	_, failed := quotaFailed(t, e, false)
	if _, err := e.mgr.opt.Scheduler.SetFallbackSettings(ctx, e.project.ID, domain.FallbackSettings{Enabled: true, Providers: []string{"fake-two"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var notices []domain.FallbackNotice
	for _, ev := range e.log(failed.ID) {
		if ev.Type == domain.EventAgentFallback {
			var n domain.FallbackNotice
			if err := json.Unmarshal(ev.Payload, &n); err != nil {
				t.Fatal(err)
			}
			notices = append(notices, n)
		}
	}
	if len(notices) != 1 || notices[0].Result != domain.FallbackDeclined || notices[0].Reason == "" {
		t.Fatalf("a declined fallback must be said once, with its reason: %+v", notices)
	}
}
