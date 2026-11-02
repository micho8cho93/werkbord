package runner

import (
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

// A plan run only arms tasks. The scheduler loop dispatches them, in order, once each, across restarts and repeated polls.
func TestRunPlanDispatchesInOrderOnceAcrossRestarts(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	a, b := e.task("first"), e.task("second")
	plan, err := e.tasks.SaveWorkPlan(ctx, e.project.ID, "", domain.WorkPlan{Title: "Release", TaskIDs: []string{a.ID, b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	s := e.mgr.opt.Scheduler
	pv, err := s.PlanRunPreview(ctx, e.project.ID, plan.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil || len(e.adapter.Sessions()) != 0 {
		t.Fatal("previewing must start nothing", err)
	}
	ran, err := s.RunPlan(ctx, e.project.ID, plan.ID, service.PlanRunInput{Version: plan.Version, Digest: pv.Digest})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.mgr.ScheduleOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.adapter.Sessions()) != 1 {
		t.Fatalf("only the first step may run: %d sessions", len(e.adapter.Sessions()))
	}
	// A restart and repeated polls create no second run of the same step.
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
	if rs, _ := e.runs.ListByTask(ctx, a.ID); len(rs) != 1 {
		t.Fatalf("duplicate run of the first step: %d", len(rs))
	}
	if rs, _ := e.runs.ListByTask(ctx, b.ID); len(rs) != 0 {
		t.Fatal("the second step started before the first finished")
	}
	// Pausing disarms the step that has not started and leaves the running one alone.
	before, _ := e.runs.ListByTask(ctx, a.ID)
	held, err := e.mgr.opt.Scheduler.HoldPlan(ctx, e.project.ID, plan.ID, ran.Version)
	if err != nil || held.Run.State != domain.PlanRunHeld {
		t.Fatal(held.Run, err)
	}
	if rs, _ := e.runs.ListByTask(ctx, a.ID); len(rs) != 1 || rs[0].State != before[0].State || rs[0].Version != before[0].Version {
		t.Fatal("pausing must not touch a step that already started", rs)
	}
	next, _ := e.tasks.Get(ctx, b.ID)
	if next.Orchestration.Enabled {
		t.Fatal("the unstarted step is still armed after pausing")
	}
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if rs, _ := e.runs.ListByTask(ctx, b.ID); len(rs) != 0 {
		t.Fatal("a paused plan started its next step")
	}
}
