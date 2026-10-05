package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/runnerwire"
)

// PoC-1: a second question from an already-blocked remote run wedges the whole runner sync.
func TestSyncSecondQuestionWhileBlockedIsPolicyAnswered(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	selected, resolved, e := rt.svc.Route(ctx, task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"})
	if e != nil {
		t.Fatal(e)
	}
	job := &runnerwire.Job{TargetBranch: "main"}
	run, e := rt.f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: resolved.Agent, Prompt: "work",
		Policy:   domain.ExecutionPolicy{Interaction: domain.InteractionAutonomousStopIfBlocked},
		RunnerID: selected.ID, Remote: true, Claim: rt.svc.Claim(selected, job)})
	if e != nil {
		t.Fatal(e)
	}
	q := func(seq int64, ref string) runnerwire.Observation {
		return runnerwire.Observation{Seq: seq, Kind: "event", Event: &agent.Event{Kind: agent.KindQuestion,
			Question: &agent.Question{Ref: ref, Kind: domain.QuestionClarification, Prompt: "which db?"}}}
	}
	// started, q1 => policy blocks the run
	if _, e := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run), q(2, "q1")}}}); e != nil {
		t.Fatalf("first sync: %v", e)
	}
	got, _ := rt.f.runs.Get(ctx, run.ID)
	t.Logf("state after q1: %s", got.State)
	// the agent, told to stop, asks one more question before ending its turn
	obs := []runnerwire.Observation{q(3, "q2")}
	for i := 0; i < 3; i++ {
		_, e = rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: obs}})
		t.Logf("retry %d => err=%v", i, e)
	}
	if e != nil {
		t.Fatalf("second question wedged sync: %v", e)
	}
	// even a bare heartbeat from the same runner still works, but ack never advances:
	reply, e2 := rt.sync(nil)
	t.Logf("heartbeat err=%v acks=%v", e2, reply.Acks)
	// now try to deliver an unrelated, valid report for another run: blocked behind the poison
	task2 := rt.task(t)
	run2 := rt.start(t, task2)
	_, e3 := rt.sync([]runnerwire.Report{
		{RunID: run.ID, Observations: obs},
		{RunID: run2.ID, Observations: []runnerwire.Observation{rt.started(run2)}},
	})
	r2, _ := rt.f.runs.Get(ctx, run2.ID)
	t.Logf("other run report err=%v; other run state=%s", e3, r2.State)
	if e3 != nil || r2.State != domain.RunRunning || reply.Acks[run.ID] != 3 {
		t.Fatalf("healthy sync did not advance: %+v %v", reply, e3)
	}

}

// PoC-8: event parity between a local and a remote run that gets blocked.
func TestRemoteBlockedRunEmitsLifecycleEvents(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	selected, resolved, _ := rt.svc.Route(ctx, task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"})
	run, e := rt.f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: resolved.Agent, Prompt: "work",
		Policy:   domain.ExecutionPolicy{Interaction: domain.InteractionAutonomousStopIfBlocked},
		RunnerID: selected.ID, Remote: true, Claim: rt.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
	if e != nil {
		t.Fatal(e)
	}
	say := agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: "stuck\nDEVBOARD_BLOCKED {\"summary\":\"need the prod DB password\"}"}
	obs := []runnerwire.Observation{rt.started(run),
		{Seq: 2, Kind: "event", Event: &say},
		{Seq: 3, Kind: "event", Event: &agent.Event{Kind: agent.KindTurnEnd}}}
	if _, e := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: obs}}); e != nil {
		t.Fatal(e)
	}
	got, _ := rt.f.runs.Get(ctx, run.ID)
	evs, _ := rt.f.runs.Events(ctx, run.ID, 0, 100)
	types := map[domain.EventType]int{}
	for _, ev := range evs {
		types[ev.Type]++
	}
	if types[domain.EventAgentStarted] == 0 || types[domain.EventAgentBlocked] == 0 {
		t.Fatalf("missing lifecycle events: %s %v", got.State, types)
	}
}

// PoC-10: every usage snapshot / session-ref / state change re-logs the whole Run (prompt + handoff) to the event log.
func TestUsageEventsDoNotRelogPrompt(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	selected, resolved, _ := rt.svc.Route(ctx, task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"})
	prompt := make([]byte, 30<<10)
	for i := range prompt {
		prompt[i] = 'p'
	}
	run, e := rt.f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: resolved.Agent, Prompt: string(prompt),
		RunnerID: selected.ID, Remote: true, Claim: rt.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
	if e != nil {
		t.Fatal(e)
	}
	obs := []runnerwire.Observation{rt.started(run)}
	for i := 0; i < 100; i++ {
		in := int64(1000 + i*500)
		obs = append(obs, runnerwire.Observation{Seq: int64(2 + i), Kind: "event", Event: &agent.Event{Kind: agent.KindUsage, Usage: &domain.Usage{InputTokens: &in, CostKind: "usage_only"}}})
	}
	if _, e := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: obs}}); e != nil {
		t.Fatal(e)
	}
	evs, _ := rt.f.runs.Events(ctx, run.ID, 0, 1000)
	total := 0
	for _, ev := range evs {
		total += len(ev.Payload)
	}
	if total > 300000 {
		t.Fatalf("usage event payload grew to %d bytes", total)
	}
}

func TestPermanentReportIsIsolatedQuarantinedAndHeartbeatAdvances(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	bad := rt.start(t, rt.task(t))
	good := rt.start(t, rt.task(t))
	invalid := rt.started(bad)
	invalid.Branch = "someone-elses-branch"
	reply, err := rt.sync([]runnerwire.Report{{RunID: bad.ID, Observations: []runnerwire.Observation{invalid}}, {RunID: good.ID, Observations: []runnerwire.Observation{rt.started(good)}}})
	if err != nil || reply.Rejected[bad.ID] == "" || reply.Acks[bad.ID] != 1 || reply.Acks[good.ID] != 1 {
		t.Fatalf("batch: %+v %v", reply, err)
	}
	healthy, _ := rt.f.runs.Get(ctx, good.ID)
	if healthy.State != domain.RunRunning {
		t.Fatal(healthy.State)
	}
	rt.now = rt.now.Add(10 * time.Second)
	reply, err = rt.sync([]runnerwire.Report{{RunID: bad.ID, Observations: []runnerwire.Observation{invalid}}})
	if err != nil || reply.Rejected[bad.ID] == "" {
		t.Fatalf("persistent rejection: %+v %v", reply, err)
	}
	runners, err := rt.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runners {
		if r.ID == rt.runner.ID && (!r.Online || !r.LastSeenAt.Equal(rt.now)) {
			t.Fatalf("heartbeat rolled back: %+v", r)
		}
	}
	result := agent.Result{State: domain.RunFailed, Reason: "owner resolved on runner"}
	_, err = rt.sync([]runnerwire.Report{{RunID: bad.ID, Observations: []runnerwire.Observation{{Seq: 2, Kind: "ended", Result: &result}}}})
	if err != nil {
		t.Fatal(err)
	}
	stopped, _ := rt.f.runs.Get(ctx, bad.ID)
	if stopped.State.Active() {
		t.Fatal("explicit resolution did not release work")
	}
}

func TestRevocationFencesIdentityBeforeReleasingOwnership(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	r := rt.start(t, rt.task(t))
	if err := rt.svc.Revoke(ctx, rt.runner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.sync(nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoked identity authenticated: %v", err)
	}
	if err := rt.svc.ReleaseRevoked(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := rt.f.runs.Get(ctx, r.ID)
	if !got.State.Active() {
		t.Fatal("ownership released before lease expired")
	}
	rt.now = rt.now.Add(runnerwire.Lease + runnerwire.OnlineWindow + time.Second)
	if err := rt.svc.ReleaseRevoked(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = rt.f.runs.Get(ctx, r.ID)
	if got.State.Active() || !strings.Contains(got.Reason, "unverified") {
		t.Fatalf("unsafe recovery: %+v", got)
	}
}

func TestOpaqueImportIsIdempotentAndRetainsEdits(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	in := NewTask{ProjectID: rt.project.ID, Title: strings.Repeat("界", 200), Description: strings.Repeat("界", 20000), SourceRef: "https://source.test/item/1", WorkBranch: "ticket-1", BaseBranch: "main"}
	first, err := rt.f.tasks.CreateTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	title := "user edited task"
	edited, err := rt.f.tasks.Update(ctx, first.ID, TaskPatch{Version: first.Version, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := rt.f.tasks.CreateTask(ctx, in)
		if err != nil || again.ID != first.ID || again.Title != edited.Title || again.WorkBranch != "ticket-1" {
			t.Fatalf("duplicate or overwritten association: %+v %v", again, err)
		}
	}
}

func TestUsageSnapshotsPreserveKnownEvidence(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	run := rt.start(t, rt.task(t))
	n := int64(120)
	cost := .5
	if _, err := rt.f.runs.SetUsage(ctx, run.ID, domain.Usage{InputTokens: &n, CostUSD: &cost, CostKind: "actual_api", Source: "meter"}); err != nil {
		t.Fatal(err)
	}
	output := int64(10)
	if _, err := rt.f.runs.SetUsage(ctx, run.ID, domain.Usage{OutputTokens: &output, CostKind: "usage_only", Source: "partial"}); err != nil {
		t.Fatal(err)
	}
	got, _ := rt.f.runs.Get(ctx, run.ID)
	if got.Usage.InputTokens == nil || *got.Usage.InputTokens != 120 || got.Usage.CostUSD == nil || *got.Usage.CostUSD != cost || got.Usage.Source != "meter" {
		t.Fatalf("lost evidence: %+v", got.Usage)
	}
}

func TestInvalidTerminalTransitionIsQuarantinedPerReport(t *testing.T) {
	rt := newRunnerTest(t)
	bad := rt.start(t, rt.task(t))
	good := rt.start(t, rt.task(t))
	// Completing without a started observation violates the state machine.
	result := agent.Result{State: domain.RunCompleted}
	reply, err := rt.sync([]runnerwire.Report{
		{RunID: bad.ID, Observations: []runnerwire.Observation{{Seq: 1, Kind: "ended", Result: &result}}},
		{RunID: good.ID, Observations: []runnerwire.Observation{rt.started(good)}},
	})
	if err != nil || reply.Rejected[bad.ID] == "" || reply.Acks[good.ID] != 1 {
		t.Fatalf("invalid transition rolled back healthy report: %+v %v", reply, err)
	}
	got, _ := rt.f.runs.Get(context.Background(), good.ID)
	if got.State != domain.RunRunning {
		t.Fatal(got.State)
	}
}
