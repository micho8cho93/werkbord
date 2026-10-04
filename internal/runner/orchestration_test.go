package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
)

func (e *env) orchestrate(now *time.Time) {
	s := &service.Scheduler{Deps: e.tasks.Deps, Git: &gitrepo.CLI{}}
	s.Now = func() time.Time { return *now }
	h := &service.Handoffs{Deps: e.tasks.Deps, Git: &gitrepo.CLI{}}
	e.runs.Now = s.Now
	e.mgr.opt.Scheduler = s
	e.mgr.opt.Handoffs = h
	e.runs.Handoffs = h
}
func (e *env) arm(task *domain.Task, o domain.Orchestration) *domain.Task {
	e.t.Helper()
	o.Enabled = true
	task, err := e.tasks.Update(ctx, task.ID, service.TaskPatch{Version: task.Version, Orchestration: &o})
	if err != nil {
		e.t.Fatal(err)
	}
	return task
}
func TestScheduledExecutionRecoveryNoDuplicatesAndDependencies(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	future := now.Add(time.Hour)
	a := e.arm(e.task("first"), domain.Orchestration{ScheduledAt: &future})
	b := e.arm(e.task("second"), domain.Orchestration{Dependencies: []string{a.ID}})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("ran too early")
	}
	// Restart before due time restores the schedule from SQLite.
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
	now = future
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 1 {
		t.Fatalf("sessions=%d", len(e.adapter.Sessions()))
	}
	first := e.session()
	first.Assistant(`<devboard-handoff>{"summary":"Implemented first","tests":["go test ./... passed"],"decisions":["Keep API"],"nextAction":"Review first"}</devboard-handoff>`)
	first.TurnEnd()
	eventually(t, "scheduled turn completes without browser", func() bool {
		runs, err := e.runs.ListByTask(ctx, a.ID)
		return err == nil && len(runs) == 1 && runs[0].State == domain.RunCompleted && runs[0].Handoff != nil && len(runs[0].Handoff.Tests) == 1
	})
	if e.taskState(a.ID) != domain.TaskReview {
		t.Fatal("scheduled completion should be reviewable")
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 2 {
		t.Fatalf("downstream was not started: %d", len(e.adapter.Sessions()))
	}
	second := e.session()
	second.TurnEnd()
	eventually(t, "second completes", func() bool { rs, _ := e.runs.ListByTask(ctx, b.ID); return len(rs) == 1 && rs[0].State.Terminal() })
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
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 2 {
		t.Fatal("duplicate execution after restart")
	}
}
func TestScheduleCrashAfterClaimIsNotRetried(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("crash"), domain.Orchestration{})
	// Crash boundary: persisted starting run, before launch.
	r, err := e.runs.Create(ctx, service.NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work", ScheduleKey: task.Orchestration.Key})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.db.Close()
	e.open()
	e.mgr = e.newManager()
	e.orchestrate(&now)
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if e.run(r.ID).State != domain.RunFailed {
		t.Fatal("starting run not recovered")
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("claimed occurrence reran")
	}
}
func TestScheduledPoliciesDoNotInventAnswers(t *testing.T) {
	for _, policy := range []domain.InteractionPolicy{domain.InteractionInteractive, domain.InteractionAutonomous, domain.InteractionAutonomousStopIfBlocked} {
		t.Run(string(policy), func(t *testing.T) {
			e := newEnv(t)
			now := time.Now().UTC()
			e.orchestrate(&now)
			task := e.arm(e.taskWith("policy", policy), domain.Orchestration{})
			if err := e.mgr.ScheduleOnce(ctx); err != nil {
				t.Fatal(err)
			}
			rs, _ := e.runs.ListByTask(ctx, task.ID)
			r := rs[0]
			s := e.session()
			s.Ask("question", "Which database?")
			switch policy {
			case domain.InteractionInteractive:
				e.waitWaiting(r.ID, domain.WaitQuestion)
				if len(s.Responses()) != 0 {
					t.Fatal("interactive answer invented")
				}
				s.TurnEnd()
				e.waitWaiting(r.ID, domain.WaitQuestion)
			case domain.InteractionAutonomous:
				eventually(t, "policy continues", func() bool { return len(s.Responses()) == 1 })
				if e.run(r.ID).State != domain.RunRunning {
					t.Fatal(e.run(r.ID))
				}
			case domain.InteractionAutonomousStopIfBlocked:
				e.waitBlocked(r.ID)
				eventually(t, "stop instruction delivered", func() bool { return len(s.Responses()) == 1 })
				if s.Responses()[0].Answer != agent.StopReply() {
					t.Fatal("blocked answer invented")
				}
			}
		})
	}
}
func TestScheduledMissedAndSetupFailurePersist(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	past := now.Add(-time.Hour)
	skip := e.arm(e.task("missed"), domain.Orchestration{ScheduledAt: &past, MissedPolicy: "skip", GraceSeconds: 30})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ := e.tasks.Get(ctx, skip.ID)
	if !stored.Orchestration.Missed || len(e.adapter.Sessions()) != 0 {
		t.Fatal(stored)
	}
	failing := e.arm(e.task("setup fails"), domain.Orchestration{})
	e.adapter.StartFunc = func(agent.StartRequest) error { return errors.New("unavailable") }
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ = e.tasks.Get(ctx, failing.ID)
	if stored.Orchestration.RunID == "" {
		t.Fatal("launch failure did not consume claim")
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rs, _ := e.runs.ListByTask(ctx, failing.ID)
	if len(rs) != 1 || rs[0].State != domain.RunFailed {
		t.Fatal(rs)
	}
}
func TestManualStartConsumesPendingSchedule(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	future := now.Add(time.Hour)
	task := e.arm(e.task("early"), domain.Orchestration{ScheduledAt: &future})
	run, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if run.ScheduleKey != task.Orchestration.Key {
		t.Fatal("manual start did not claim schedule")
	}
	e.session().TurnEnd()
	e.waitState(run.ID, domain.RunCompleted)
	now = future
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 1 {
		t.Fatal("duplicated manual work")
	}
}
func TestHandoffSwitchAgentModelReviewFixChain(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	second := &fake.Adapter{Name: "reviewer"}
	if err := e.agents.Register(second); err != nil {
		t.Fatal(err)
	}
	task := e.task("implement")
	run := e.start(task)
	s := e.session()
	s.Say(domain.StreamUser, "PRIVATE EARLIER CONVERSATION")
	wt := e.worktreeOf(run)
	if err := os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("new implementation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Assistant("Implementation ready for review.")
	s.Exit(0, "")
	e.waitState(run.ID, domain.RunCompleted)
	review, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "reviewer", Model: "review-model", ParentRunID: run.ID, Purpose: "review", SelectedContext: "Check API behavior only"})
	if err != nil {
		t.Fatal(err)
	}
	req := second.Last().Req
	if req.ResumeRef != "" || req.WorkDir != wt.Path || !strings.Contains(req.Prompt, "new implementation") || !strings.Contains(req.Prompt, "Check API behavior only") || strings.Contains(req.Prompt, "PRIVATE EARLIER CONVERSATION") {
		t.Fatalf("bad continuation context: %s", req.Prompt)
	}
	if review.ParentRunID != run.ID || review.Model != "review-model" || review.AgentID != "reviewer" {
		t.Fatal(review)
	}
	second.Last().Assistant("Review found a boundary issue.")
	second.Last().Exit(0, "")
	e.waitState(review.ID, domain.RunCompleted)
	fix, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID, AgentID: "fake", Model: "fix-model", ParentRunID: review.ID, Purpose: "fix"})
	if err != nil {
		t.Fatal(err)
	}
	if fix.ParentRunID != review.ID || fix.WorktreeID != wt.ID {
		t.Fatal(fix)
	}
	if !strings.Contains(e.session().Req.Prompt, "boundary issue") {
		t.Fatal("review handoff missing")
	}
	e.session().Exit(0, "")
	e.waitState(fix.ID, domain.RunCompleted)
	rs, _ := e.runs.ListByTask(ctx, task.ID)
	if len(rs) != 3 {
		t.Fatal(rs)
	}
}
func TestSchedulerDetectsObservedOverlapAndStaleBranches(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	if err := e.mgr.opt.Scheduler.SetSettings(ctx, e.project.ID, service.OrchestrationSettings{ConcurrencyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	a := e.arm(e.task("declared api"), domain.Orchestration{ExpectedPaths: []string{"internal/"}})
	b := e.arm(e.task("declared web"), domain.Orchestration{ExpectedPaths: []string{"web/"}})
	run, err := e.mgr.Start(ctx, StartInput{TaskID: a.ID, AgentID: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	wt := e.worktreeOf(run)
	if err := os.MkdirAll(filepath.Join(wt.Path, "web"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "web/app.ts"), []byte("observed overlap"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := e.mgr.opt.Scheduler.Plan(ctx, e.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan {
		if d.TaskID == b.ID && d.State != "potentially_conflicting" {
			t.Fatal(d)
		}
	}
	// Commit the overlap: detection must include committed changes too.
	git(t, wt.Path, "add", ".")
	git(t, wt.Path, "commit", "-qm", "agent changes")
	if err := e.mgr.opt.Scheduler.CheckStart(ctx, b.ID, b.Orchestration.Key); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("committed overlap: %v", err)
	}
	e.session().Exit(0, "")
	e.waitState(run.ID, domain.RunCompleted)
	if err := os.WriteFile(filepath.Join(e.repo, "target.txt"), []byte("moved target"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-qm", "target advanced")
	if err := e.mgr.opt.Scheduler.CheckStart(ctx, a.ID, ""); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "behind") {
		t.Fatalf("stale branch: %v", err)
	}
}
func TestScheduledLoopRunsWithoutFrontend(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("background"), domain.Orchestration{})
	background, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); e.mgr.ScheduleLoop(background) }()
	eventually(t, "background dispatch", func() bool {
		rs, _ := e.runs.ListByTask(ctx, task.ID)
		return len(rs) == 1 && rs[0].State == domain.RunRunning
	})
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop failed to stop")
	}
}

func TestSchedulerRunsDisjointTasksUpToCapacity(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	if err := e.mgr.opt.Scheduler.SetSettings(ctx, e.project.ID, service.OrchestrationSettings{ConcurrencyLimit: 2}); err != nil {
		t.Fatal(err)
	}
	tasks := []*domain.Task{}
	for i, p := range []string{"internal/", "web/", "docs/"} {
		order := i + 1
		tasks = append(tasks, e.arm(e.task(p), domain.Orchestration{ExpectedPaths: []string{p}, ExecutionOrder: &order}))
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 2 {
		t.Fatalf("sessions %d", len(e.adapter.Sessions()))
	}
	if rs, _ := e.runs.ListByTask(ctx, tasks[2].ID); len(rs) != 0 {
		t.Fatal("exceeded capacity")
	}
	e.adapter.Sessions()[0].TurnEnd()
	eventually(t, "capacity released", func() bool {
		rs, _ := e.runs.ListByTask(ctx, tasks[0].ID)
		return len(rs) == 1 && rs[0].State == domain.RunCompleted
	})
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 3 {
		t.Fatal("queued work did not use released slot")
	}
}
func TestSchedulerBlocksRepositoryOperationAndStalePinnedTarget(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	task := e.arm(e.task("repository gates"), domain.Orchestration{})
	sha := strings.TrimSpace(git(t, e.repo, "rev-parse", "HEAD"))
	marker := filepath.Join(e.repo, ".git", "MERGE_HEAD")
	if err := os.WriteFile(marker, []byte(sha+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.opt.Scheduler.CheckStart(ctx, task.ID, task.Orchestration.Key); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "operation") {
		t.Fatalf("operation: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	task = e.arm(task, domain.Orchestration{TargetCommit: sha})
	if err := os.WriteFile(filepath.Join(e.repo, "target.txt"), []byte("moved"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-qm", "target changed")
	if err := e.mgr.opt.Scheduler.CheckStart(ctx, task.ID, task.Orchestration.Key); !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "stale target") {
		t.Fatalf("pin: %v", err)
	}
}
