package runner

import (
	"errors"
	"strings"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/planning"
	"devboard/internal/service"
)

func TestNoAgentIsStartedOnHumanWorkOrOnAWorkProject(t *testing.T) {
	e := newEnv(t)

	human, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "Call the client", WorkMode: planning.ModeHuman})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: human.ID, AgentID: "fake"}); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "human work") {
		t.Fatalf("starting an agent on human work: %v", err)
	}
	if e.adapter.Last() != nil {
		t.Fatal("an agent process was started for human work")
	}
	if runs, _ := e.runs.ListByTask(ctx, human.ID); len(runs) != 0 {
		t.Fatalf("a run was recorded for human work: %+v", runs)
	}

	work, err := e.projects.CreateWork(ctx, "Campaign")
	if err != nil {
		t.Fatal(err)
	}
	// Even a task in a work project that says it is agent work has no repository for an agent to work in.
	agentTask, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: work.ID, Title: "Draft copy", WorkMode: planning.ModeAgent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Start(ctx, StartInput{TaskID: agentTask.ID, AgentID: "fake"}); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "no Git repository") {
		t.Fatalf("starting an agent in a work project: %v", err)
	}
	if e.adapter.Last() != nil {
		t.Fatal("an agent process was started in a work project")
	}
}

func TestHybridAndDefaultWorkStillRunExactlyAsBefore(t *testing.T) {
	e := newEnv(t)
	for _, mode := range []planning.ExecutionMode{"", planning.ModeAgent, planning.ModeHybrid} {
		task, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "Task " + string(mode), WorkMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		run := e.start(task)
		if run.State != domain.RunRunning {
			t.Fatalf("mode %q: run = %+v", mode, run)
		}
		if got := e.taskState(task.ID); got != domain.TaskDoing {
			t.Fatalf("mode %q: card is in %s", mode, got)
		}
		e.finish(run)
	}
}

func TestTheSchedulerLeavesWorkProjectsAlone(t *testing.T) {
	e := newEnv(t)
	work, err := e.projects.CreateWork(ctx, "Ops")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: work.ID, Title: "Renew the contract"}); err != nil {
		t.Fatal(err)
	}
	// A tick with a work project among the projects neither fails nor starts anything.
	now := time.Now().UTC()
	e.orchestrate(&now)
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if e.adapter.Last() != nil {
		t.Fatal("the scheduler started an agent")
	}
}

// Team gave its tickets one list of dependencies. Individual already had one, and this release does not touch it: the
// list on the task's orchestration is what the scheduler waits for and also what the timeline draws and checks.
func TestIndividualHasOneDependencyListForTheSchedulerAndTheTimeline(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.orchestrate(&now)
	design, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "Design", Description: "Do it.", Plan: domain.Plan{Start: "2026-10-05", End: "2026-10-09"}})
	if err != nil {
		t.Fatal(err)
	}
	build, err := e.tasks.CreateTask(ctx, service.NewTask{ProjectID: e.project.ID, Title: "Build", Description: "Do it.", Plan: domain.Plan{Start: "2026-10-07", End: "2026-10-14"}})
	if err != nil {
		t.Fatal(err)
	}
	build = e.arm(build, domain.Orchestration{Dependencies: []string{design.ID}})

	// The scheduler does not start the dependent task while what it waits for is not finished…
	if err := e.mgr.ScheduleOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.adapter.Sessions()) != 0 {
		t.Fatal("the scheduler started a task before its dependency finished")
	}
	// …and the timeline reads that same list: it is the only place a dependency is set.
	tl, err := e.tasks.Timeline(ctx, e.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Warnings) != 1 || tl.Warnings[0].Code != planning.CodeStartsBeforeDependent || tl.Warnings[0].ItemID != build.ID || tl.Warnings[0].OtherID != design.ID {
		t.Fatalf("the timeline did not read the orchestration's dependencies: %+v", tl.Warnings)
	}
}
