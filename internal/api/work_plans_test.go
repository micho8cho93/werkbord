package api

import (
	"encoding/json"
	"testing"
	"werkbord/internal/domain"
)

func TestWorkPlanAPIIsScopedAndRejectsNarrowCredentials(t *testing.T) {
	sc := newScoped(t)
	other := sc.addProject(t, "Other")
	task := sc.taskIn(t, sc.project.ID, "Implement protocol")
	base := sc.url + "/api/projects/" + sc.project.ID + "/work-plans"
	for _, token := range []string{"", sc.mint(t, "Narrow client")} {
		code, _ := call(t, "GET", base, token, "")
		if code != 401 && code != 403 {
			t.Fatalf("narrow credential read plans: %d", code)
		}
	}
	body, _ := json.Marshal(domain.WorkPlan{Title: "Protocol delivery", Description: "Use existing tasks", TaskIDs: []string{task.ID}})
	code, raw := call(t, "POST", base, sc.owner, string(body))
	if code != 200 {
		t.Fatalf("save %d %s", code, raw)
	}
	var plan domain.WorkPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	code, _ = call(t, "PUT", sc.url+"/api/projects/"+other.ID+"/work-plans/"+plan.ID, sc.owner, string(raw))
	if code != 404 {
		t.Fatal("cross-project plan edit", code)
	}
	code, _ = call(t, "PUT", base+"/"+plan.ID, sc.owner, string(raw))
	if code != 200 {
		t.Fatal("edit", code)
	}
	code, _ = call(t, "PUT", base+"/"+plan.ID, sc.owner, string(raw))
	if code != 409 {
		t.Fatal("stale edit", code)
	}
}
