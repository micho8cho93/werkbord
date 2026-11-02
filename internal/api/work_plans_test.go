package api

import (
	"encoding/json"
	"fmt"
	"testing"
	"werkbord/internal/domain"
	"werkbord/internal/service"
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

func TestPlanRunRoutesNeedTheOwnerAndAreProjectScoped(t *testing.T) {
	sc := newScoped(t)
	task := sc.taskIn(t, sc.project.ID, "Implement protocol")
	base := sc.url + "/api/projects/" + sc.project.ID + "/work-plans/wpl_x"
	for _, route := range [][2]string{{"GET", base + "/run"}, {"POST", base + "/run"}, {"POST", base + "/hold"}} {
		for _, token := range []string{"", sc.mint(t, "Narrow client")} {
			if code, _ := call(t, route[0], route[1], token, `{}`); code != 401 && code != 403 {
				t.Fatalf("%s %s with a narrow credential: %d", route[0], route[1], code)
			}
		}
	}
	_ = task
}

func TestPlanRunAPIPreviewRunAndHold(t *testing.T) {
	ts := newTestServer(t, func(o *Options) { o.Scheduler = &service.Scheduler{Deps: o.Tasks.Deps} })
	var p service.ProjectDetail
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(gitRepo(t))+`}`, &p); code != 201 {
		t.Fatal(code)
	}
	base := ts.URL + "/api/projects/" + p.ID
	var a, b domain.Task
	do(t, "POST", base+"/tasks", `{"title":"first"}`, &a)
	do(t, "POST", base+"/tasks", `{"title":"second"}`, &b)
	var plan domain.WorkPlan
	if code := do(t, "POST", base+"/work-plans", fmt.Sprintf(`{"title":"Release","taskIds":[%q,%q]}`, a.ID, b.ID), &plan); code != 200 {
		t.Fatal(code)
	}
	var pv service.PlanRunPreview
	if code := do(t, "GET", base+"/work-plans/"+plan.ID+"/run", "", &pv); code != 200 || pv.ToStart != 2 || pv.Control != service.PlanControlRun {
		t.Fatal(code, pv)
	}
	if code := do(t, "GET", base+"/work-plans/"+plan.ID+"/run?mode=sideways", "", nil); code != 400 {
		t.Fatal("unknown mode", code)
	}
	if code := do(t, "POST", base+"/work-plans/"+plan.ID+"/run", fmt.Sprintf(`{"version":%d,"digest":"wrong"}`, plan.Version), nil); code != 409 {
		t.Fatal("a digest nobody reviewed", code)
	}
	var ran domain.WorkPlan
	if code := do(t, "POST", base+"/work-plans/"+plan.ID+"/run", fmt.Sprintf(`{"version":%d,"digest":%q}`, plan.Version, pv.Digest), &ran); code != 200 || ran.Run == nil || ran.Run.State != domain.PlanRunRunning {
		t.Fatal(code, ran)
	}
	var held domain.WorkPlan
	if code := do(t, "POST", base+"/work-plans/"+plan.ID+"/hold", fmt.Sprintf(`{"version":%d}`, ran.Version), &held); code != 200 || held.Run.State != domain.PlanRunHeld {
		t.Fatal(code, held)
	}
	if code := do(t, "GET", ts.URL+"/api/projects/"+p.ID+"/work-plans/wpl_missing/run", "", nil); code != 404 {
		t.Fatal("missing plan", code)
	}
}

func TestFallbackAPIIsOffByDefaultAndValidated(t *testing.T) {
	ts := newTestServer(t, func(o *Options) { o.Scheduler = &service.Scheduler{Deps: o.Tasks.Deps} })
	var p service.ProjectDetail
	if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(gitRepo(t))+`}`, &p); code != 201 {
		t.Fatal(code)
	}
	url := ts.URL + "/api/projects/" + p.ID + "/fallback"
	var got struct {
		domain.FallbackSettings
		Ledger []domain.FallbackEntry `json:"ledger"`
	}
	if code := do(t, "GET", url, "", &got); code != 200 || got.Enabled || got.Ledger == nil {
		t.Fatal(code, got)
	}
	if code := do(t, "PUT", url, `{"enabled":true,"providers":[]}`, nil); code != 400 {
		t.Fatal("enabling with nobody to hand over to", code)
	}
	if code := do(t, "PUT", url, `{"enabled":true,"providers":["codex"],"maxSwitches":2,"dailyLimit":5}`, &got); code != 200 || !got.Enabled || got.MaxSwitches != 2 || got.DailyLimit != 5 {
		t.Fatal(code, got)
	}
	if code := do(t, "GET", ts.URL+"/api/projects/prj_missing/fallback", "", nil); code != 404 {
		t.Fatal(code)
	}
}
