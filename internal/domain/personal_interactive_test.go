package domain

import (
	"errors"
	"testing"
)

func TestPersonalInteractiveViews(t *testing.T) {
	schemas := map[string]PersonalCollectionSpec{personalTestCollectionID: personalTestSchema()}
	spec := personalTestWorkspace()
	spec.Definition.Views = []PersonalView{
		{ID: "todos", Name: "To-dos", Kind: "checklist", CollectionID: personalTestCollectionID, Fields: []string{"title", "due"}, CheckedField: "done", Operations: []string{"create", "update", "delete"}},
		{ID: "progress", Name: "Progress", Kind: "progress", CollectionID: personalTestCollectionID, Fields: []string{"done"}, CheckedField: "done"},
		{ID: "status", Name: "Status", Kind: "status", CollectionID: personalTestCollectionID, Fields: []string{"done"}, CheckedField: "done"},
		{ID: "chart", Name: "Chart", Kind: "chart", CollectionID: personalTestCollectionID, Fields: []string{"status"}, GroupBy: "status"},
	}
	spec.Definition.Layout = PersonalNode{Type: "card", Text: "Personal", Children: []PersonalNode{{Type: "view", ViewID: "todos"}}}
	if err := ValidatePersonalWorkspace(spec, schemas); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PersonalWorkspaceSpec){
		"unknown operation":       func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Operations = []string{"execute"} },
		"duplicate operation":     func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Operations = []string{"update", "update"} },
		"create missing required": func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Fields = []string{"due"} },
		"summary write":           func(s *PersonalWorkspaceSpec) { s.Definition.Views[1].Operations = []string{"update"} },
		"chart type":              func(s *PersonalWorkspaceSpec) { s.Definition.Views[3].GroupBy = "title" },
		"card binding":            func(s *PersonalWorkspaceSpec) { s.Definition.Layout.BindingID = "tasks" },
		"empty card":              func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Children = nil },
	} {
		t.Run(name, func(t *testing.T) {
			next := spec
			next.Definition.Views = append([]PersonalView(nil), spec.Definition.Views...)
			mutate(&next)
			if err := ValidatePersonalWorkspace(next, schemas); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted unsupported capability: %v", err)
			}
		})
	}
}
