package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func TestAnalyticsAggregation(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	ptr := func(n int64) *int64 { return &n }
	cost := 2.0
	data := store.AnalyticsData{Tasks: []domain.Task{
		{ID: "one", ProjectID: "p", CreatedAt: start, LabelIDs: []string{"bug"}, State: domain.TaskDone},
		{ID: "two", ProjectID: "p", CreatedAt: start, LabelIDs: []string{"bug", "feature"}},
		{ID: "blocked", ProjectID: "p", CreatedAt: start.Add(-24 * time.Hour)},
		{ID: "stopped", ProjectID: "p", CreatedAt: start.Add(-24 * time.Hour)},
		{ID: "old-done", ProjectID: "p", CreatedAt: start.Add(-72 * time.Hour), State: domain.TaskDone},
	}, Runs: []domain.Run{
		{ID: "r1", TaskID: "one", ProjectID: "p", AgentID: "codex", RunnerID: "local", Model: "model-a", CreatedAt: start, State: domain.RunCompleted, Attempt: 1, Usage: domain.Usage{InputTokens: ptr(100), OutputTokens: ptr(0), CostUSD: &cost, CostKind: "actual_api", Source: "provider"}},
		{ID: "r2", TaskID: "two", ProjectID: "p", AgentID: "claude-code", RunnerID: "remote", CreatedAt: start, State: domain.RunFailed, Attempt: 2},
		{ID: "r3", TaskID: "blocked", ProjectID: "p", AgentID: "codex", CreatedAt: start.Add(-24 * time.Hour), State: domain.RunBlocked},
		{ID: "r4", TaskID: "stopped", ProjectID: "p", AgentID: "codex", CreatedAt: start.Add(-24 * time.Hour), State: domain.RunStopped},
	}}
	event := func(kind domain.EventType, task, run string, at time.Time, payload any) {
		b, _ := json.Marshal(payload)
		data.Events = append(data.Events, domain.Event{Seq: int64(len(data.Events) + 1), ProjectID: "p", TaskID: task, RunID: run, Type: kind, CreatedAt: at, Payload: b})
	}
	event(domain.EventTaskCreated, "old-done", "", start.Add(-72*time.Hour), map[string]any{"state": "backlog"})
	event(domain.EventTaskUpdated, "old-done", "", start.Add(-48*time.Hour), map[string]any{"state": "done"})
	event(domain.EventTaskCreated, "one", "", start, map[string]any{"state": "backlog"})
	event(domain.EventTaskCreated, "two", "", start, map[string]any{"state": "backlog"})
	event(domain.EventRunStateChanged, "one", "r1", start, map[string]any{"acceptance": ""})
	event(domain.EventTaskUpdated, "one", "", start.Add(time.Hour), map[string]any{"state": "done"})
	event(domain.EventTaskUpdated, "one", "", start.Add(2*time.Hour), map[string]any{"state": "done"}) // editing Done is not completion
	event(domain.EventTaskUpdated, "two", "", start.Add(3*time.Hour), map[string]any{"state": "done"})
	event(domain.EventTaskUpdated, "old-done", "", start.Add(4*time.Hour), map[string]any{"state": "done"})
	event(domain.EventAgentCompleted, "one", "r1", start.Add(time.Hour), map[string]any{})
	event(domain.EventAgentCompleted, "one", "r1", start.Add(time.Hour), map[string]any{}) // dedup reports
	event(domain.EventRunStateChanged, "one", "r1", start.Add(2*time.Hour), map[string]any{"acceptance": "accepted"})
	event(domain.EventRunStateChanged, "one", "r1", start.Add(3*time.Hour), map[string]any{"acceptance": "accepted"}) // cumulative usage snapshot
	event(domain.EventGitMerged, "one", "r1", start.Add(4*time.Hour), map[string]any{"ok": true, "outcome": "done"})
	event(domain.EventAgentOutput, "one", "r1", start.Add(5*time.Hour), map[string]any{"stream": "system", "text": "The turn failed: usage limit reached"})
	event(domain.EventAgentOutput, "two", "r2", start.Add(5*time.Hour), map[string]any{"stream": "system", "text": "Rate limit: rejected"})
	event(domain.EventAgentOutput, "two", "r2", start.Add(5*time.Hour), map[string]any{"stream": "assistant", "text": "Rate limit: rejected"})
	out := aggregateAnalytics(data, []domain.Label{{ID: "bug", Name: "Bug"}, {ID: "feature", Name: "Feature"}}, AnalyticsFilter{}, start, end)
	if out.Created != 2 || out.Completed != 2 || out.DurationSamples != 2 || *out.AverageTaskSeconds != 7200 || *out.MedianTaskSeconds != 7200 {
		t.Fatalf("throughput/duration: %+v", out)
	}
	if out.Attempts != 2 || out.Retries != 1 || out.Blocked != 1 || out.Stopped != 1 || out.AgentReportedCompletion != nil || out.NormalSessionCompletions != 1 || out.VerifiedCompletion != nil || out.VerifiedMerges != 1 || out.Accepted != 1 || out.LimitEvents != 2 {
		t.Fatalf("execution evidence: %+v", out)
	}
	if out.InputTokens == nil || *out.InputTokens != 100 || out.OutputTokens == nil || *out.OutputTokens != 0 || out.CachedTokens != nil || out.InputRuns != 1 || out.ActualCostUSD == nil || *out.ActualCostUSD != 2 || out.EstimatedCostUSD != nil {
		t.Fatalf("usage: %+v", out.AnalyticsMeasurements)
	}
	filtered := aggregateAnalytics(data, nil, AnalyticsFilter{ProjectID: "p", Provider: "codex"}, start, end)
	if filtered.Created != 1 || filtered.Completed != 1 || filtered.Attempts != 1 || filtered.Retries != 0 || filtered.LimitEvents != 1 {
		t.Fatalf("provider: %+v", filtered)
	}
	if len(out.Executions) != 2 {
		t.Fatalf("groups=%+v", out.Executions)
	} // old run notices may create a group only when they have evidence
	if len(out.Categories) != 2 || out.Categories[0].Created != 2 || out.Categories[1].Completed != 1 {
		t.Fatalf("categories=%+v", out.Categories)
	}
	other := aggregateAnalytics(data, nil, AnalyticsFilter{ProjectID: "other"}, start, end)
	if other.Created != 0 || other.Attempts != 0 || len(other.Providers) != 0 {
		t.Fatalf("cross-project leak: %+v", other)
	}
}

func TestAnalyticsMissingAndBoundaryData(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	data := store.AnalyticsData{Truncated: true, Tasks: []domain.Task{{ID: "missing", ProjectID: "p", CreatedAt: start}}, Runs: []domain.Run{{ID: "r", ProjectID: "p", TaskID: "missing", AgentID: "codex", CreatedAt: end.Add(time.Millisecond)}}}
	data.Events = []domain.Event{{Type: domain.EventTaskUpdated, TaskID: "missing", ProjectID: "p", CreatedAt: start, Payload: json.RawMessage(`{"state":"done"}`)}}
	out := aggregateAnalytics(data, nil, AnalyticsFilter{}, start, end)
	if out.Created != 1 || out.Completed != 0 || out.MissingTaskHistory != 1 || out.Attempts != 0 || out.AverageTaskSeconds != nil || out.MedianTaskSeconds != nil || out.InputTokens != nil || out.ActualCostUSD != nil || !out.Truncated {
		t.Fatalf("missing data: %+v", out)
	}
	if len(out.Days) != 8 {
		t.Fatalf("partial UTC days=%d", len(out.Days))
	}
	boundary := store.AnalyticsData{Tasks: []domain.Task{{ID: "at-end", ProjectID: "p", CreatedAt: end, State: domain.TaskDone}}, Events: []domain.Event{
		{Seq: 1, Type: domain.EventTaskCreated, TaskID: "at-end", ProjectID: "p", CreatedAt: end, Payload: json.RawMessage(`{"state":"backlog"}`)},
		{Seq: 2, Type: domain.EventTaskUpdated, TaskID: "at-end", ProjectID: "p", CreatedAt: end, Payload: json.RawMessage(`{"state":"done"}`)},
	}}
	boundaryReport := aggregateAnalytics(boundary, nil, AnalyticsFilter{}, start, end)
	if boundaryReport.Created != 1 || boundaryReport.Completed != 1 || boundaryReport.Days[len(boundaryReport.Days)-1].Completed != 1 {
		t.Fatalf("snapshot millisecond excluded: %+v", boundaryReport)
	}
	for _, p := range []string{"fake", "opencode", "claude-code"} {
		if limitNotice(p, "The turn failed: usage limit reached") {
			t.Fatalf("inferred unsupported limit for %s", p)
		}
	}
	for _, text := range []string{"rate limit exceeded in user instructions", "The turn failed: network disconnected", "Rate limit: allowed_warning"} {
		if limitNotice("codex", text) || limitNotice("claude-code", text) {
			t.Fatalf("false limit %q", text)
		}
	}
}

func TestAnalyticsReadOnlyProjectionAndScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	f.deps.Now = func() time.Time { return now.Add(time.Minute) }
	p, _ := f.projects.Register(ctx, "/repos/analytics", "")
	task, e := f.tasks.Create(ctx, p.ID, "private title", "sensitive description")
	if e != nil {
		t.Fatal(e)
	}
	var before, after int64
	var data store.AnalyticsData
	e = f.deps.Store.View(ctx, func(tx store.Tx) error {
		before, _ = tx.Events().LatestSeq(ctx)
		var e error
		data, e = tx.Analytics().Read(ctx, p.ID, now.Add(-time.Hour), now.Add(time.Minute), 1)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(data.Tasks) != 1 || data.Tasks[0].Title != "" || data.Tasks[0].Description != "" {
		t.Fatalf("projection %+v", data)
	}
	report, e := (&Analytics{Deps: f.deps}).Report(ctx, AnalyticsFilter{ProjectID: p.ID, Days: 7})
	if e != nil || report.Created != 1 {
		t.Fatalf("report=%+v err=%v", report, e)
	}
	_ = f.deps.Store.View(ctx, func(tx store.Tx) error {
		after, _ = tx.Events().LatestSeq(ctx)
		fresh, _ := tx.Tasks().Get(ctx, task.ID)
		if !reflect.DeepEqual(fresh, task) {
			t.Fatal("analytics changed task")
		}
		return nil
	})
	if before != after {
		t.Fatal("analytics appended events")
	}
	if _, e = (&Analytics{Deps: f.deps}).Report(ctx, AnalyticsFilter{ProjectID: "missing", Days: 7}); e == nil {
		t.Fatal("missing project accepted")
	}
}
