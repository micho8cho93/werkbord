package runner

import (
	"errors"
	"strings"
	"testing"

	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/service"
)

// These tests are about what a run is started with. The hierarchy itself is
// tested in domain; here it is shown to reach the agent and the run's record.

func (e *env) setGlobal(cfg domain.ExecutionConfig) {
	e.t.Helper()
	if _, err := e.settings.SetExecution(ctx, cfg); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) setProject(cfg domain.ExecutionConfig) {
	e.t.Helper()
	if _, err := e.projects.SetExecution(ctx, e.project.ID, cfg); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) taskExec(title string, cfg domain.ExecutionConfig) *domain.Task {
	e.t.Helper()
	tk, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: title, Description: "Do it.", Execution: cfg})
	if err != nil {
		e.t.Fatal(err)
	}
	return tk
}

// startAuto starts a run that names nothing: whatever the settings say is used.
func (e *env) startAuto(task *domain.Task) *domain.Run {
	e.t.Helper()
	r, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID})
	if err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	return r
}

func (e *env) finish(r *domain.Run) {
	e.t.Helper()
	if _, err := e.mgr.Finish(ctx, r.ID); err != nil {
		e.t.Fatal(err)
	}
}

func TestNothingConfiguredMeansTheAgentsOwnDefaults(t *testing.T) {
	e := newEnv(t)
	run := e.startAuto(e.task("Plain"))
	req := e.session().Req
	if run.AgentID != "fake" || run.Model != "" || run.Reasoning != "" || req.Model != "" || req.Reasoning != "" {
		t.Fatalf("run = %+v, agent was given model %q reasoning %q", run, req.Model, req.Reasoning)
	}
	if run.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("policy = %+v", run.Policy)
	}
}

func TestGlobalDefaultsApplyAndCanBeOverriddenByProjectThenTask(t *testing.T) {
	e := newEnv(t)
	e.adapter.Opts = &domain.AgentOptions{AgentID: "fake", ReasoningSource: domain.OptionsFromAgent, ModelsSource: domain.OptionsFromAgent,
		Reasoning: []domain.AgentOption{{ID: "low"}, {ID: "medium"}, {ID: "high"}}}

	// Global defaults alone.
	e.setGlobal(domain.ExecutionConfig{Agent: "fake", Model: "global-model", Reasoning: "low", Interaction: domain.InteractionAutonomous})
	r1 := e.startAuto(e.task("Inherits all"))
	if r1.Model != "global-model" || r1.Reasoning != "low" || r1.Policy.Interaction != domain.InteractionAutonomous || e.session().Req.Model != "global-model" {
		t.Fatalf("global defaults: run = %+v, request = %+v", r1, e.session().Req)
	}
	e.finish(r1)

	// The project overrides some of them; the rest still come from the global defaults.
	e.setProject(domain.ExecutionConfig{Agent: "fake", Model: "project-model"})
	r2 := e.startAuto(e.task("Project wins"))
	if r2.Model != "project-model" || r2.Reasoning != "low" || r2.Policy.Interaction != domain.InteractionAutonomous {
		t.Fatalf("project over global: %+v", r2)
	}
	e.finish(r2)

	// A task overrides the project: task always wins.
	task := e.taskExec("Task wins", domain.ExecutionConfig{Agent: "fake", Model: "task-model", Reasoning: "high", Interaction: domain.InteractionInteractive})
	r3 := e.startAuto(task)
	if r3.Model != "task-model" || r3.Reasoning != "high" || r3.Policy.Interaction != domain.InteractionInteractive {
		t.Fatalf("task over project: %+v", r3)
	}
	e.finish(r3)

	// A task can ask for the agent's own default even though the project names a model.
	task2 := e.taskExec("Agent default", domain.ExecutionConfig{Agent: "fake", Model: domain.AgentDefault, Reasoning: domain.AgentDefault})
	r4 := e.startAuto(task2)
	if r4.Model != "" || r4.Reasoning != "" || e.session().Req.Model != "" || e.session().Req.Reasoning != "" {
		t.Fatalf("agent default must pass nothing: run %+v, request %+v", r4, e.session().Req)
	}
	e.finish(r4)

	// This run only: what is asked when starting wins over everything, and changes nothing stored.
	r5, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("One-off").ID, AgentID: "fake", Model: "once", Reasoning: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if r5.Model != "once" || r5.Reasoning != "medium" {
		t.Fatalf("run override: %+v", r5)
	}
	if got, _ := e.projects.Get(ctx, e.project.ID); got.Execution.Model != "project-model" {
		t.Fatalf("a one-off run changed the project: %+v", got.Execution)
	}
}

func TestChangingTheDefaultsDoesNotChangeARunThatIsWorking(t *testing.T) {
	e := newEnv(t)
	e.setGlobal(domain.ExecutionConfig{Agent: "fake", Model: "before"})
	run := e.startAuto(e.task("Long job"))
	e.setGlobal(domain.ExecutionConfig{Agent: "fake", Model: "after"})
	if got := e.run(run.ID); got.Model != "before" {
		t.Fatalf("the working run's model changed to %q", got.Model)
	}
	e.finish(run)
	if next := e.startAuto(e.task("Next")); next.Model != "after" {
		t.Fatalf("the next run's model = %q", next.Model)
	}
}

func TestAModelBelongsToItsAgent(t *testing.T) {
	e := newEnv(t)
	other := &fake.Adapter{Name: "other"}
	if err := e.agents.Register(other); err != nil {
		t.Fatal(err)
	}
	// The project's default is a model of "fake"; a task that switches agent must
	// not be handed it.
	e.setProject(domain.ExecutionConfig{Agent: "fake", Model: "fake-model", Reasoning: "high"})
	task := e.taskExec("Switches agent", domain.ExecutionConfig{Agent: "other"})
	run := e.startAuto(task)
	if run.AgentID != "other" || run.Model != "" || run.Reasoning != "" {
		t.Fatalf("run = %+v: another agent's model leaked", run)
	}
	if r := other.Last().Req; r.Model != "" || r.Reasoning != "" {
		t.Fatalf("the agent was given %q / %q", r.Model, r.Reasoning)
	}
	e.finish(run)

	// And the task that does not switch still gets it.
	if r := e.startAuto(e.task("Stays")); r.AgentID != "fake" || r.Model != "fake-model" {
		t.Fatalf("run = %+v", r)
	}
}

func TestNoAgentChosenUsesTheFirstUsableOne(t *testing.T) {
	e := newEnv(t)
	// Registered later but sorts first, and is unusable; "fake" is then the first usable.
	broken := &fake.Adapter{Name: "aaa", Info: domain.Agent{ID: "aaa", Name: "Broken", Detail: "not installed"}}
	if err := e.agents.Register(broken); err != nil {
		t.Fatal(err)
	}
	if run := e.startAuto(e.task("Auto")); run.AgentID != "fake" {
		t.Fatalf("agent = %s", run.AgentID)
	}
}

func TestMissingAgentIsRefusedWithWhy(t *testing.T) {
	e := newEnv(t)
	e.adapter.Info = domain.Agent{ID: "fake", Name: "Fake", Detail: "not installed", Guidance: "install it"}

	// Nothing chosen and nothing usable: say why, start nothing.
	task := e.task("No agent")
	_, err := e.mgr.Start(ctx, StartInput{TaskID: task.ID})
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
	// Chosen by the project but missing: say whose choice it was.
	e.setProject(domain.ExecutionConfig{Agent: "fake"})
	_, err = e.mgr.Start(ctx, StartInput{TaskID: task.ID})
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "project settings choose fake") {
		t.Fatalf("err = %v", err)
	}
	if rs, _ := e.runs.ListByTask(ctx, task.ID); len(rs) != 0 {
		t.Fatalf("a refused start left runs: %+v", rs)
	}
}

func TestAReasoningLevelTheAgentLacksIsRefused(t *testing.T) {
	e := newEnv(t)
	e.adapter.Opts = &domain.AgentOptions{AgentID: "fake", Reasoning: []domain.AgentOption{{ID: "low"}, {ID: "high"}}}
	// Refused when it is saved...
	if _, err := e.settings.SetExecution(ctx, domain.ExecutionConfig{Agent: "fake", Reasoning: "turbo"}); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "low, high") {
		t.Fatalf("saved: %v", err)
	}
	// ...and when a run asks for it, with the choices that exist.
	_, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("t").ID, AgentID: "fake", Reasoning: "turbo"})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "low, high") {
		t.Fatalf("started: %v", err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("t2").ID, AgentID: "fake", Reasoning: "low"}); err != nil {
		t.Fatalf("a level the agent has: %v", err)
	}
	// An unknown agent in the defaults is refused too.
	if _, err := e.projects.SetExecution(ctx, e.project.ID, domain.ExecutionConfig{Agent: "gemini"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown agent: %v", err)
	}
}

func TestAResumedRunKeepsItsModelAndReasoningAcrossARestart(t *testing.T) {
	e := newEnv(t)
	e.adapter.Opts = &domain.AgentOptions{AgentID: "fake", Reasoning: []domain.AgentOption{{ID: "high"}}}
	run, err := e.mgr.Start(ctx, StartInput{TaskID: e.task("Resumable").ID, AgentID: "fake", Model: "m1", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	e.session().Ref("sess-1")
	eventually(t, "the session ref", func() bool { return e.run(run.ID).SessionRef == "sess-1" })
	e.session().TurnEnd()
	e.waitWaiting(run.ID, domain.WaitIdle)

	if err := e.mgr.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = e.db.Close()
	e.open()
	// The defaults changed while it sat idle; a message that resumes it must not pick them up.
	e.setGlobal(domain.ExecutionConfig{Agent: "fake", Model: "m2"})
	e.mgr = e.newManager()
	if err := e.mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Send(ctx, run.ID, "Carry on."); err != nil {
		t.Fatal(err)
	}
	if req := e.session().Req; req.Model != "m1" || req.Reasoning != "high" || req.ResumeRef != "sess-1" {
		t.Fatalf("resume request = %+v", req)
	}
}
