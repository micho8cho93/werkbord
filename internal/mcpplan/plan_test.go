package mcpplan

import (
	"testing"

	"devboard/internal/planning"
)

func TestCleanTaskPlanAndTemporaryDependencies(t *testing.T) {
	title := "  Design  "
	plan := Plan{ProjectID: "project_1", IdempotencyKey: "request-1", Tasks: []Change{
		{TempID: "tmp_design", Title: &title, Plan: &planning.Range{Start: "2026-10-12", Milestone: true}},
		{TempID: "tmp_build", Title: ptr("Build"), Dependencies: ptr([]string{"tmp_design"})},
	}}
	clean, err := Clean(plan)
	if err != nil {
		t.Fatal(err)
	}
	if *clean.Tasks[0].Title != "Design" || (*clean.Tasks[1].Dependencies)[0] != "tmp_design" {
		t.Fatalf("plan was not canonicalized: %+v", clean)
	}
}

func TestCleanTaskPlanRejectsMissingTempIDAndBadRange(t *testing.T) {
	for name, plan := range map[string]Plan{
		"missing temp": {ProjectID: "p", IdempotencyKey: "r", Tasks: []Change{{TempID: "tmp_a", Title: ptr("A"), Dependencies: ptr([]string{"tmp_missing"})}}},
		"bad date":     {ProjectID: "p", IdempotencyKey: "r", Tasks: []Change{{TempID: "tmp_a", Title: ptr("A"), Plan: &planning.Range{Start: "next week"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Clean(plan); err == nil {
				t.Fatal("invalid plan was accepted")
			}
		})
	}
}

func ptr[T any](value T) *T { return &value }
