package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/domain"
)

func TestArchivePreservesHistoryAndDisarmsAutomation(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	r := f.running(t)
	if _, err := f.runs.End(ctx, r.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	k, _ := f.tasks.Get(ctx, f.task.ID)
	o := domain.Orchestration{Enabled: true}
	k, err := f.tasks.Update(ctx, k.ID, TaskPatch{Version: k.Version, Orchestration: &o})
	if err != nil {
		t.Fatal(err)
	}
	f.drain()
	archived := true
	closed, err := f.tasks.Update(ctx, k.ID, TaskPatch{Version: k.Version, Archived: &archived})
	if err != nil {
		t.Fatal(err)
	}
	if closed.ArchivedAt == nil || closed.Orchestration.Enabled || closed.Description != k.Description || closed.State != k.State || closed.Version != k.Version+1 {
		t.Fatalf("closed = %+v", closed)
	}
	wantTypes(t, f.drain(), domain.EventTaskUpdated)
	history, err := f.runs.ListByTask(ctx, k.ID)
	if err != nil || len(history) != 1 || history[0].ID != r.ID {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if _, err := f.runs.Create(ctx, NewRun{TaskID: k.ID, AgentID: "fake", Prompt: "run closed work"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archived start: %v", err)
	}
	title := "edit closed work"
	if _, err := f.tasks.Update(ctx, k.ID, TaskPatch{Version: closed.Version, Title: &title}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archived edit: %v", err)
	}
	archived = false
	if _, err := f.tasks.Update(ctx, k.ID, TaskPatch{Version: k.Version, Archived: &archived}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale restore: %v", err)
	}
	restored, err := f.tasks.Update(ctx, k.ID, TaskPatch{Version: closed.Version, Archived: &archived})
	if err != nil || restored.ArchivedAt != nil || restored.Orchestration.Enabled || restored.State != k.State {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
}

func TestClearDoneIsAtomicAndLeavesOtherColumnsAlone(t *testing.T) {
	f := newRunFixture(t)
	ctx := context.Background()
	done := domain.TaskDone
	first, err := f.tasks.Update(ctx, f.task.ID, TaskPatch{Version: f.task.Version, State: &done})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := f.tasks.Create(ctx, f.project.ID, "Done with active run", "")
	active, err := f.runs.Create(ctx, NewRun{TaskID: second.ID, AgentID: "fake", Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	second, err = f.tasks.Update(ctx, second.ID, TaskPatch{Version: second.Version, State: &done})
	if err != nil {
		t.Fatal(err)
	}
	backlog, _ := f.tasks.Create(ctx, f.project.ID, "Keep on board", "")
	f.drain()
	if _, err := f.tasks.ArchiveDone(ctx, f.project.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("active batch: %v", err)
	}
	got, _ := f.tasks.Get(ctx, first.ID)
	if got.ArchivedAt != nil || got.Version != first.Version || len(f.drain()) != 0 {
		t.Fatal("a rejected batch changed data or published events")
	}
	if _, err := f.runs.End(ctx, active.ID, Ended{State: domain.RunStopped}); err != nil {
		t.Fatal(err)
	}
	f.drain()
	closed, err := f.tasks.ArchiveDone(ctx, f.project.ID)
	if err != nil || len(closed) != 2 {
		t.Fatalf("clear done = %+v, %v", closed, err)
	}
	wantTypes(t, f.drain(), domain.EventTaskUpdated, domain.EventTaskUpdated)
	got, _ = f.tasks.Get(ctx, backlog.ID)
	if got.ArchivedAt != nil {
		t.Fatal("cleared another column")
	}
	closed, err = f.tasks.ArchiveDone(ctx, f.project.ID)
	if err != nil || len(closed) != 0 {
		t.Fatalf("repeat = %+v, %v", closed, err)
	}
}

func TestArchivedScheduleCannotDispatch(t *testing.T) {
	f, scheduler, pid, _ := schedulingFixture(t)
	k := arm(t, f, pid, "Closed scheduled work", domain.Orchestration{})
	archive := true
	if _, err := f.tasks.Update(context.Background(), k.ID, TaskPatch{Version: k.Version, Archived: &archive}); err != nil {
		t.Fatal(err)
	}
	d := decisionFor(t, scheduler, pid, k.ID)
	if d.State != "blocked" {
		t.Fatalf("archive decision = %+v", d)
	}
}
