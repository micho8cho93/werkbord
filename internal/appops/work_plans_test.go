package appops

import (
	"encoding/json"
	"strings"
	"testing"

	"werkbord/internal/domain"
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

func TestSalieImportsAChainedBatchWithOneConfirmation(t *testing.T) {
	f := newFx(t)
	project := f.project("Delivery")
	p := f.session("ast_batch")
	args := map[string]any{"projectId": project.ID, "chain": true, "plans": []any{
		map[string]any{"title": "UI", "tasks": []any{map[string]any{"title": "Tokens"}, map[string]any{"title": "Buttons"}, map[string]any{"title": "Forms"}}},
		map[string]any{"title": "UX", "after": "UI", "tasks": []any{map[string]any{"title": "Flows"}, map[string]any{"title": "Copy"}}},
	}}
	pending := f.mustCall(p, "import_work_batch", args)
	if pending.Pending == nil || len(f.tickets(project.ID)) != 0 {
		t.Fatalf("created before confirmation: %+v", pending)
	}
	if !strings.Contains(pending.Pending.Summary, "2 plans with 5 tasks") || !strings.Contains(pending.Pending.Summary, "starts after UI") {
		t.Fatal(pending.Pending.Summary)
	}
	if _, err := f.confirm(p, pending.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	plans, _ := f.tasks.WorkPlans(bg, project.ID)
	if len(plans) != 2 {
		t.Fatalf("plans: %+v", plans)
	}
	// Plans made in the same instant come back in no promised order.
	if plans[0].Title != "UI" {
		plans[0], plans[1] = plans[1], plans[0]
	}
	if len(plans[0].TaskIDs) != 3 || len(plans[1].TaskIDs) != 2 {
		t.Fatalf("plans: %+v", plans)
	}
	byID := map[string]domain.Task{}
	for _, task := range f.tickets(project.ID) {
		byID[task.ID] = task
	}
	deps := func(id string) []string {
		return byID[id].Orchestration.Dependencies
	}
	ui, ux := plans[0].TaskIDs, plans[1].TaskIDs
	if len(deps(ui[0])) != 0 || deps(ui[1])[0] != ui[0] || deps(ui[2])[0] != ui[1] {
		t.Fatal("tasks within a plan are not chained")
	}
	if deps(ux[0])[0] != ui[2] || deps(ux[1])[0] != ux[0] {
		t.Fatal("the second plan does not wait for the first")
	}
	if _, err := f.call(p, "import_work_batch", map[string]any{"projectId": project.ID, "chain": true, "plans": []any{
		map[string]any{"title": "A", "after": "B", "tasks": []any{map[string]any{"title": "x"}}}, map[string]any{"title": "B", "tasks": []any{map[string]any{"title": "y"}}}}}); err == nil {
		t.Fatal("a plan was allowed to come after one that follows it")
	}
}

func TestCheckWorkStructureNamesLooseEnds(t *testing.T) {
	f := newFx(t)
	project := f.project("Delivery")
	p := f.session("ast_structure")
	a, _ := f.tasks.Create(bg, project.ID, "Design", "")
	b, _ := f.tasks.Create(bg, project.ID, "Build", "")
	if _, err := f.tasks.Create(bg, project.ID, "Stray", ""); err != nil {
		t.Fatal(err)
	}
	pending := f.mustCall(p, "save_work_plan", map[string]any{"projectId": project.ID, "title": "Ship", "description": "", "taskIds": []string{a.ID, b.ID}, "decisions": "", "questions": "", "version": 0})
	if _, err := f.confirm(p, pending.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	res := f.mustCall(p, "check_work_structure", map[string]any{"projectId": project.ID})
	raw, _ := json.Marshal(res.Data)
	for _, want := range []string{"tasks_outside_plans", "Stray", "plans_without_order", "tasks_without_dates"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}

func TestSalieChainsAnExistingPlan(t *testing.T) {
	f := newFx(t)
	project := f.project("Delivery")
	p := f.session("ast_chain")
	a, _ := f.tasks.Create(bg, project.ID, "One", "")
	b, _ := f.tasks.Create(bg, project.ID, "Two", "")
	c, _ := f.tasks.Create(bg, project.ID, "Three", "")
	saved := f.mustCall(p, "save_work_plan", map[string]any{"projectId": project.ID, "title": "Ship", "description": "", "taskIds": []string{a.ID, b.ID, c.ID}, "decisions": "", "questions": "", "version": 0})
	if _, err := f.confirm(p, saved.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	plans, _ := f.tasks.WorkPlans(bg, project.ID)
	pending := f.mustCall(p, "chain_work_plan", map[string]any{"projectId": project.ID, "planId": plans[0].ID})
	if pending.Pending == nil || !strings.Contains(pending.Pending.Summary, `"Two" waits for "One"`) {
		t.Fatalf("%+v", pending)
	}
	if _, err := f.confirm(p, pending.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.Task{}
	for _, task := range f.tickets(project.ID) {
		byID[task.ID] = task
	}
	if d := byID[b.ID].Orchestration.Dependencies; len(d) != 1 || d[0] != a.ID {
		t.Fatalf("two: %v", d)
	}
	if d := byID[c.ID].Orchestration.Dependencies; len(d) != 1 || d[0] != b.ID {
		t.Fatalf("three: %v", d)
	}
	if _, err := f.call(p, "chain_work_plan", map[string]any{"projectId": project.ID, "planId": plans[0].ID}); err == nil {
		t.Fatal("offered to chain a plan that is already in order")
	}
}

func TestSalieSchedulesAndCancelsAStart(t *testing.T) {
	f := newFx(t)
	project := f.repoProject("Delivery")
	p := f.session("ast_schedule")
	task := f.ticket(project.ID, "Nightly checks")
	ask := map[string]any{"projectId": project.ID, "ticketId": task.ID, "startAt": "2999-11-02T09:00", "timezone": "Europe/Paris"}
	if _, err := f.call(p, "schedule_task_run", map[string]any{"projectId": project.ID, "ticketId": task.ID, "startAt": "2001-01-01T09:00", "timezone": "UTC"}); err == nil {
		t.Fatal("scheduled in the past")
	}
	if _, err := f.call(p, "schedule_task_run", map[string]any{"projectId": project.ID, "ticketId": task.ID, "startAt": "tomorrow"}); err == nil {
		t.Fatal("accepted a time it cannot read")
	}
	pending := f.mustCall(p, "schedule_task_run", ask)
	if !strings.Contains(pending.Pending.Summary, "09:00 (Europe/Paris)") {
		t.Fatal(pending.Pending.Summary)
	}
	if got := f.tickets(project.ID)[0]; got.Orchestration.Enabled {
		t.Fatal("scheduled before confirmation")
	}
	if _, err := f.confirm(p, pending.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	got := f.tickets(project.ID)[0]
	if !got.Orchestration.Enabled || got.Orchestration.ScheduledAt == nil || got.Orchestration.ScheduledAt.UTC().Format("15:04") != "08:00" || got.Orchestration.Timezone != "Europe/Paris" {
		t.Fatalf("%+v", got.Orchestration)
	}
	cancel := f.mustCall(p, "schedule_task_run", map[string]any{"projectId": project.ID, "ticketId": task.ID, "cancel": true})
	if _, err := f.confirm(p, cancel.Pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	if after := f.tickets(project.ID)[0]; after.Orchestration.Enabled || after.Orchestration.ScheduledAt != nil {
		t.Fatalf("%+v", after.Orchestration)
	}
}
