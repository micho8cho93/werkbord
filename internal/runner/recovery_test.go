package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"werkbord/internal/agent"
	"werkbord/internal/domain"
	"werkbord/internal/runnerwire"
	"werkbord/internal/service"
)

func recoveryPeer(t *testing.T, e *env, now *time.Time) (string, func()) {
	t.Helper()
	install := func() *service.Runners {
		deps := e.tasks.Deps
		deps.Now = func() time.Time { return *now }
		d := &service.Runners{Deps: deps, Runs: e.runs}
		e.mgr.opt.Distributed = d
		e.mgr.opt.Scheduler.Runners = d
		return d
	}
	d := install()
	pair, err := d.Pair(ctx, "http://controller.test:7420", []string{e.project.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _ := runnerwire.DecodeCode(pair.Code)
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	peer, err := d.Join(ctx, runnerwire.Join{Secret: secret, PublicKey: base64.RawURLEncoding.EncodeToString(public), Name: "Recovery peer"})
	if err != nil {
		t.Fatal(err)
	}
	peer.Automatic = true
	if _, err := d.Manage(ctx, peer.ID, *peer, false); err != nil {
		t.Fatal(err)
	}
	sequence := int64(0)
	return peer.ID, func() {
		t.Helper()
		d = install()
		sequence++
		heartbeat := runnerwire.Sync{Protocol: runnerwire.Protocol, RunnerID: peer.ID, Sequence: sequence, At: *now, Capabilities: domain.RunnerCapabilities{CPU: 8, Agents: []domain.Agent{{ID: "fake", Available: true}}, Repositories: []string{e.project.ID}}}
		body, _ := json.Marshal(heartbeat)
		if _, err := d.Sync(ctx, body, runnerwire.Signature(key, body)); err != nil {
			t.Fatal(err)
		}
	}
}

func recoveryAssignment(t *testing.T, e *env, runnerID string, o domain.Orchestration) *domain.Task {
	t.Helper()
	task := e.task("Recover scheduled work")
	config := domain.ExecutionConfig{Runner: runnerID, Agent: "fake"}
	var err error
	task, err = e.tasks.Update(ctx, task.ID, service.TaskPatch{Version: task.Version, Execution: &config})
	if err != nil {
		t.Fatal(err)
	}
	return e.arm(task, o)
}

func TestOfflineScheduleSurvivesRestartAndRepeatedReconnect(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	peer, reconnect := recoveryPeer(t, e, &now)
	task := recoveryAssignment(t, e, peer, domain.Orchestration{})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := e.tasks.Get(ctx, task.ID)
	if !saved.Orchestration.WaitingRunner || saved.Orchestration.RunID != "" {
		t.Fatalf("offline state: %+v", saved.Orchestration)
	}
	plan, err := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
	if err != nil || plan[0].State != "waiting_runner" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Close(); err != nil {
		t.Fatal(err)
	}
	e.open()
	e.mgr = e.newManager()
	e.orchestrate(&now)
	reconnect()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		reconnect()
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := e.runs.ListByTask(ctx, task.ID)
	if err != nil || len(runs) != 1 || !runs[0].Remote || runs[0].RunnerID != peer || runs[0].ScheduleKey != task.Orchestration.Key {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("offline work fell back to controller")
	}
}

func TestReconnectRequiresApprovalAndRechecksDependencies(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	peer, reconnect := recoveryPeer(t, e, &now)
	task := recoveryAssignment(t, e, peer, domain.Orchestration{ReconnectPolicy: "manual"})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	reconnect()
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 0 {
		t.Fatal("reconnect bypassed approval")
	}
	// Rearming grants approval, but cannot grant permission to bypass a dependency.
	dependency := e.task("unfinished prerequisite")
	task, _ = e.tasks.Get(ctx, task.ID)
	task = e.arm(task, domain.Orchestration{Rearm: true, Dependencies: []string{dependency.ID}})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	runs, _ = e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 0 {
		t.Fatal("approval bypassed dependencies")
	}
	task = e.arm(task, domain.Orchestration{Rearm: true})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	runs, _ = e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 1 {
		t.Fatalf("approved run count: %d", len(runs))
	}
}

func TestBoundedPreDispatchRetrySurvivesRestart(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("Rate limited before dispatch"), domain.Orchestration{RetryLimit: 2, UsageLimitBehavior: "retry_temporary"})
	attempts := 0
	failSelection := func(context.Context, domain.SelectionRequest) (domain.SelectionDecision, error) {
		attempts++
		return domain.SelectionDecision{}, errors.New("provider rate_limit: too many requests")
	}
	e.mgr.opt.Selection = failSelection
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := e.tasks.Get(ctx, task.ID)
	if saved.Orchestration.RetryCount != 1 || saved.Orchestration.RetryAt == nil {
		t.Fatalf("retry missing: %+v", saved.Orchestration)
	}
	due := *saved.Orchestration.RetryAt
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if attempts != 1 {
		t.Fatal("backoff ignored")
	}
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	e.orchestrate(&now)
	e.mgr.opt.Selection = failSelection
	now = due
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ = e.tasks.Get(ctx, task.ID)
	if saved.Orchestration.RetryCount != 2 || saved.Orchestration.RetryAt == nil || saved.Orchestration.RetryAt.Sub(now) != time.Minute {
		t.Fatalf("second backoff: %+v", saved.Orchestration)
	}
	now = *saved.Orchestration.RetryAt
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if attempts != 3 {
		t.Fatalf("unbounded retries: %d", attempts)
	}
	saved, _ = e.tasks.Get(ctx, task.ID)
	if saved.Orchestration.RetryAt != nil || saved.Orchestration.Error == "" {
		t.Fatal("exhausted retry was not held")
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 0 {
		t.Fatal("preflight failure created a run")
	}
}

func TestQuotaAndClaimedRunsAreNeverRetried(t *testing.T) {
	for _, message := range []string{"insufficient_quota", "context window exhausted", "invalid_api_key"} {
		t.Run(message, func(t *testing.T) {
			e := newEnv(t)
			now := time.Now().UTC()
			e.orchestrate(&now)
			task := e.arm(e.task("Provider failure"), domain.Orchestration{RetryLimit: 5, UsageLimitBehavior: "retry_temporary"})
			e.mgr.opt.Selection = func(context.Context, domain.SelectionRequest) (domain.SelectionDecision, error) {
				return domain.SelectionDecision{}, errors.New(message)
			}
			if err := e.mgr.ScheduleOnce(ctx); err != nil {
				t.Fatal(err)
			}
			saved, _ := e.tasks.Get(ctx, task.ID)
			if saved.Orchestration.RetryAt != nil || saved.Orchestration.Error == "" || saved.Orchestration.FailureKind == "" {
				t.Fatalf("unsafe recovery: %+v", saved.Orchestration)
			}
		})
	}
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("Claimed start failed"), domain.Orchestration{RetryLimit: 5, UsageLimitBehavior: "retry_temporary"})
	e.adapter.StartFunc = func(_ agent.StartRequest) error { return errors.New("rate_limit") }
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 1 || runs[0].State != domain.RunFailed {
		t.Fatalf("claimed launch repeated: %+v", runs)
	}
}

func TestFailedScheduledProviderTurnKeepsSessionBlocked(t *testing.T) {
	for _, tc := range []struct {
		message string
		kind    domain.ProviderErrorKind
	}{
		{"rate limit", domain.ProviderRateLimit}, {"usage limit reached", domain.ProviderQuota},
		{"context window exhausted", domain.ProviderContext}, {"invalid_api_key", domain.ProviderAuth}, {"unknown failure", domain.ProviderOther},
	} {
		t.Run(tc.message, func(t *testing.T) {
			e := newEnv(t)
			now := time.Now().UTC()
			e.orchestrate(&now)
			task := e.arm(e.task("Failed provider turn"), domain.Orchestration{})
			if err := e.mgr.ScheduleOnce(ctx); err != nil {
				t.Fatal(err)
			}
			runs, _ := e.runs.ListByTask(ctx, task.ID)
			id := runs[0].ID
			e.session().Ref("recover-session")
			e.session().FailTurn(tc.message)
			got := e.waitState(id, domain.RunBlocked)
			if got.Blocker == nil || got.Blocker.ProviderKind != tc.kind {
				t.Fatalf("failure lost: %+v", got)
			}
			if e.session().CloseRequested() {
				t.Fatal("failed scheduled turn auto-finished")
			}
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
			got = e.run(id)
			if got.State != domain.RunBlocked || got.SessionRef != "recover-session" || got.Blocker.ProviderKind != tc.kind {
				t.Fatalf("restart lost failure: %+v", got)
			}
			if err := e.mgr.Send(ctx, id, "I reviewed previous changes; continue safely."); err != nil {
				t.Fatal(err)
			}
			if req := e.session().Req; req.ResumeRef != "recover-session" || req.RunID != id || req.Model != got.Model {
				t.Fatalf("wrong resume: %+v", req)
			}
		})
	}
}

func TestInterruptedWorkingSessionRequiresReviewAndResumesSameRun(t *testing.T) {
	e := newEnv(t)
	run := e.start(e.task("Interrupted work"))
	e.session().Ref("known-session")
	eventually(t, "persisted ref", func() bool { return e.run(run.ID).SessionRef == "known-session" })
	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	got := e.run(run.ID)
	if got.State != domain.RunBlocked || got.Blocker == nil || got.SessionRef != "known-session" {
		t.Fatalf("work lost: %+v", got)
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: run.TaskID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := e.mgr.Send(ctx, run.ID, "Review the saved work and continue."); err != nil {
		t.Fatal(err)
	}
	if e.session().Req.ResumeRef != "known-session" {
		t.Fatal("blind restart")
	}
}

func TestRetrySuccessClaimsOnceAndCannotForgeRecoveryMetadata(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("Retry succeeds"), domain.Orchestration{RetryLimit: 1, UsageLimitBehavior: "retry_temporary"})
	e.mgr.opt.Selection = func(context.Context, domain.SelectionRequest) (domain.SelectionDecision, error) {
		return domain.SelectionDecision{}, errors.New("rate limit")
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	task, _ = e.tasks.Get(ctx, task.ID)
	due := *task.Orchestration.RetryAt
	// The client can edit preferences but cannot reset the retry budget or erase a wait.
	forged := task.Orchestration
	forged.RetryCount, forged.RetryAt, forged.FailureKind = 0, nil, ""
	task = e.arm(task, forged)
	if task.Orchestration.RetryCount != 1 || task.Orchestration.RetryAt == nil {
		t.Fatal("client reset recovery state")
	}
	now = due
	e.mgr.opt.Selection = func(context.Context, domain.SelectionRequest) (domain.SelectionDecision, error) {
		return domain.SelectionDecision{Agent: "fake"}, nil
	}
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(runs) != 1 || len(e.adapter.Sessions()) != 1 {
		t.Fatalf("retry success duplicated: %+v", runs)
	}
	task, _ = e.tasks.Get(ctx, task.ID)
	if task.Orchestration.RunID != runs[0].ID || task.Orchestration.RetryAt != nil || task.Orchestration.Error != "" {
		t.Fatalf("claim did not settle retry: %+v", task.Orchestration)
	}
}
