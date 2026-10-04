package api

import (
	"testing"

	"devboard/internal/domain"
)

type optionsBody struct {
	AgentID      string               `json:"agentId"`
	Models       []domain.AgentOption `json:"models"`
	Reasoning    []domain.AgentOption `json:"reasoning"`
	CustomModels bool                 `json:"customModels"`
}

func TestAgentOptionsAlwaysOfferTheAgentDefault(t *testing.T) {
	rs := newRunsServer(t)
	rs.adapter.Opts = &domain.AgentOptions{AgentID: "fake", Models: []domain.AgentOption{{ID: "m1", Name: "M1"}},
		Reasoning: []domain.AgentOption{{ID: "low"}, {ID: "high"}}, ModelsSource: domain.OptionsFromAgent, ReasoningSource: domain.OptionsFromAgent}
	var o optionsBody
	if code := do(t, "GET", rs.url+"/api/agents/fake/options", "", &o); code != 200 {
		t.Fatalf("options = %d", code)
	}
	if o.Models[0].ID != domain.AgentDefault || o.Models[0].Name != "Agent default" || o.Models[1].ID != "m1" ||
		o.Reasoning[0].ID != domain.AgentDefault || len(o.Reasoning) != 3 {
		t.Fatalf("options = %+v", o)
	}
	// An agent that cannot list anything still offers the default.
	rs.adapter.Opts = nil
	if code := do(t, "GET", rs.url+"/api/agents/fake/options", "", &o); code != 200 || len(o.Models) != 1 || len(o.Reasoning) != 1 || !o.CustomModels {
		t.Fatalf("an agent with no list: %d %+v", code, o)
	}
	var e apiError
	if code := do(t, "GET", rs.url+"/api/agents/gemini/options", "", &e); code != 404 {
		t.Fatalf("unknown agent = %d", code)
	}
}

func TestGlobalProjectAndTaskDefaultsOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	pid := rs.project.ID
	rs.adapter.Opts = &domain.AgentOptions{AgentID: "fake", Reasoning: []domain.AgentOption{{ID: "low"}, {ID: "high"}}}

	// Nothing set yet: the global defaults are empty.
	var got struct{ Execution domain.ExecutionConfig }
	if code := do(t, "GET", rs.url+"/api/settings", "", &got); code != 200 || !got.Execution.IsZero() {
		t.Fatalf("fresh settings = %d %+v", code, got)
	}

	// Global, then project.
	if code := do(t, "PUT", rs.url+"/api/settings/execution", `{"agent":"fake","model":"g-model","reasoning":"low","interaction":"autonomous","priority":"low"}`, &got); code != 200 || got.Execution.Model != "g-model" {
		t.Fatalf("set global = %d %+v", code, got)
	}
	var proj struct {
		domain.Project
	}
	if code := do(t, "PUT", rs.url+"/api/projects/"+pid+"/execution", `{"agent":"fake","model":"p-model"}`, &proj); code != 200 || proj.Execution.Model != "p-model" {
		t.Fatalf("set project = %d %+v", code, proj)
	}
	// The project shows its defaults when listed, and they persist.
	var list struct{ Projects []domain.Project }
	if code := do(t, "GET", rs.url+"/api/projects", "", &list); code != 200 || list.Projects[0].Execution.Model != "p-model" {
		t.Fatalf("projects = %d %+v", code, list)
	}
	if code := do(t, "GET", rs.url+"/api/settings", "", &got); code != 200 || got.Execution.Reasoning != "low" || got.Execution.Interaction != domain.InteractionAutonomous {
		t.Fatalf("settings = %d %+v", code, got)
	}

	// A task that sets nothing inherits; a task override wins; the run is told what it got.
	plain := rs.task(t, "inherits")
	run := rs.start(t, plain)
	if run.Model != "p-model" || run.Reasoning != "low" || run.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("inherited run = %+v", run)
	}
	if req := rs.adapter.Last().Req; req.Model != "p-model" || req.Reasoning != "low" {
		t.Fatalf("the agent was given %+v", req)
	}
	rs.finish(t, run)

	var task domain.Task
	if code := do(t, "POST", rs.url+"/api/projects/"+pid+"/tasks", `{"title":"override","execution":{"agent":"fake","model":"t-model","reasoning":"high","priority":"high"}}`, &task); code != 201 ||
		task.Execution.Model != "t-model" || task.Execution.Priority != domain.PriorityHigh {
		t.Fatalf("create = %d %+v", code, task)
	}
	var started domain.Run
	if code := do(t, "POST", rs.url+"/api/projects/"+pid+"/tasks/"+task.ID+"/runs", `{}`, &started); code != 201 || started.Model != "t-model" || started.Reasoning != "high" || started.AgentID != "fake" {
		t.Fatalf("a run naming nothing = %d %+v", code, started)
	}
	rs.finish(t, started)

	// The task override persists across a read.
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", rs.url+"/api/projects/"+pid+"/tasks", "", &tasks)
	var found bool
	for _, tk := range tasks.Tasks {
		if tk.ID == task.ID {
			found = tk.Execution.Model == "t-model" && tk.Execution.Reasoning == "high"
		}
	}
	if !found {
		t.Fatalf("the override was not persisted: %+v", tasks.Tasks)
	}

	// Clearing the global defaults is an empty configuration.
	var cleared struct{ Execution domain.ExecutionConfig }
	if code := do(t, "PUT", rs.url+"/api/settings/execution", `{}`, &cleared); code != 200 || !cleared.Execution.IsZero() {
		t.Fatalf("clear = %d %+v", code, cleared)
	}
	var reread struct{ Execution domain.ExecutionConfig }
	if code := do(t, "GET", rs.url+"/api/settings", "", &reread); code != 200 || !reread.Execution.IsZero() {
		t.Fatalf("after clearing = %d %+v", code, reread)
	}
}

func TestExecutionSettingsAreValidatedOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	rs.adapter.Opts = &domain.AgentOptions{AgentID: "fake", Reasoning: []domain.AgentOption{{ID: "low"}}}
	pid := rs.project.ID
	var e apiError
	for _, body := range []string{
		`{"model":"x"}`,                        // a model needs an agent
		`{"agent":"gemini"}`,                   // no such agent
		`{"agent":"fake","reasoning":"turbo"}`, // the agent has no such level
		`{"agent":"fake","model":"--yolo"}`,    // looks like a flag
		`{"agent":"fake","interaction":"yolo"}`,
		`{"priority":"asap"}`,
		`{"agent":"fake","surprise":1}`, // unknown field
	} {
		for _, url := range []string{"/api/settings/execution", "/api/projects/" + pid + "/execution"} {
			if code := do(t, "PUT", rs.url+url, body, &e); code != 400 || e.Error.Code != "invalid" {
				t.Errorf("PUT %s %s = %d %+v", url, body, code, e)
			}
		}
	}
	if code := do(t, "PUT", rs.url+"/api/projects/prj_missing/execution", `{}`, &e); code != 404 {
		t.Errorf("unknown project = %d", code)
	}
	// Nothing invalid was stored.
	var got struct{ Execution domain.ExecutionConfig }
	do(t, "GET", rs.url+"/api/settings", "", &got)
	if !got.Execution.IsZero() {
		t.Errorf("settings = %+v", got)
	}
}

func TestRunnerIsListedAndOnboardingIsRemembered(t *testing.T) {
	rs := newRunsServer(t)
	// Registering is the controller's job at start; here nothing registered yet.
	var rn struct{ Runners []domain.Runner }
	if code := do(t, "GET", rs.url+"/api/runners", "", &rn); code != 200 || len(rn.Runners) != 0 {
		t.Fatalf("runners = %d %+v", code, rn)
	}

	// This server already has a project (the fixture registered one): a computer with
	// projects counts as set up, so an upgrade never opens the wizard over them.
	var ob domain.Onboarding
	if code := do(t, "GET", rs.url+"/api/onboarding", "", &ob); code != 200 || ob.CompletedAt == nil {
		t.Fatalf("onboarding with projects = %d %+v", code, ob)
	}
	if code := do(t, "POST", rs.url+"/api/onboarding/reset", `{}`, &ob); code != 200 {
		t.Fatalf("reset = %d", code)
	}
	if code := do(t, "POST", rs.url+"/api/onboarding/complete", `{"skipped":["github","bogus"]}`, &ob); code != 400 {
		t.Fatalf("an unknown step = %d", code)
	}
	if code := do(t, "POST", rs.url+"/api/onboarding/complete", `{"skipped":["github"]}`, &ob); code != 200 || ob.CompletedAt == nil || len(ob.Skipped) != 1 {
		t.Fatalf("complete = %d %+v", code, ob)
	}
	var again domain.Onboarding
	if code := do(t, "GET", rs.url+"/api/onboarding", "", &again); code != 200 || again.CompletedAt == nil || again.Skipped[0] != "github" {
		t.Fatalf("remembered = %d %+v", code, again)
	}
}
