package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"werkbord/internal/domain"
	"werkbord/internal/workspace"
)

func TestWorkPlansReferenceTasksAndGuardConcurrentEdits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/app", "App")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Register(ctx, "/repos/other", "Other")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := f.tasks.Create(ctx, p.ID, "Design", "Decide interfaces")
	second, _ := f.tasks.Create(ctx, p.ID, "Build", "Implement interfaces")
	foreign, _ := f.tasks.Create(ctx, other.ID, "Other work", "")
	in := domain.WorkPlan{Title: "Ship interfaces", Description: "Coordinate delivery", TaskIDs: []string{second.ID, first.ID}, Decisions: "Keep API compatible", Questions: "Which clients need migration?"}
	saved, err := f.tasks.SaveWorkPlan(ctx, p.ID, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || saved.TaskIDs[0] != second.ID {
		t.Fatal(saved)
	}
	all, err := f.tasks.WorkPlans(ctx, p.ID)
	if err != nil || len(all) != 1 || all[0].Questions != in.Questions {
		t.Fatal(all, err)
	}
	// Saving organisational context must not create or execute tasks.
	tasks, _ := f.tasks.List(ctx, p.ID)
	runs, _ := f.runs.ListByTask(ctx, first.ID)
	if len(tasks) != 2 || len(runs) != 0 || tasks[0].Version != 1 {
		t.Fatal(tasks, runs)
	}
	updated := saved
	updated.TaskIDs = []string{first.ID, second.ID}
	updated.Decisions = "Use the agreed protocol"
	updated, err = f.tasks.SaveWorkPlan(ctx, p.ID, saved.ID, updated)
	if err != nil || updated.Version != 2 {
		t.Fatal(updated, err)
	}
	if _, err = f.tasks.SaveWorkPlan(ctx, p.ID, saved.ID, saved); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale write", err)
	}
	updated.TaskIDs = []string{foreign.ID}
	if _, err = f.tasks.SaveWorkPlan(ctx, p.ID, saved.ID, updated); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-project task", err)
	}
	updated.TaskIDs = []string{first.ID, first.ID}
	if _, err = f.tasks.SaveWorkPlan(ctx, p.ID, saved.ID, updated); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("duplicate task", err)
	}
	if _, err = f.tasks.SaveWorkPlan(ctx, other.ID, saved.ID, saved); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-project plan", err)
	}
	summary, err := (&ControlCenter{Deps: f.deps}).WorkspaceSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(summary)
	decoded, dropped, err := workspace.Decode(bytes.NewReader(raw), workspace.IndividualID)
	if err != nil || dropped != 0 || len(decoded.Queue) != 4 {
		t.Fatalf("summary: %v dropped=%d queue=%+v", err, dropped, decoded.Queue)
	}
}
