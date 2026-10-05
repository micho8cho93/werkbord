package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
	"devboard/internal/runnerwire"
	"devboard/internal/store"
)

type runnerTest struct {
	f        *fixture
	svc      *Runners
	now      time.Time
	project  *ProjectDetail
	key      ed25519.PrivateKey
	runner   *domain.Runner
	sequence int64
}

func newRunnerTest(t *testing.T) *runnerTest {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	p, e := f.projects.Register(ctx, "/repos/multi", "Multi")
	if e != nil {
		t.Fatal(e)
	}
	rt := &runnerTest{f: f, now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), project: p}
	deps := f.deps
	deps.Now = func() time.Time { return rt.now }
	f.runs.Deps = deps
	rt.svc = &Runners{Deps: deps, Runs: f.runs}
	public, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	rt.key = key
	pair, e := rt.svc.Pair(ctx, "http://controller.test:7420", []string{p.ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	_, secret, e := runnerwire.DecodeCode(pair.Code)
	if e != nil {
		t.Fatal(e)
	}
	rt.runner, e = rt.svc.Join(ctx, runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(public), Name: "Desktop", OS: "linux", Arch: "amd64"})
	if e != nil {
		t.Fatal(e)
	}
	rt.runner.Automatic = true
	rt.runner.Capacity = 2
	rt.runner, e = rt.svc.Manage(ctx, rt.runner.ID, *rt.runner, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := rt.sync(nil); e != nil {
		t.Fatal(e)
	}
	return rt
}
func (rt *runnerTest) caps() domain.RunnerCapabilities {
	return domain.RunnerCapabilities{CPU: 8, Agents: []domain.Agent{{ID: "codex", Available: true}}, Repositories: []string{rt.project.ID}, Options: []domain.AgentOptions{{AgentID: "codex", CustomModels: true}}}
}
func (rt *runnerTest) sync(reports []runnerwire.Report) (runnerwire.SyncReply, error) {
	rt.sequence++
	in := runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: rt.runner.ID, Sequence: rt.sequence, At: rt.now, Capabilities: rt.caps(), Reports: reports}
	body, _ := json.Marshal(in)
	return rt.svc.Sync(context.Background(), body, runnerwire.Signature(rt.key, body))
}
func (rt *runnerTest) task(t *testing.T) *domain.Task {
	t.Helper()
	task, e := rt.f.tasks.Create(context.Background(), rt.project.ID, "Build", "Implement")
	if e != nil {
		t.Fatal(e)
	}
	return task
}
func (rt *runnerTest) start(t *testing.T, task *domain.Task) *domain.Run {
	t.Helper()
	selected, resolved, e := rt.svc.Route(context.Background(), task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"})
	if e != nil {
		t.Fatal(e)
	}
	job := &runnerwire.Job{TargetBranch: "main"}
	run, e := rt.f.runs.Create(context.Background(), NewRun{TaskID: task.ID, AgentID: resolved.Agent, Prompt: "work", RunnerID: selected.ID, Remote: true, Claim: rt.svc.Claim(selected, job)})
	if e != nil {
		t.Fatal(e)
	}
	return run
}
func (rt *runnerTest) started(run *domain.Run) runnerwire.Observation {
	return runnerwire.Observation{Seq: 1, Kind: "started", Branch: "devboard/" + run.RunnerID + "/" + run.ID, BaseCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

func TestWorkspaceReportsOnlyReconcileOwnedTerminalGitMetadata(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	run := rt.start(t, rt.task(t))
	clean := false
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	workspace := runnerwire.Observation{Seq: 1, Kind: "workspace", HeadCommit: commit, Uncommitted: &clean}
	poisoned := rt.start(t, rt.task(t))
	if reply, err := rt.sync([]runnerwire.Report{{RunID: poisoned.ID, Observations: []runnerwire.Observation{workspace}}}); err != nil || reply.Rejected[poisoned.ID] == "" {
		t.Fatalf("invalid active workspace not isolated: %+v %v", reply, err)
	}
	result := agent.Result{State: domain.RunCompleted}
	if _, err := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run), {Seq: 2, Kind: "ended", Result: &result}}}}); err != nil {
		t.Fatal(err)
	}
	ended, _ := rt.f.runs.Get(ctx, run.ID)
	workspace.Seq = 3
	// A workspace report has no authority to change completion or economics.
	workspace.Result = &agent.Result{State: domain.RunFailed}
	count := int64(999)
	workspace.Usage = &domain.Usage{InputTokens: &count}
	report := []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{workspace}}}
	if _, err := rt.sync(report); err != nil {
		t.Fatal(err)
	}
	reconciled, _ := rt.f.runs.Get(ctx, run.ID)
	if reconciled.HeadCommit != commit || reconciled.Uncommitted == nil || *reconciled.Uncommitted || reconciled.State != ended.State || !reconciled.EndedAt.Equal(*ended.EndedAt) || reconciled.Usage.InputTokens != nil {
		t.Fatalf("workspace altered execution: %+v", reconciled)
	}
	version := reconciled.Version
	if _, err := rt.sync(report); err != nil {
		t.Fatal(err)
	}
	duplicate, _ := rt.f.runs.Get(ctx, run.ID)
	if duplicate.Version != version {
		t.Fatal("duplicate workspace report reapplied")
	}
	workspace.Seq, workspace.HeadCommit = 4, "--bad-revision"
	if reply, err := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{workspace}}}); err != nil || reply.Rejected[run.ID] == "" {
		t.Fatalf("unsafe commit admitted: %+v %v", reply, err)
	}
}

func TestPairingExpiresIsSingleUseAndDuplicateIdentity(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	pub := rt.key.Public().(ed25519.PublicKey)
	pair, e := rt.svc.Pair(ctx, "http://controller.test:7420", []string{rt.project.ID}, true)
	if e != nil {
		t.Fatal(e)
	}
	_, secret, _ := runnerwire.DecodeCode(pair.Code)
	in := runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(pub), Name: "Duplicate"}
	if _, e := rt.svc.Join(ctx, in); !errors.Is(e, domain.ErrDuplicate) {
		t.Fatalf("duplicate identity: %v", e)
	}
	_, another, _ := ed25519.GenerateKey(rand.Reader)
	in.PublicKey = base64.RawURLEncoding.EncodeToString(another.Public().(ed25519.PublicKey))
	rt.now = pair.ExpiresAt
	if _, e := rt.svc.Join(ctx, in); !errors.Is(e, domain.ErrInvalid) {
		t.Fatalf("expiry boundary: %v", e)
	}
	rt.now = rt.now.Add(time.Second)
	pair, e = rt.svc.Pair(ctx, "http://controller.test:7420", []string{rt.project.ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	_, in.Secret, _ = runnerwire.DecodeCode(pair.Code)
	if _, e := rt.svc.Join(ctx, in); e != nil {
		t.Fatal(e)
	}
	if recovered, e := rt.svc.Join(ctx, in); e != nil || recovered == nil {
		t.Fatalf("lost response recovery: %v", e)
	}
	reused := in
	reused.PublicKey = base64.RawURLEncoding.EncodeToString(pub)
	if _, e := rt.svc.Join(ctx, reused); !errors.Is(e, domain.ErrInvalid) {
		t.Fatalf("another identity reused code: %v", e)
	}
	var stored runnerwire.Pairing
	_ = rt.f.deps.Store.View(ctx, func(tx store.Tx) error {
		return tx.Settings().Get(ctx, "pair:"+runnerwire.SecretHash(in.Secret), &stored)
	})
	if stored.Code != "" || !stored.Used {
		t.Fatalf("secret was stored or not consumed: %+v", stored)
	}
}
func TestManualOfflineMissingAgentAndDeterministicAutomaticRouting(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	r, _, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex"})
	if e != nil || r.ID != rt.runner.ID {
		t.Fatalf("automatic: %v %+v", e, r)
	}
	if _, _, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "claude-code"}); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("missing agent: %v", e)
	}
	rt.now = rt.now.Add(runnerwire.OnlineWindow + time.Second)
	if _, _, e := rt.svc.Route(ctx, task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"}); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("offline: %v", e)
	}
	if _, e := rt.sync(nil); e != nil {
		t.Fatal(e)
	}
	rt.runner.Automatic = false
	_, e = rt.svc.Manage(ctx, rt.runner.ID, *rt.runner, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex"}); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("automatic opt-out: %v", e)
	}
	if r, _, e := rt.svc.Route(ctx, task, domain.Resolved{Runner: rt.runner.ID, Agent: "codex"}); e != nil || r.ID != rt.runner.ID {
		t.Fatalf("manual opted-out: %v", e)
	}
	// A tie goes to runner ID, independent of registration order.
	second := *rt.runner
	second.ID = "rnr_0000000000000000"
	second.PublicKey = ""
	second.Capabilities = rt.caps()
	second.Automatic = true
	second.LastSeenAt = rt.now
	_ = rt.f.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Runners().Save(ctx, &second) })
	rt.runner.Automatic = true
	_, _ = rt.svc.Manage(ctx, rt.runner.ID, *rt.runner, false)
	for i := 0; i < 3; i++ {
		r, _, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex"})
		if e != nil || r.ID != second.ID {
			t.Fatalf("unstable route: %+v %v", r, e)
		}
	}
}
func TestDisconnectedOwnershipSurvivesControllerRestartAndDuplicateReports(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	run := rt.start(t, task)
	if _, e := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run)}}}); e != nil {
		t.Fatal(e)
	}
	rt.now = rt.now.Add(runnerwire.Lease + time.Minute)
	if n, e := rt.f.runs.RecoverAfterRestart(ctx); e != nil || n != 0 {
		t.Fatalf("recovery ended remote run: %d %v", n, e)
	}
	if _, e := rt.f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "codex", Prompt: "duplicate"}); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("disconnected task duplicated: %v", e)
	}
	if _, e := rt.svc.Manage(ctx, rt.runner.ID, domain.Runner{}, true); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("active runner removed: %v", e)
	}
	_ = rt.syncMust(t, nil)
	out := agent.Event{Kind: agent.KindOutput, Stream: domain.StreamAssistant, Text: "done"}
	res := agent.Result{State: domain.RunCompleted, ExitCode: 0}
	report := []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run), {Seq: 2, Kind: "event", Event: &out}, {Seq: 3, Kind: "ended", Result: &res}}}}
	reply, e := rt.sync(report)
	if e != nil {
		t.Fatal(e)
	}
	finishedTask, e := rt.f.tasks.Get(ctx, task.ID)
	if e != nil || finishedTask.State != domain.TaskReview {
		t.Fatalf("remote completion not reviewable: %+v %v", finishedTask, e)
	}
	if reply.Acks[run.ID] != 3 {
		t.Fatalf("ack=%v", reply.Acks)
	}
	if _, e := rt.sync(report); e != nil {
		t.Fatal(e)
	}
	events, e := rt.f.runs.Events(ctx, run.ID, 0, 100)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, ev := range events {
		if ev.Type == domain.EventAgentOutput {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate output: %d", count)
	}
	if _, e := rt.svc.Manage(ctx, rt.runner.ID, domain.Runner{}, true); e != nil {
		t.Fatal(e)
	}
	if _, e := rt.sync(nil); !errors.Is(e, domain.ErrNotFound) {
		t.Fatalf("removed key still accepted: %v", e)
	}
}
func (rt *runnerTest) syncMust(t *testing.T, reports []runnerwire.Report) runnerwire.SyncReply {
	t.Helper()
	out, e := rt.sync(reports)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestCapacityClaimsAreAtomicAndSingleTaskNeverDuplicates(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	tasks := []*domain.Task{rt.task(t), rt.task(t), rt.task(t), rt.task(t)}
	selected, _, e := rt.svc.Route(ctx, tasks[0], domain.Resolved{Runner: rt.runner.ID, Agent: "codex"})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0
	for _, task := range tasks {
		wg.Add(1)
		go func(task *domain.Task) {
			defer wg.Done()
			_, e := rt.f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "codex", Prompt: "work", RunnerID: rt.runner.ID, Remote: true, Claim: rt.svc.Claim(selected, &runnerwire.Job{TargetBranch: "main"})})
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				count++
			} else if !errors.Is(e, domain.ErrConflict) {
				t.Errorf("claim: %v", e)
			}
		}(task)
	}
	wg.Wait()
	if count != 2 {
		t.Fatalf("capacity reservation count=%d", count)
	}
}
func TestRunnerCannotImpersonateReplayOrReportAnotherRun(t *testing.T) {
	rt := newRunnerTest(t)
	run := rt.start(t, rt.task(t))
	ctx := context.Background()
	rt.sequence++
	body, _ := json.Marshal(runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: rt.runner.ID, Sequence: rt.sequence, At: rt.now})
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, e := rt.svc.Sync(ctx, body, runnerwire.Signature(other, body)); !errors.Is(e, domain.ErrNotFound) {
		t.Fatalf("impersonation accepted: %v", e)
	}
	sig := runnerwire.Signature(rt.key, body)
	if _, e := rt.svc.Sync(ctx, body, sig); e != nil {
		t.Fatal(e)
	}
	if _, e := rt.svc.Sync(ctx, body, sig); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("replay accepted: %v", e)
	}
	foreign, e := rt.f.runs.Create(ctx, NewRun{TaskID: rt.task(t).ID, AgentID: "codex", Prompt: "local"})
	if e != nil {
		t.Fatal(e)
	}
	if reply, e := rt.sync([]runnerwire.Report{{RunID: foreign.ID, Observations: []runnerwire.Observation{rt.started(run)}}}); e != nil || reply.Rejected[foreign.ID] == "" {
		t.Fatalf("other run report accepted: %+v %v", reply, e)
	}
	// Only this report rolls back on a gap; the heartbeat is still accepted.
	bad := rt.started(run)
	bad.Seq = 3
	if reply, e := rt.sync([]runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run), bad}}}); e != nil || reply.Rejected[run.ID] == "" {
		t.Fatalf("gap accepted: %+v %v", reply, e)
	}
	got, _ := rt.f.runs.Get(ctx, run.ID)
	if got.State != domain.RunStarting {
		t.Fatalf("partial transaction: %s", got.State)
	}
}
func TestRemoteQuestionsCommandsUsageAndUnknownAccounting(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	run := rt.start(t, rt.task(t))
	rt.syncMust(t, []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{rt.started(run)}}})
	qev := agent.Event{Kind: agent.KindQuestion, Question: &agent.Question{Ref: "permission", Kind: domain.QuestionApproval, Prompt: "Run tests?", Options: []string{"Allow", "Deny"}}}
	rt.syncMust(t, []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{{Seq: 2, Kind: "event", Event: &qev}}}})
	qs, e := rt.f.runs.ListPendingQuestions(ctx)
	if e != nil || len(qs) != 1 {
		t.Fatalf("questions=%+v %v", qs, e)
	}
	if _, e := rt.svc.Answer(ctx, qs[0].ID, "Allow"); e != nil {
		t.Fatal(e)
	}
	reply := rt.syncMust(t, nil)
	if len(reply.Jobs) != 1 || len(reply.Jobs[0].Commands) != 1 {
		t.Fatalf("command=%+v", reply)
	}
	command := reply.Jobs[0].Commands[0]
	if command.Kind != "respond" || command.Ref != "permission" {
		t.Fatal(command)
	}
	rt.syncMust(t, []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{{Seq: 3, Kind: "command", CommandID: command.ID}}}})
	q, _ := rt.f.runs.GetQuestion(ctx, qs[0].ID)
	if q.DeliveredAt == nil {
		t.Fatal("delivery not acknowledged")
	}
	n := int64(42)
	cost := 1.25
	usage := domain.Usage{InputTokens: &n, CostUSD: &cost, CostKind: "estimated_api_equivalent", Source: "reported by simulated CLI"}
	ev := agent.Event{Kind: agent.KindUsage, Usage: &usage}
	rt.syncMust(t, []runnerwire.Report{{RunID: run.ID, Observations: []runnerwire.Observation{{Seq: 4, Kind: "event", Event: &ev}}}})
	rt.start(t, rt.task(t))
	var summaries []UsageSummary
	e = rt.f.deps.Store.View(ctx, func(tx store.Tx) error { var e error; summaries, e = RecentUsage(ctx, tx, rt.now); return e })
	if e != nil {
		t.Fatal(e)
	}
	if len(summaries) != 1 || summaries[0].Runs != 2 || summaries[0].TokenRuns != 1 || summaries[0].CostRuns != 1 || summaries[0].ActualCostUSD != 0 || summaries[0].EstimatedCostUSD != cost {
		t.Fatalf("fabricated accounting: %+v", summaries)
	}
}
func TestRoutingRulesRespectTaskOverridesAndResources(t *testing.T) {
	rt := newRunnerTest(t)
	ctx := context.Background()
	task := rt.task(t)
	task.Title = "Security review"
	rules := []domain.RoutingRule{{Name: "Review", Contains: "security", Agent: "codex", Reasoning: "high", MinCPU: 4}}
	if e := rt.svc.SetRules(ctx, rules); e != nil {
		t.Fatal(e)
	}
	_, res, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex"})
	if e != nil || res.Reasoning != "high" {
		t.Fatalf("rule: %+v %v", res, e)
	}
	_, res, e = rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex", Reasoning: "low", Sources: domain.ResolvedSources{Reasoning: domain.SourceTask}})
	if e != nil || res.Reasoning != "low" {
		t.Fatalf("override: %+v %v", res, e)
	}
	rules[0].MinRAMBytes = 1
	if e := rt.svc.SetRules(ctx, rules); e != nil {
		t.Fatal(e)
	}
	if _, _, e := rt.svc.Route(ctx, task, domain.Resolved{Agent: "codex"}); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("unknown RAM admitted: %v", e)
	}
}
