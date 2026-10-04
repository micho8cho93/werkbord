package api

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/service"
	"fmt"
	"testing"
	"time"
)

func TestOrchestrationAPIAndProjectScope(t *testing.T) {
	var runs *service.Runs
	ts := newTestServer(t, func(o *Options) {
		deps := o.Tasks.Deps
		o.Scheduler = &service.Scheduler{Deps: deps}
		o.Scheduler.Now = func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) }
		o.Handoffs = &service.Handoffs{Deps: deps}
		runs = o.Runs
	})
	var p, q service.ProjectDetail
	for _, out := range []*service.ProjectDetail{&p, &q} {
		if code := do(t, "POST", ts.URL+"/api/projects", `{"path":`+jsonString(gitRepo(t))+`}`, out); code != 201 {
			t.Fatal(code)
		}
	}
	var task domain.Task
	base := ts.URL + "/api/projects/" + p.ID
	if code := do(t, "POST", base+"/tasks", `{"title":"scheduled","orchestration":{"enabled":true,"scheduledAt":"2026-10-05T01:00:00+02:00","timezone":"Europe/Madrid","missedPolicy":"skip","graceSeconds":300,"executionOrder":1}}`, &task); code != 201 {
		t.Fatal(code)
	}
	if task.Orchestration.ScheduledAt.Format(time.RFC3339) != "2026-10-04T23:00:00Z" || task.Orchestration.Key == "" {
		t.Fatal(task)
	}
	if code := do(t, "PATCH", base+"/tasks/"+task.ID, fmt.Sprintf(`{"version":%d,"orchestration":{"scheduledAt":"2026-10-05T01:00:00"}}`, task.Version), nil); code != 400 {
		t.Fatalf("offset-free timestamp: %d", code)
	}
	var settings service.OrchestrationSettings
	if code := do(t, "PUT", base+"/orchestration", `{"concurrencyLimit":2}`, &settings); code != 200 || settings.ConcurrencyLimit != 2 {
		t.Fatal(code, settings)
	}
	var plan struct{ Decisions []domain.SchedulingDecision }
	if code := do(t, "GET", base+"/schedule", "", &plan); code != 200 || len(plan.Decisions) != 1 || plan.Decisions[0].State != "waiting_schedule" {
		t.Fatal(code, plan)
	}
	if code := do(t, "GET", ts.URL+"/api/projects/missing/schedule", "", nil); code != 404 {
		t.Fatal(code)
	}
	run, e := runs.Create(context.Background(), service.NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "work"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = runs.End(context.Background(), run.ID, service.Ended{State: domain.RunStopped}); e != nil {
		t.Fatal(e)
	}
	wrong := ts.URL + "/api/projects/" + q.ID + "/runs/" + run.ID
	for _, route := range []struct{ method, path, body string }{{"POST", wrong + "/handoff", ""}, {"PUT", wrong + "/handoff", `{"version":2,"handoff":{"summary":"wrong"}}`}} {
		if code := do(t, route.method, route.path, route.body, nil); code != 404 {
			t.Fatalf("scope: %+v %d", route, code)
		}
	}
	var captured domain.Run
	if code := do(t, "POST", base+"/runs/"+run.ID+"/handoff", "", &captured); code != 200 || captured.Handoff == nil {
		t.Fatal(code, captured)
	}
	if code := do(t, "PUT", base+"/runs/"+run.ID+"/handoff", fmt.Sprintf(`{"version":%d,"handoff":{"objective":"work","summary":"review ready","tests":["go test passed"],"nextAction":"review"}}`, captured.Version), &captured); code != 200 || captured.Handoff.Summary != "review ready" {
		t.Fatal(code, captured)
	}
	// A stale editor cannot overwrite someone else's handoff.
	if code := do(t, "PUT", base+"/runs/"+run.ID+"/handoff", `{"version":1,"handoff":{"summary":"stale"}}`, nil); code != 409 {
		t.Fatal(code)
	}
	var overview service.Overview
	if code := do(t, "GET", ts.URL+"/api/control-center", "", &overview); code != 200 || len(overview.Orchestration) != 1 || overview.Orchestration[0].Task.ID != task.ID {
		t.Fatal(code, overview)
	}
}
