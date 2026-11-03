package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"werkbord/internal/domain"
)

// clock lets a test move time: every service of the fixture reads it.
func clock(f *fixture, start time.Time) *time.Time {
	now := start
	read := func() time.Time { return now }
	f.deps.Now, f.tasks.Now, f.projects.Now, f.runs.Now = read, read, read, read
	return &now
}

func finished(t *testing.T, f *fixture, projectID, title string) *domain.Task {
	t.Helper()
	ctx := context.Background()
	task, err := f.tasks.Create(ctx, projectID, title, "")
	if err != nil {
		t.Fatal(err)
	}
	done := domain.TaskDone
	task, err = f.tasks.Update(ctx, task.ID, TaskPatch{Version: task.Version, State: &done})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func archivedTask(t *testing.T, f *fixture, task *domain.Task) *domain.Task {
	t.Helper()
	yes := true
	out, err := f.tasks.Update(context.Background(), task.ID, TaskPatch{Version: task.Version, Archived: &yes})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestATaskIsArchivedBeforeItIsDeletedAndCanBeRestoredFromTheTrash(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, err := f.projects.Register(ctx, "/repos/trash", "Trash")
	if err != nil {
		t.Fatal(err)
	}
	task := finished(t, f, p.ID, "Ship it")

	if _, err := f.tasks.Trash(ctx, p.ID, task.ID, task.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting a task that is not archived: %v", err)
	}
	task = archivedTask(t, f, task)
	if _, err := f.tasks.Trash(ctx, p.ID, task.ID, task.Version+9); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a stale version: %v", err)
	}
	if _, err := f.tasks.Trash(ctx, "prj_other", task.ID, task.Version); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a task of another project: %v", err)
	}
	trashed, err := f.tasks.Trash(ctx, p.ID, task.ID, task.Version)
	if err != nil || trashed.DeletedAt == nil || trashed.ArchivedAt == nil || !trashed.DeletedAt.Equal(*now) {
		t.Fatalf("trash: %+v %v", trashed, err)
	}
	if err := f.tasks.Erase(ctx, p.ID, task.ID, trashed.Version+1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("erase with a stale version: %v", err)
	}

	back, err := f.tasks.Untrash(ctx, p.ID, task.ID, trashed.Version)
	if err != nil || back.DeletedAt != nil || back.ArchivedAt == nil {
		t.Fatalf("untrash goes back to the archive: %+v %v", back, err)
	}
	if _, err := f.tasks.Untrash(ctx, p.ID, task.ID, back.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("untrash of a task that is not deleted: %v", err)
	}
	if err := f.tasks.Erase(ctx, p.ID, task.ID, back.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a task must be deleted before it is erased: %v", err)
	}

	// Restoring a deleted task from the archive's own button restores it whole.
	again, _ := f.tasks.Trash(ctx, p.ID, task.ID, back.Version)
	no := false
	whole, err := f.tasks.Update(ctx, task.ID, TaskPatch{Version: again.Version, Archived: &no})
	if err != nil || whole.ArchivedAt != nil || whole.DeletedAt != nil {
		t.Fatalf("restore: %+v %v", whole, err)
	}
}

func TestErasingATaskRemovesItFromWhatOthersWaitFor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, _ := f.projects.Register(ctx, "/repos/erase", "Erase")
	gone := archivedTask(t, f, finished(t, f, p.ID, "Goes"))
	other, _ := f.tasks.Create(ctx, p.ID, "Waits", "")
	deps := domain.Orchestration{Dependencies: []string{gone.ID}}
	other, err := f.tasks.Update(ctx, other.ID, TaskPatch{Version: other.Version, Orchestration: &deps})
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := f.tasks.Trash(ctx, p.ID, gone.ID, gone.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Erase(ctx, p.ID, gone.ID, trashed.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Get(ctx, gone.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the task is still there: %v", err)
	}
	waiting, _ := f.tasks.Get(ctx, other.ID)
	if len(waiting.Orchestration.Dependencies) != 0 {
		t.Fatalf("still waiting on an erased task: %+v", waiting.Orchestration.Dependencies)
	}
}

func TestHousekeepingArchivesFinishedWorkAsTheProjectAsksAndErasesWhatTheTrashHeldLongEnough(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, _ := f.projects.Register(ctx, "/repos/house", "House")
	other, _ := f.projects.Register(ctx, "/repos/other", "Other")

	old := finished(t, f, p.ID, "Finished long ago")
	oldElsewhere := finished(t, f, other.ID, "Also old, but its project never archives")
	*now = now.Add(10 * 24 * time.Hour)
	recent := finished(t, f, p.ID, "Finished today")
	open, _ := f.tasks.Create(ctx, p.ID, "Still open", "")

	if _, err := f.tasks.SetArchiveSettings(ctx, p.ID, domain.ArchiveSettings{AfterDays: 5}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a period that is not offered: %v", err)
	}
	if _, err := f.tasks.SetArchiveSettings(ctx, p.ID, domain.ArchiveSettings{AfterDays: 7}); err != nil {
		t.Fatal(err)
	}
	archived, erased, err := f.tasks.Housekeep(ctx)
	if err != nil || archived != 1 || erased != 0 {
		t.Fatalf("housekeeping: %d archived, %d erased, %v", archived, erased, err)
	}
	for id, want := range map[string]bool{old.ID: true, recent.ID: false, open.ID: false, oldElsewhere.ID: false} {
		got, _ := f.tasks.Get(ctx, id)
		if (got.ArchivedAt != nil) != want {
			t.Fatalf("%s archived = %v, want %v", got.Title, got.ArchivedAt != nil, want)
		}
	}
	if a, e, _ := f.tasks.Housekeep(ctx); a != 0 || e != 0 {
		t.Fatalf("running it again does nothing: %d %d", a, e)
	}

	// A deleted task is kept for the retention period, and not a day less.
	got, _ := f.tasks.Get(ctx, old.ID)
	trashed, err := f.tasks.Trash(ctx, p.ID, old.ID, got.Version)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(domain.TrashRetention - time.Hour)
	if _, e, _ := f.tasks.Housekeep(ctx); e != 0 {
		t.Fatal("erased before its time")
	}
	*now = now.Add(2 * time.Hour)
	if _, e, err := f.tasks.Housekeep(ctx); err != nil || e != 1 {
		t.Fatalf("erased = %d, %v", e, err)
	}
	if _, err := f.tasks.Get(ctx, trashed.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
}

func TestAProjectIsArchivedThenDeletedThenErasedWithItsWork(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, _ := f.projects.Register(ctx, "/repos/gone", "Gone")
	keep, _ := f.projects.Register(ctx, "/repos/keep", "Keep")
	task, _ := f.tasks.Create(ctx, p.ID, "Work", "")
	kept, _ := f.tasks.Create(ctx, keep.ID, "Other work", "")
	mem := memoryService(t, f, "individual", domain.MemoryLimits{})
	scope := domain.MemoryScope{ProjectID: p.ID}
	memory := createMemory(t, mem, scope, memoryContent("A decision of the doomed project"))
	keepMemory := createMemory(t, mem, domain.MemoryScope{ProjectID: keep.ID}, memoryContent("A decision of the other project"))

	if err := f.projects.Erase(ctx, p.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("erasing an active project: %v", err)
	}
	if _, err := f.projects.SetLifecycle(ctx, p.ID, "archived"); err != nil {
		t.Fatal(err)
	}
	if err := f.projects.Erase(ctx, p.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("erasing an archived project: %v", err)
	}
	if _, err := f.projects.SetLifecycle(ctx, p.ID, "deleted"); err != nil {
		t.Fatal(err)
	}
	if n, err := f.projects.PurgeExpired(ctx); err != nil || n != 0 {
		t.Fatalf("purged too early: %d %v", n, err)
	}
	*now = now.Add(domain.TrashRetention + time.Hour)
	if n, err := f.projects.PurgeExpired(ctx); err != nil || n != 1 {
		t.Fatalf("purged = %d, %v", n, err)
	}
	if _, err := f.projects.Get(ctx, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the project is still there: %v", err)
	}
	if _, err := f.tasks.Get(ctx, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("its task is still there: %v", err)
	}
	if _, err := mem.Get(ctx, scope, memory.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("its memory is still there: %v", err)
	}
	// Nothing of another project goes with it.
	if _, err := f.tasks.Get(ctx, kept.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Get(ctx, domain.MemoryScope{ProjectID: keep.ID}, keepMemory.ID); err != nil {
		t.Fatal(err)
	}
	// The path is free again: erasing is the one way a project's identity is given back.
	if _, err := f.projects.Register(ctx, "/repos/gone", "Back"); err != nil {
		t.Fatalf("registering the erased project's path again: %v", err)
	}
}

func TestEmptyingTheTrashErasesAProjectAtOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, _ := f.projects.Register(ctx, "/repos/now", "Now")
	_, _ = f.projects.SetLifecycle(ctx, p.ID, "archived")
	_, _ = f.projects.SetLifecycle(ctx, p.ID, "deleted")
	if err := f.projects.Erase(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if h, _ := f.projects.History(ctx); len(h) != 0 {
		t.Fatalf("history: %+v", h)
	}
}

func TestErasingTakesTheRunsOfATaskAndOfAProjectWithIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := clock(f, time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	p, _ := f.projects.Register(ctx, "/repos/runs", "Runs")
	task := finished(t, f, p.ID, "Had a session")
	run, err := f.runs.Create(ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runs.End(ctx, run.ID, Ended{State: domain.RunFailed, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	task, _ = f.tasks.Get(ctx, task.ID)
	task = archivedTask(t, f, task)
	trashed, err := f.tasks.Trash(ctx, p.ID, task.ID, task.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.tasks.Erase(ctx, p.ID, task.ID, trashed.Version); err != nil {
		t.Fatalf("a task with a finished run: %v", err)
	}
	if _, err := f.runs.Get(ctx, run.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("its run is still there: %v", err)
	}

	second := finished(t, f, p.ID, "Another")
	run2, _ := f.runs.Create(ctx, NewRun{TaskID: second.ID, AgentID: "fake", Prompt: "Work"})
	if _, err := f.runs.End(ctx, run2.ID, Ended{State: domain.RunFailed, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	_, _ = f.projects.SetLifecycle(ctx, p.ID, "archived")
	_, _ = f.projects.SetLifecycle(ctx, p.ID, "deleted")
	*now = now.Add(domain.TrashRetention + time.Hour)
	if n, err := f.projects.PurgeExpired(ctx); err != nil || n != 1 {
		t.Fatalf("a project with runs: %d %v", n, err)
	}
	if _, err := f.runs.Get(ctx, run2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("its run is still there: %v", err)
	}
}
