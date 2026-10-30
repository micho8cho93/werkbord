package appops

import (
	"encoding/json"
	"testing"
)

func TestSalieWorkPlanRequiresConfirmationAndKeepsTaskIdentity(t *testing.T) {
	f := newFx(t)
	project := f.project("Delivery")
	a, _ := f.tasks.Create(bg, project.ID, "Design", "")
	b, _ := f.tasks.Create(bg, project.ID, "Build", "")
	p := f.session("ast_plans")
	args := map[string]any{"projectId": project.ID, "title": "Ship delivery", "description": "Coordinate work", "taskIds": []string{a.ID, b.ID}, "decisions": "Use existing orchestration", "questions": "Who reviews?", "version": 0}
	pending := f.mustCall(p, "save_work_plan", args)
	plans, _ := f.tasks.WorkPlans(bg, project.ID)
	if len(plans) != 0 {
		t.Fatal("saved before confirmation")
	}
	if pending.Pending == nil {
		t.Fatalf("no proposal: %+v", pending)
	}
	_, err := f.confirm(p, pending.Pending.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	plans, _ = f.tasks.WorkPlans(bg, project.ID)
	if len(plans) != 1 || plans[0].TaskIDs[1] != b.ID || len(f.tickets(project.ID)) != 2 {
		t.Fatal(plans)
	}
	result := f.mustCall(p, "list_work_plans", map[string]any{"projectId": project.ID})
	raw, _ := json.Marshal(result.Data)
	if len(raw) == 0 {
		t.Fatal("empty read")
	}
	args["planId"] = plans[0].ID
	args["version"] = plans[0].Version
	args["questions"] = "Ready?"
	pending = f.mustCall(p, "save_work_plan", args)
	changed := plans[0]
	changed.Questions = "Changed in Work"
	if _, err = f.tasks.SaveWorkPlan(bg, project.ID, changed.ID, changed); err != nil {
		t.Fatal(err)
	}
	if _, err = f.confirm(p, pending.Pending.ActionID, true); err == nil {
		t.Fatal("stale confirmation overwrote Work")
	}
	current, _ := f.tasks.WorkPlans(bg, project.ID)
	if current[0].Questions != "Changed in Work" {
		t.Fatal(current)
	}
	readonly := f.session("ast_plans_ro", PermTicketsRead)
	if _, err = f.call(readonly, "save_work_plan", args); err == nil {
		t.Fatal("read-only mutation")
	}
}
