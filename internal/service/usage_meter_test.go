package service

import (
	"context"
	"testing"

	"werkbord/internal/domain"
)

func tokens(n int64) *int64 { return &n }

// usedBy makes a finished run by an agent that reported this many tokens an hour ago, on the fixture's clock.
func usedBy(t *testing.T, f *fixture, s *Scheduler, taskID, agent string, in int64) {
	t.Helper()
	ctx := context.Background()
	run, err := f.runs.Create(ctx, NewRun{TaskID: taskID, AgentID: agent, Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.SetUsage(ctx, run.ID, domain.Usage{InputTokens: tokens(in), OutputTokens: tokens(0), CostKind: "usage_only", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.End(ctx, run.ID, Ended{State: domain.RunStopped}); err != nil {
		t.Fatal(err)
	}
}

func TestTheMeterReadsWhatRunsReportedAndSaverHoldsAutomaticStartsOnly(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	spent, _ := f.tasks.Create(ctx, pid, "Already worked on", "")
	usedBy(t, f, s, spent.ID, "claude-code", 900)
	// A task the person pointed at that agent, set to start by itself.
	auto := arm(t, f, pid, "Runs by itself", domain.Orchestration{})
	cfg := domain.ExecutionConfig{Agent: "claude-code"}
	if _, err := f.tasks.Update(ctx, auto.ID, TaskPatch{Version: auto.Version, Execution: &cfg}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetUsageSettings(ctx, domain.UsageSettings{Mode: domain.UsageStandard, Budgets: map[string]domain.UsageBudget{"claude-code": {FiveHour: 1000}}}); err != nil {
		t.Fatal(err)
	}
	m, err := s.UsageMeter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Agents) != 1 || m.Agents[0].FiveHour.Tokens != 900 || m.Agents[0].FiveHour.Percent == nil || *m.Agents[0].FiveHour.Percent != 90 {
		t.Fatalf("%+v", m.Agents)
	}
	if d := decisionFor(t, s, pid, auto.ID); d.State != "runnable" {
		t.Fatalf("standard mode only measures: %+v", d)
	}

	if _, err := s.SetUsageSettings(ctx, domain.UsageSettings{Mode: domain.UsageSaver, Budgets: map[string]domain.UsageBudget{"claude-code": {FiveHour: 1000}}}); err != nil {
		t.Fatal(err)
	}
	d := decisionFor(t, s, pid, auto.ID)
	if d.State != "waiting_usage" || d.Reason == "" {
		t.Fatalf("an automatic start on an agent at 90%% of its budget waits: %+v", d)
	}
	if why := s.UsageHeld(ctx, "claude-code"); why == "" {
		t.Fatal("fallback must see the hold")
	}
	if why := s.UsageHeld(ctx, "codex"); why != "" {
		t.Fatal("an agent with no budget is never held")
	}
	if err := s.CheckStart(ctx, auto.ID, auto.Orchestration.Key); err == nil {
		t.Fatal("the scheduler's own start is refused while held")
	}
	// The person's own start is never refused.
	if err := s.CheckStart(ctx, auto.ID, ""); err != nil {
		t.Fatalf("a manual start must not be held by Usage saver: %v", err)
	}
	// Raising the budget releases it.
	if _, err := s.SetUsageSettings(ctx, domain.UsageSettings{Mode: domain.UsageSaver, Budgets: map[string]domain.UsageBudget{"claude-code": {FiveHour: 5000}}}); err != nil {
		t.Fatal(err)
	}
	if d := decisionFor(t, s, pid, auto.ID); d.State != "runnable" {
		t.Fatalf("%+v", d)
	}
}

func TestUsageSettingsDefaultToMeasuringOnly(t *testing.T) {
	_, s, _, _ := schedulingFixture(t)
	cfg, err := s.UsageSettings(context.Background())
	if err != nil || cfg.Mode != domain.UsageStandard || cfg.SaverAt != 80 || len(cfg.Budgets) != 0 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := s.SetUsageSettings(context.Background(), domain.UsageSettings{Mode: "frugal"}); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
}
