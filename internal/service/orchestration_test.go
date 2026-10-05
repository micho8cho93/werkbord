package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
)

func schedulingFixture(t *testing.T) (*fixture, *Scheduler, string, time.Time) {
	t.Helper()
	f := newFixture(t)
	p, e := f.projects.Register(context.Background(), "/repos/schedule", "schedule")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	s := &Scheduler{Deps: f.deps}
	s.Now = func() time.Time { return now }
	f.tasks.Now = s.Now
	f.runs.Now = s.Now
	return f, s, p.ID, now
}
func arm(t *testing.T, f *fixture, pid, title string, o domain.Orchestration) *domain.Task {
	t.Helper()
	o.Enabled = true
	task, e := f.tasks.CreateTask(context.Background(), NewTask{ProjectID: pid, Title: title, Orchestration: o})
	if e != nil {
		t.Fatal(e)
	}
	return task
}
func decisionFor(t *testing.T, s *Scheduler, pid, id string) domain.SchedulingDecision {
	t.Helper()
	plan, e := s.Plan(context.Background(), pid)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range plan {
		if d.TaskID == id {
			return d
		}
	}
	t.Fatal("missing decision")
	return domain.SchedulingDecision{}
}
func TestSchedulingTimesTimezoneAndDeadline(t *testing.T) {
	f, s, pid, now := schedulingFixture(t)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	a := arm(t, f, pid, "later", domain.Orchestration{ScheduledAt: &future, Timezone: "Europe/Madrid"})
	b := arm(t, f, pid, "not before", domain.Orchestration{NotBefore: &future})
	c := arm(t, f, pid, "deadline", domain.Orchestration{Deadline: &past})
	for _, tt := range []struct {
		task  *domain.Task
		state string
	}{{a, "waiting_schedule"}, {b, "waiting_schedule"}, {c, "blocked"}} {
		if d := decisionFor(t, s, pid, tt.task.ID); d.State != tt.state {
			t.Fatalf("%s: %+v", tt.task.Title, d)
		}
	}
	s.Now = func() time.Time { return future }
	if e := s.CheckStart(context.Background(), a.ID, a.Orchestration.Key); e != nil {
		t.Fatal(e)
	}
}
func TestMissedSchedulePolicies(t *testing.T) {
	f, s, pid, now := schedulingFixture(t)
	at := now.Add(-time.Hour)
	late := arm(t, f, pid, "run late", domain.Orchestration{ScheduledAt: &at, MissedPolicy: "run_late"})
	skip := arm(t, f, pid, "skip", domain.Orchestration{ScheduledAt: &at, MissedPolicy: "skip", GraceSeconds: 300})
	if d := decisionFor(t, s, pid, late.ID); d.State != "runnable" {
		t.Fatal(d)
	}
	d := decisionFor(t, s, pid, skip.ID)
	if d.State != "blocked" || !strings.Contains(d.Reason, "missed") {
		t.Fatal(d)
	}
	if e := s.RecordDispatchError(context.Background(), skip.ID, skip.Orchestration.Key, d.Reason, true); e != nil {
		t.Fatal(e)
	}
	changed, e := f.tasks.Get(context.Background(), skip.ID)
	if e != nil || !changed.Orchestration.Missed {
		t.Fatalf("%+v %v", changed, e)
	}
	changed, e = f.tasks.Update(context.Background(), skip.ID, TaskPatch{Version: changed.Version, Orchestration: &domain.Orchestration{Enabled: true}})
	if e != nil || !changed.Orchestration.Missed || changed.Orchestration.Key != skip.Orchestration.Key {
		t.Fatalf("ordinary edit rearmed work: %+v %v", changed, e)
	}
	changed, e = f.tasks.Update(context.Background(), skip.ID, TaskPatch{Version: changed.Version, Orchestration: &domain.Orchestration{Enabled: true, Rearm: true}})
	if e != nil || changed.Orchestration.Missed || changed.Orchestration.Key == skip.Orchestration.Key {
		t.Fatalf("rearm: %+v %v", changed, e)
	}
}
func TestDependencyGatesAndCycles(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	a, err := f.tasks.Create(ctx, pid, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	b := arm(t, f, pid, "B", domain.Orchestration{Dependencies: []string{a.ID}})
	if d := decisionFor(t, s, pid, b.ID); d.State != "waiting_dependency" {
		t.Fatal(d)
	}
	if _, e := f.tasks.Update(ctx, a.ID, TaskPatch{Version: a.Version, Orchestration: &domain.Orchestration{Dependencies: []string{b.ID}}}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatalf("cycle: %v", e)
	}
	other, e := f.projects.Register(ctx, "/repos/other", "")
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := f.tasks.Create(ctx, other.ID, "foreign", "")
	if e != nil {
		t.Fatal(e)
	}
	for _, deps := range [][]string{{a.ID}, {foreign.ID}, {b.ID, b.ID}} {
		_, e := f.tasks.Update(ctx, a.ID, TaskPatch{Version: a.Version, Orchestration: &domain.Orchestration{Dependencies: deps}})
		if !errors.Is(e, domain.ErrInvalid) {
			t.Fatalf("dependencies %v: %v", deps, e)
		}
	}
	run, e := f.runs.Create(ctx, NewRun{TaskID: a.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.MarkStarted(ctx, run.ID, Started{})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.Block(ctx, run.ID, domain.Blocker{Summary: "needs a decision", Source: domain.BlockerReport})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "blocked" || !strings.Contains(d.Reason, "A") {
		t.Fatal(d)
	}
	_, e = f.runs.End(ctx, run.ID, Ended{State: domain.RunFailed, Reason: "tests failed"})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "blocked" || !strings.Contains(d.Reason, "tests failed") {
		t.Fatal(d)
	}
	retry, e := f.runs.Create(ctx, NewRun{TaskID: a.ID, AgentID: "fake", Prompt: "retry"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.MarkStarted(ctx, retry.ID, Started{})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.End(ctx, retry.ID, Ended{State: domain.RunStopped, Reason: "cancelled"})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "blocked" || !strings.Contains(d.Reason, "stopped") {
		t.Fatal(d)
	}
	retry, e = f.runs.Create(ctx, NewRun{TaskID: a.ID, AgentID: "fake", Prompt: "retry"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.MarkStarted(ctx, retry.ID, Started{})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.runs.End(ctx, retry.ID, Ended{State: domain.RunCompleted})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "runnable" {
		t.Fatal(d)
	}
}
func TestOrderingPriorityAndCapacity(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	one, two := 1, 2
	low := arm(t, f, pid, "ordered low", domain.Orchestration{ExecutionOrder: &one})
	_, e := f.tasks.Update(ctx, low.ID, TaskPatch{Version: low.Version, Execution: &domain.ExecutionConfig{Priority: domain.PriorityLow}})
	if e != nil {
		t.Fatal(e)
	}
	high := arm(t, f, pid, "ordered high", domain.Orchestration{ExecutionOrder: &two})
	_, e = f.tasks.Update(ctx, high.ID, TaskPatch{Version: high.Version, Execution: &domain.ExecutionConfig{Priority: domain.PriorityHigh}})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, low.ID); d.State != "runnable" {
		t.Fatal(d)
	}
	if d := decisionFor(t, s, pid, high.ID); d.State != "queued" {
		t.Fatal(d)
	}
	run, e := f.runs.Create(ctx, NewRun{TaskID: low.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, high.ID); d.State != "waiting_capacity" {
		t.Fatal(d)
	}
	if e := s.SetSettings(ctx, pid, OrchestrationSettings{ConcurrencyLimit: 2}); e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, high.ID); d.State != "potentially_conflicting" {
		t.Fatal(d)
	}
	if _, e = f.runs.End(ctx, run.ID, Ended{State: domain.RunStopped}); e != nil {
		t.Fatal(e)
	}
	if e := s.SetSettings(ctx, pid, OrchestrationSettings{ConcurrencyLimit: 0}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
}
func TestPriorityInheritedAndDeterministicTie(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	low := arm(t, f, pid, "low", domain.Orchestration{})
	_, e := f.tasks.Update(ctx, low.ID, TaskPatch{Version: low.Version, Execution: &domain.ExecutionConfig{Priority: domain.PriorityLow}})
	if e != nil {
		t.Fatal(e)
	}
	high := arm(t, f, pid, "high", domain.Orchestration{})
	_, e = f.tasks.Update(ctx, high.ID, TaskPatch{Version: high.Version, Execution: &domain.ExecutionConfig{Priority: domain.PriorityHigh}})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if d := decisionFor(t, s, pid, high.ID); d.State != "runnable" {
			t.Fatal(d)
		}
	}
}
func TestScopeHintsAndHealth(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	if e := s.SetSettings(ctx, pid, OrchestrationSettings{ConcurrencyLimit: 2}); e != nil {
		t.Fatal(e)
	}
	a := arm(t, f, pid, "api", domain.Orchestration{ExpectedPaths: []string{"internal/api/"}})
	b := arm(t, f, pid, "frontend", domain.Orchestration{ExpectedPaths: []string{"web/"}})
	if d := decisionFor(t, s, pid, a.ID); d.State != "runnable" {
		t.Fatal(d)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "runnable" {
		t.Fatal(d)
	}
	r, e := f.runs.Create(ctx, NewRun{TaskID: a.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	_ = r
	if d := decisionFor(t, s, pid, b.ID); d.State != "runnable" {
		t.Fatal(d)
	}
	b, e = f.tasks.Update(ctx, b.ID, TaskPatch{Version: b.Version, Orchestration: &domain.Orchestration{Enabled: true, ExpectedPaths: []string{"internal/api/handlers.go"}}})
	if e != nil {
		t.Fatal(e)
	}
	if d := decisionFor(t, s, pid, b.ID); d.State != "potentially_conflicting" {
		t.Fatal(d)
	}
	if !pathsOverlap([]string{"a/b"}, []string{"a/b/file"}) || pathsOverlap([]string{"a/b"}, []string{"a/bc"}) {
		t.Fatal("path boundary matching")
	}
}
func TestAtomicScheduleClaimAndHandoff(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	task := arm(t, f, pid, "claim", domain.Orchestration{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := []*domain.Run{}
	errs := []error{}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work", ScheduleKey: task.Orchestration.Key})
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				success = append(success, r)
			} else {
				errs = append(errs, e)
			}
		}()
	}
	wg.Wait()
	if len(success) != 1 {
		t.Fatalf("claims=%d errors=%v", len(success), errs)
	}
	for _, e := range errs {
		if !errors.Is(e, domain.ErrConflict) {
			t.Fatal(e)
		}
	}
	r := success[0]
	claimed, e := f.tasks.Get(ctx, task.ID)
	if e != nil || claimed.Orchestration.RunID != r.ID || claimed.Orchestration.DispatchedAt == nil {
		t.Fatalf("%+v %v", claimed, e)
	}
	if _, e := f.runs.MarkStarted(ctx, r.ID, Started{}); e != nil {
		t.Fatal(e)
	}
	if e := f.runs.AppendOutput(ctx, r.ID, []OutputItem{{Stream: domain.StreamAssistant, Text: "Changed parser. Tests: go test ./... passed."}}); e != nil {
		t.Fatal(e)
	}
	ended, e := f.runs.End(ctx, r.ID, Ended{State: domain.RunCompleted})
	if e != nil {
		t.Fatal(e)
	}
	if ended.Handoff == nil || !strings.Contains(ended.Handoff.Summary, "Changed parser") || len(ended.Handoff.Tests) != 0 {
		t.Fatalf("handoff %+v", ended.Handoff)
	}
	if d := decisionFor(t, s, pid, task.ID); d.State != "queued" || !strings.Contains(d.Reason, "already dispatched") {
		t.Fatal(d)
	}
	// A different service instance (as after restart) cannot reclaim it.
	restarted := &Runs{Deps: f.deps}
	if _, e := restarted.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "duplicate", ScheduleKey: task.Orchestration.Key}); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	h := &Handoffs{Deps: f.deps}
	draft := *ended.Handoff
	draft.Tests = []string{"go test ./... passed"}
	draft.Decisions = []string{"Preserve parser API"}
	draft.NextAction = "Review parser"
	saved, e := h.Save(ctx, r.ID, ended.Version, draft)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := h.Save(ctx, r.ID, ended.Version, draft); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	context, e := h.Context(ctx, r.ID, "review", "Review boundary cases only")
	if e != nil || !strings.Contains(context, "Review boundary cases only") || !strings.Contains(context, "Preserve parser API") {
		t.Fatalf("%s %v", context, e)
	}
	continuation, e := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "another-agent", Model: "model-b", Prompt: context, ParentRunID: r.ID, Purpose: "review"})
	if e != nil {
		t.Fatal(e)
	}
	if continuation.ParentRunID != saved.ID || continuation.Model != "model-b" {
		t.Fatal(continuation)
	}
	history, e := f.runs.ListByTask(ctx, task.ID)
	if e != nil || len(history) != 2 {
		t.Fatalf("history %v %v", history, e)
	}
}
func TestCheckStartRechecksDependencyAndNotBefore(t *testing.T) {
	f, s, pid, now := schedulingFixture(t)
	ctx := context.Background()
	future := now.Add(time.Hour)
	dep := arm(t, f, pid, "dep", domain.Orchestration{})
	task := arm(t, f, pid, "work", domain.Orchestration{Dependencies: []string{dep.ID}})
	if e := s.CheckStart(ctx, task.ID, ""); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if _, e := f.tasks.Update(ctx, dep.ID, TaskPatch{Version: dep.Version, State: ptrState(domain.TaskDone)}); e != nil {
		t.Fatal(e)
	}
	task, e := f.tasks.Update(ctx, task.ID, TaskPatch{Version: task.Version, Orchestration: &domain.Orchestration{Enabled: true, NotBefore: &future}})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.CheckStart(ctx, task.ID, ""); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if e := s.CheckStart(ctx, task.ID, "old-key"); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
}
func ptrState(s domain.TaskState) *domain.TaskState { return &s }
func TestHandoffParentScopeAndActiveRefused(t *testing.T) {
	f, _, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	a := arm(t, f, pid, "a", domain.Orchestration{})
	b := arm(t, f, pid, "b", domain.Orchestration{})
	r, e := f.runs.Create(ctx, NewRun{TaskID: a.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	h := &Handoffs{Deps: f.deps}
	if _, e := h.Generate(ctx, r.ID); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if _, e := f.runs.Create(ctx, NewRun{TaskID: b.ID, AgentID: "fake", Prompt: "work", ParentRunID: r.ID}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := f.runs.End(ctx, r.ID, Ended{State: domain.RunStopped}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.runs.Create(ctx, NewRun{TaskID: b.ID, AgentID: "fake", Prompt: "work", ParentRunID: r.ID}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
}

func TestHandoffStreamingAndOriginalObjective(t *testing.T) {
	f, _, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	task := arm(t, f, pid, "Original objective", domain.Orchestration{})
	r, e := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	title := "Edited future objective"
	if _, e := f.tasks.Update(ctx, task.ID, TaskPatch{Version: task.Version, Title: &title}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.runs.MarkStarted(ctx, r.ID, Started{}); e != nil {
		t.Fatal(e)
	}
	items := []OutputItem{{Stream: domain.StreamAssistant, Text: "Old conversation text"}, {Stream: domain.StreamAssistant, Text: `<devboard-handoff>{"summary":"Changed`}, {Stream: domain.StreamTool, Text: "ignored tool output"}, {Stream: domain.StreamAssistant, Text: ` parser","tests":["go test passed"],"results":"OK"}</devboard-handoff>`}}
	if e := f.runs.AppendOutput(ctx, r.ID, items); e != nil {
		t.Fatal(e)
	}
	r, e = f.runs.End(ctx, r.ID, Ended{State: domain.RunCompleted})
	if e != nil {
		t.Fatal(e)
	}
	h := r.Handoff
	if h == nil || !strings.Contains(h.Summary, "Changed parser") || len(h.Tests) != 1 || !strings.HasPrefix(h.Objective, "Original objective") || h.Decisions == nil || h.FilesChanged == nil {
		t.Fatalf("%+v", h)
	}
}

func TestControlCenterIncludesFailedScheduledSetup(t *testing.T) {
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	task := arm(t, f, pid, "setup failure", domain.Orchestration{})
	r, e := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work", ScheduleKey: task.Orchestration.Key})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.runs.End(ctx, r.ID, Ended{State: domain.RunFailed, Reason: "agent unavailable"}); e != nil {
		t.Fatal(e)
	}
	control := &ControlCenter{Deps: s.Deps, Scheduler: s}
	overview, e := control.Overview(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(overview.Failed) != 1 || overview.Failed[0].Run.ID != r.ID || overview.Projects[0].Failed != 1 {
		t.Fatalf("%+v", overview)
	}
}
