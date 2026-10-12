package service

import (
	"context"
	"errors"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/mcpplan"
	"devboard/internal/planning"
)

func TestMCPTaskPlanPreviewApplyAndIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.projects.CreateWork(ctx, "Batch planning")
	if err != nil {
		t.Fatal(err)
	}
	start := planning.Range{End: "2026-10-16"}
	dependent := planning.Range{Start: "2026-10-15"}
	plan := mcpplan.Plan{ProjectID: project.ID, IdempotencyKey: "client-request-1", Tasks: []mcpplan.Change{
		{TempID: "tmp_design", Title: ptr("Design"), Plan: &start},
		{TempID: "tmp_build", Title: ptr("Build"), Plan: &dependent, Dependencies: ptr([]string{"tmp_design"})},
	}}
	preview, err := f.tasks.PreviewMCPTaskPlan(ctx, plan)
	if err != nil || len(preview.Warnings) != 1 || preview.Warnings[0].Code != planning.CodeStartsBeforeDependent {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	first, err := f.tasks.ApplyMCPTaskPlan(ctx, "mcp:grant-1", plan)
	if err != nil || len(first.Created) != 2 || first.TempIDs["tmp_design"] == "" || first.TempIDs["tmp_build"] == "" {
		t.Fatalf("apply = %+v, %v", first, err)
	}
	second, err := f.tasks.ApplyMCPTaskPlan(ctx, "mcp:grant-1", plan)
	if err != nil || len(second.Created) != 2 || second.Created[0] != first.Created[0] || second.Created[1] != first.Created[1] {
		t.Fatalf("retry was not idempotent: first=%+v second=%+v err=%v", first, second, err)
	}
	all, err := f.tasks.List(ctx, project.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("retry created duplicate tasks: tasks=%d err=%v", len(all), err)
	}
	if all[1].Orchestration.Dependencies[0] != all[0].ID {
		t.Fatalf("temporary dependency was not resolved: %+v", all)
	}
	changed := plan
	changed.Tasks = append([]mcpplan.Change(nil), plan.Tasks...)
	changedTitle := "Different build"
	changed.Tasks[1].Title = &changedTitle
	if _, err := f.tasks.ApplyMCPTaskPlan(ctx, "mcp:grant-1", changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("reused idempotency key with different content: %v", err)
	}
}

func TestMCPTaskPlanRejectsCyclesMissingDependenciesAndStaleVersionsAtomically(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.projects.CreateWork(ctx, "Batch validation")
	if err != nil {
		t.Fatal(err)
	}
	cycle := mcpplan.Plan{ProjectID: project.ID, IdempotencyKey: "cycle", Tasks: []mcpplan.Change{
		{TempID: "tmp_a", Title: ptr("A"), Dependencies: ptr([]string{"tmp_b"})},
		{TempID: "tmp_b", Title: ptr("B"), Dependencies: ptr([]string{"tmp_a"})},
	}}
	if _, err := f.tasks.PreviewMCPTaskPlan(ctx, cycle); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("dependency cycle was accepted: %v", err)
	}
	missing := mcpplan.Plan{ProjectID: project.ID, IdempotencyKey: "missing", Tasks: []mcpplan.Change{
		{TempID: "tmp_a", Title: ptr("A"), Dependencies: ptr([]string{"tsk_missing"})},
	}}
	if _, err := f.tasks.PreviewMCPTaskPlan(ctx, missing); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("missing dependency was accepted: %v", err)
	}
	sourceRef := "mail:meeting-42"
	withSource := mcpplan.Plan{ProjectID: project.ID, IdempotencyKey: "source", Tasks: []mcpplan.Change{
		{TempID: "tmp_source", Title: ptr("From a source"), SourceRef: &sourceRef},
	}}
	if _, err := f.tasks.PreviewMCPTaskPlan(ctx, withSource); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("source reference was accepted on a work project: %v", err)
	}
	existing, err := f.tasks.Create(ctx, project.ID, "Existing", "")
	if err != nil {
		t.Fatal(err)
	}
	stale := existing.Version
	newTitle := "Changed elsewhere"
	if _, err := f.tasks.Update(ctx, existing.ID, TaskPatch{Version: existing.Version, Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	newTask := mcpplan.Change{TempID: "tmp_new", Title: ptr("Would roll back")}
	stalePlan := mcpplan.Plan{ProjectID: project.ID, IdempotencyKey: "stale", Tasks: []mcpplan.Change{
		newTask, {TaskID: existing.ID, Version: stale, Title: ptr("Stale edit")},
	}}
	if _, err := f.tasks.ApplyMCPTaskPlan(ctx, "mcp:grant-2", stalePlan); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale version was accepted: %v", err)
	}
	all, err := f.tasks.List(ctx, project.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("failed batch partially applied: tasks=%d err=%v", len(all), err)
	}
}

func ptr[T any](v T) *T { return &v }
