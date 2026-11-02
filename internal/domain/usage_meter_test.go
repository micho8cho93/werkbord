package domain

import (
	"errors"
	"testing"
	"time"
)

func i64(n int64) *int64 { return &n }

func usageRun(agent string, started, ended time.Duration, now time.Time, in, out *int64) Run {
	r := Run{AgentID: agent, State: RunCompleted, CreatedAt: now.Add(-started), Usage: Usage{InputTokens: in, OutputTokens: out}}
	if ended >= 0 {
		e := now.Add(-ended)
		r.EndedAt = &e
	} else {
		r.State = RunRunning
	}
	return r
}

func agentUsage(m UsageMeter, agent string) AgentUsage {
	for _, a := range m.Agents {
		if a.Agent == agent {
			return a
		}
	}
	return AgentUsage{}
}

func TestUsageIsCountedInRollingWindowsFromWhatRunsReported(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	h := time.Hour
	m := MeasureUsage([]Run{
		usageRun("claude-code", 2*h, h, now, i64(1000), i64(500)),     // inside both windows
		usageRun("claude-code", 10*h, 8*h, now, i64(4000), i64(0)),    // inside the week only
		usageRun("claude-code", 10*24*h, 9*24*h, now, i64(9), i64(9)), // outside both
		usageRun("codex", 6*h, 0, now, i64(600), nil),                 // straddles the 5-hour window: 5 of its 6 hours are inside
		usageRun("codex", h, -1, now, nil, nil),                       // running, no tokens reported yet
	}, UsageSettings{}, now)
	cc := agentUsage(m, "claude-code")
	if cc.FiveHour.Tokens != 1500 || cc.Weekly.Tokens != 5500 || cc.FiveHour.Runs != 1 || cc.Weekly.Runs != 2 {
		t.Fatalf("%+v", cc)
	}
	cx := agentUsage(m, "codex")
	if cx.FiveHour.Tokens != 500 || cx.Weekly.Tokens != 600 {
		t.Fatalf("a run that began before the window counts its share inside it: %+v", cx)
	}
	if cx.FiveHour.Unreported != 1 || cx.FiveHour.Runs != 2 {
		t.Fatalf("a run that reported no tokens is counted as a run and as unreported: %+v", cx.FiveHour)
	}
	if cc.FiveHour.EasesAt == nil || !cc.FiveHour.EasesAt.Equal(now.Add(-2*h).Add(5*h)) {
		t.Fatalf("the figure starts to fall when the oldest counted run leaves the window: %v", cc.FiveHour.EasesAt)
	}
}

func TestCachedTokensAreNotAddedToWhatWasUsed(t *testing.T) {
	now := time.Now().UTC()
	r := usageRun("codex", time.Hour, 0, now, i64(100), i64(50))
	r.Usage.CachedTokens = i64(90)
	if got := agentUsage(MeasureUsage([]Run{r}, UsageSettings{}, now), "codex").FiveHour.Tokens; got != 150 {
		t.Fatalf("cached tokens may overlap input and are left out: %d", got)
	}
}

func TestBudgetsMakePercentsAndSaverHoldsOnlyInSaverMode(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	runs := []Run{usageRun("claude-code", 2*time.Hour, time.Hour, now, i64(800), i64(100))}
	budgets := map[string]UsageBudget{"claude-code": {FiveHour: 1000, Weekly: 100000}, "codex": {FiveHour: 500}}
	std := MeasureUsage(runs, UsageSettings{Budgets: budgets}, now)
	cc := agentUsage(std, "claude-code")
	if cc.FiveHour.Percent == nil || *cc.FiveHour.Percent != 90 || cc.Weekly.Percent == nil || *cc.Weekly.Percent != 0 {
		t.Fatalf("%+v", cc)
	}
	if cc.Held != "" {
		t.Fatal("standard mode measures and never holds")
	}
	if cx := agentUsage(std, "codex"); cx.FiveHour.Tokens != 0 || cx.FiveHour.Percent == nil {
		t.Fatalf("an agent with a budget and no runs still shows its meter: %+v", cx)
	}
	sv := MeasureUsage(runs, UsageSettings{Mode: UsageSaver, Budgets: budgets}, now)
	held, all := sv.Holds()
	if held["claude-code"] == "" || len(held) != 1 || all {
		t.Fatalf("saver holds the agent at 90%% and not the idle one: %v %v", held, all)
	}
	sv = MeasureUsage(runs, UsageSettings{Mode: UsageSaver, SaverAt: 95, Budgets: budgets}, now)
	if held, _ := sv.Holds(); len(held) != 0 {
		t.Fatal("below the person's threshold nothing is held")
	}
	only := MeasureUsage(runs, UsageSettings{Mode: UsageSaver, Budgets: map[string]UsageBudget{"claude-code": {FiveHour: 1000}}}, now)
	if _, all := only.Holds(); !all {
		t.Fatal("when every budgeted agent is held, an automatic choice has nowhere to go")
	}
}

func TestUsageSettingsAreValidated(t *testing.T) {
	ok := UsageSettings{Mode: UsageSaver, SaverAt: 80, Budgets: map[string]UsageBudget{"codex": {FiveHour: 1}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]UsageSettings{
		"mode":      {Mode: "frugal", SaverAt: 80},
		"too low":   {Mode: UsageSaver, SaverAt: 10},
		"too high":  {Mode: UsageSaver, SaverAt: 100},
		"agent id":  {Mode: UsageSaver, SaverAt: 80, Budgets: map[string]UsageBudget{"Codex!": {}}},
		"negative":  {Mode: UsageSaver, SaverAt: 80, Budgets: map[string]UsageBudget{"codex": {Weekly: -1}}},
		"too large": {Mode: UsageSaver, SaverAt: 80, Budgets: map[string]UsageBudget{"codex": {Weekly: 1 << 50}}},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var zero UsageSettings
	zero.Normalize()
	if err := zero.Validate(); err != nil || zero.Mode != UsageStandard || zero.SaverAt != 80 {
		t.Fatalf("%+v %v", zero, err)
	}
}
