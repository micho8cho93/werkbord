package service

import (
	"context"
	"errors"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/planning"
	"werkbord/internal/store"
)

func planFixture(t *testing.T) (*fixture, *Scheduler, string, domain.WorkPlan, []*domain.Task) {
	t.Helper()
	f, s, pid, _ := schedulingFixture(t)
	ctx := context.Background()
	var tasks []*domain.Task
	var ids []string
	for _, title := range []string{"Design", "Build", "Ship"} {
		task, err := f.tasks.Create(ctx, pid, title, "")
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
		ids = append(ids, task.ID)
	}
	plan, err := f.tasks.SaveWorkPlan(ctx, pid, "", domain.WorkPlan{Title: "Release", TaskIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	return f, s, pid, plan, tasks
}

func TestPlanRunPreviewChangesNothing(t *testing.T) {
	f, s, pid, plan, tasks := planFixture(t)
	ctx := context.Background()
	pv, err := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Mode != domain.PlanModeInOrder || pv.ToStart != 3 || pv.Control != PlanControlRun || pv.State != "idle" || pv.Digest == "" || len(pv.Steps) != 3 {
		t.Fatalf("%+v", pv)
	}
	if got := pv.Steps[1].After; len(got) != 1 || got[0] != "Design" {
		t.Fatalf("second step should wait for the first: %+v", pv.Steps[1])
	}
	if pv.Billing == "" || pv.Interrupt == "" || pv.Review == "" {
		t.Fatal("the preview must carry its billing, interruption and review notes")
	}
	again, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if again.Digest != pv.Digest {
		t.Fatal("a preview must be repeatable")
	}
	for _, task := range tasks {
		got, _ := f.tasks.Get(ctx, task.ID)
		if got.Version != task.Version || got.Orchestration.Enabled {
			t.Fatal("previewing armed a task")
		}
	}
	if as, _ := s.PlanRunPreview(ctx, pid, plan.ID, domain.PlanModeAsReady); as.Digest == pv.Digest || len(as.Steps[1].After) != 0 {
		t.Fatalf("the mode is part of what was reviewed: %+v", as.Steps[1])
	}
	if _, err := s.PlanRunPreview(ctx, pid, plan.ID, "sideways"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.PlanRunPreview(ctx, pid, "wpl_missing", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRunPlanArmsInOrderOnceAndRefusesAStaleDigest(t *testing.T) {
	f, s, pid, plan, tasks := planFixture(t)
	ctx := context.Background()
	pv, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if _, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version, Digest: "stale"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a digest that was not reviewed must be refused", err)
	}
	if _, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version + 1, Digest: pv.Digest}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a stale plan version must be refused", err)
	}
	// A task edited after the preview changes what would be armed.
	title := "Build it properly"
	edited, err := f.tasks.Update(ctx, tasks[1].ID, TaskPatch{Version: tasks[1].Version, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version, Digest: pv.Digest}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("an edited step must invalidate the preview", err)
	}
	pv, _ = s.PlanRunPreview(ctx, pid, plan.ID, "")
	got, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version, Digest: pv.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run == nil || got.Run.State != domain.PlanRunRunning || len(got.Run.TaskIDs) != 3 || got.Version != plan.Version+1 {
		t.Fatalf("%+v", got)
	}
	keys := map[string]string{}
	for i, task := range tasks {
		cur, _ := f.tasks.Get(ctx, task.ID)
		o := cur.Orchestration
		if !o.Enabled || o.Key == "" || o.RunID != "" {
			t.Fatalf("step %d is not armed: %+v", i+1, o)
		}
		keys[task.ID] = o.Key
		want := 0
		if i > 0 {
			want = 1
		}
		if len(o.Dependencies) != want || want == 1 && o.Dependencies[0] != tasks[i-1].ID {
			t.Fatalf("step %d dependencies %v", i+1, o.Dependencies)
		}
	}
	_ = edited
	// Asking again changes nothing: same keys, same version.
	again, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: got.Version, Digest: pv.Digest})
	if err != nil || again.Version != got.Version {
		t.Fatal("a repeated run must be a no-op", again.Version, err)
	}
	for _, task := range tasks {
		cur, _ := f.tasks.Get(ctx, task.ID)
		if cur.Orchestration.Key != keys[task.ID] {
			t.Fatal("a repeated run re-keyed a schedule")
		}
	}
	// The scheduler orders the chain: only the first step is runnable.
	decisions, err := s.Plan(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, d := range decisions {
		states[d.TaskID] = d.State
	}
	if states[tasks[0].ID] != "runnable" || states[tasks[1].ID] != "waiting_dependency" || states[tasks[2].ID] != "waiting_dependency" {
		t.Fatalf("%v", states)
	}
	// Progress is read from the plan: a finished run leaves nothing to start, and a paused plan can be resumed.
	cur, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if cur.Control != PlanControlHold {
		t.Fatalf("a running plan offers a pause: %+v", cur)
	}
}

func TestHoldDisarmsOnlyWhatHasNotStartedAndResumeRearms(t *testing.T) {
	f, s, pid, plan, tasks := planFixture(t)
	ctx := context.Background()
	pv, _ := s.PlanRunPreview(ctx, pid, plan.ID, domain.PlanModeAsReady)
	ran, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Mode: domain.PlanModeAsReady, Version: plan.Version, Digest: pv.Digest})
	if err != nil {
		t.Fatal(err)
	}
	// The first step was already dispatched by the scheduler.
	first, _ := f.tasks.Get(ctx, tasks[0].ID)
	o := first.Orchestration
	o.RunID = "run_started"
	first.Orchestration = o
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Tasks().Update(ctx, first) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HoldPlan(ctx, pid, plan.ID, ran.Version+7); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("a stale pause must be refused", err)
	}
	held, err := s.HoldPlan(ctx, pid, plan.ID, ran.Version)
	if err != nil || held.Run.State != domain.PlanRunHeld {
		t.Fatal(held.Run, err)
	}
	for i, task := range tasks {
		cur, _ := f.tasks.Get(ctx, task.ID)
		if i == 0 && (cur.Orchestration.RunID != "run_started" || !cur.Orchestration.Enabled) {
			t.Fatal("pausing touched the step that already started")
		}
		if i > 0 && cur.Orchestration.Enabled {
			t.Fatalf("step %d is still armed", i+1)
		}
	}
	if again, err := s.HoldPlan(ctx, pid, plan.ID, held.Version); err != nil || again.Version != held.Version {
		t.Fatal("pausing twice must change nothing", err)
	}
	cur, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if cur.Control != PlanControlResume || cur.ToStart != 2 {
		t.Fatalf("%+v", cur)
	}
	resumed, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: held.Version, Digest: cur.Digest})
	if err != nil || resumed.Run.State != domain.PlanRunRunning {
		t.Fatal(resumed.Run, err)
	}
	for _, task := range tasks[1:] {
		got, _ := f.tasks.Get(ctx, task.ID)
		if !got.Orchestration.Enabled || got.Orchestration.RunID != "" {
			t.Fatal("resume did not rearm", got.Orchestration)
		}
	}
}

func TestRunPlanLeavesHumanDoneAndStartedWorkAlone(t *testing.T) {
	f, s, pid, plan, tasks := planFixture(t)
	ctx := context.Background()
	human := planning.ModeHuman
	if _, err := f.tasks.Update(ctx, tasks[0].ID, TaskPatch{Version: tasks[0].Version, WorkMode: &human}); err != nil {
		t.Fatal(err)
	}
	done := domain.TaskDone
	cur, _ := f.tasks.Get(ctx, tasks[1].ID)
	if _, err := f.tasks.Update(ctx, cur.ID, TaskPatch{Version: cur.Version, State: &done}); err != nil {
		t.Fatal(err)
	}
	pv, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if pv.Steps[0].Action != PlanStepSkip || pv.Steps[1].Action != PlanStepSkip || pv.Steps[2].Action != PlanStepStart || pv.ToStart != 1 {
		t.Fatalf("%+v", pv.Steps)
	}
	if _, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version, Digest: pv.Digest}); err != nil {
		t.Fatal(err)
	}
	last, _ := f.tasks.Get(ctx, tasks[2].ID)
	// The finished step is not waited for; the person's own step still is.
	if deps := last.Orchestration.Dependencies; len(deps) != 1 || deps[0] != tasks[0].ID {
		t.Fatalf("dependencies %v", deps)
	}
	h, _ := f.tasks.Get(ctx, tasks[0].ID)
	if h.Orchestration.Enabled {
		t.Fatal("human work was armed for an agent")
	}
}

func TestPlanEditsCannotWriteTheRunState(t *testing.T) {
	f, s, pid, plan, _ := planFixture(t)
	ctx := context.Background()
	forged := plan
	forged.Run = &domain.PlanRun{State: domain.PlanRunRunning}
	saved, err := f.tasks.SaveWorkPlan(ctx, pid, plan.ID, forged)
	if err != nil || saved.Run != nil {
		t.Fatal("a plan edit must not set the run state", saved.Run, err)
	}
	pv, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	ran, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: saved.Version, Digest: pv.Digest})
	if err != nil {
		t.Fatal(err)
	}
	ran.Title = "Renamed"
	renamed, err := f.tasks.SaveWorkPlan(ctx, pid, plan.ID, ran)
	if err != nil || renamed.Run == nil || renamed.Run.State != domain.PlanRunRunning {
		t.Fatal("a plan edit must keep the run state", renamed.Run, err)
	}
	stale := renamed
	stale.Run = nil
	cleared, err := f.tasks.SaveWorkPlan(ctx, pid, plan.ID, stale)
	if err != nil || cleared.Run == nil {
		t.Fatal("a plan edit must not clear the run state", cleared.Run, err)
	}
}

func TestRunPlanRetriesAFailedStepOnlyWhenTheReviewedPreviewSaysSo(t *testing.T) {
	f, s, pid, plan, tasks := planFixture(t)
	ctx := context.Background()
	run, err := f.runs.Create(ctx, NewRun{TaskID: tasks[0].ID, AgentID: "fake", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Runs().Get(ctx, run.ID)
		if e != nil {
			return e
		}
		r.State, r.Reason = domain.RunFailed, "provider error"
		return tx.Runs().Update(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	pv, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if pv.Steps[0].Action != PlanStepRetry || pv.Steps[0].Why == "" || pv.ToStart != 3 {
		t.Fatalf("%+v", pv.Steps[0])
	}
	if _, err := s.RunPlan(ctx, pid, plan.ID, PlanRunInput{Version: plan.Version, Digest: pv.Digest}); err != nil {
		t.Fatal(err)
	}
	first, _ := f.tasks.Get(ctx, tasks[0].ID)
	if !first.Orchestration.Enabled || first.Orchestration.Key == "" || first.Orchestration.RunID != "" {
		t.Fatalf("the failed step was not armed again: %+v", first.Orchestration)
	}
	again, _ := s.PlanRunPreview(ctx, pid, plan.ID, "")
	if again.Steps[0].Action != PlanStepArmed {
		t.Fatalf("an armed retry is waiting for the scheduler, not to be retried again: %+v", again.Steps[0])
	}
}
