package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestProjectLifecyclePreservesWorkAndIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/lifecycle", "Lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, p.ID, "Keep this work", "Past context")
	if err != nil {
		t.Fatal(err)
	}
	mem := memoryService(t, f, "individual", domain.MemoryLimits{})
	scope := domain.MemoryScope{ProjectID: p.ID}
	memory := createMemory(t, mem, scope, memoryContent("Preserve this project decision"))
	for _, state := range []string{"archived", "deleted"} {
		got, err := f.projects.SetLifecycle(ctx, p.ID, state)
		if err != nil || !got.Inactive() {
			t.Fatalf("%s: %+v %v", state, got, err)
		}
		active, _ := f.projects.List(ctx)
		if len(active) != 0 {
			t.Fatal("inactive project still listed")
		}
		history, _ := f.projects.History(ctx)
		if len(history) != 1 || history[0].ID != p.ID {
			t.Fatal("lost project history")
		}
		if _, err := f.tasks.Create(ctx, p.ID, "Blocked", ""); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("create on inactive: %v", err)
		}
		if _, err := f.tasks.Update(ctx, task.ID, TaskPatch{Version: task.Version}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("edit on inactive: %v", err)
		}
		if _, err := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "Demo work"}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("run on inactive: %v", err)
		}
		if _, err := f.projects.Register(ctx, "/repos/lifecycle", "Duplicate"); !errors.Is(err, domain.ErrDuplicate) {
			t.Fatalf("identity not reserved: %v", err)
		}
		if _, err := f.projects.SetLifecycle(ctx, p.ID, "active"); err != nil {
			t.Fatal(err)
		}
		kept, err := mem.Get(ctx, scope, memory.ID)
		if err != nil || kept.Summary != memory.Summary {
			t.Fatalf("lost memory: %+v %v", kept, err)
		}
		history, err = f.projects.History(ctx)
		if err != nil || len(history[0].Lifecycle) < 2 {
			t.Fatalf("lost lifecycle trail: %+v %v", history, err)
		}
		tasks, err := f.tasks.List(ctx, p.ID)
		if err != nil || len(tasks) != 1 || tasks[0].Description != "Past context" {
			t.Fatalf("lost work: %+v %v", tasks, err)
		}
	}
}

func TestProjectLifecycleRefusesActiveExecutionAndSchedules(t *testing.T) {
	f, _, pid, now := schedulingFixture(t)
	ctx := context.Background()
	task := arm(t, f, pid, "Scheduled", domain.Orchestration{ScheduledAt: func() *time.Time { v := now.Add(time.Hour); return &v }()})
	if _, err := f.projects.SetLifecycle(ctx, pid, "archived"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("scheduled archive: %v", err)
	}
	off := domain.Orchestration{}
	task, err := f.tasks.Update(ctx, task.ID, TaskPatch{Version: task.Version, Orchestration: &off})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "Demo work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.projects.SetLifecycle(ctx, pid, "deleted"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("active deletion: %v", err)
	}
}

func TestWorkProjectEventsDoNotCreateRepositoryRisks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.CreateWork(ctx, "Launch")
	if err != nil {
		t.Fatal(err)
	}
	health := &GitHealth{Deps: f.deps}
	if _, err := health.Refresh(ctx, p.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("work project health: %v", err)
	}
	err = f.deps.Store.View(ctx, func(tx store.Tx) error {
		_, err := tx.Health().GetCheck(ctx, p.ID)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("recorded a repository error for work: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
