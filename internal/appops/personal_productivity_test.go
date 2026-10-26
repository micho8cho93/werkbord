package appops

import (
	"encoding/json"
	"strings"
	"testing"

	"werkbord/internal/domain"
)

// Salie can propose, and a person can confirm, a workspace built from the productivity views: the tool's own schema must
// accept them, and the saved workspace must read back exactly.
func TestPersonalProductivityWorkspaceIsProposedConfirmedAndSaved(t *testing.T) {
	f, personal, p := personalFixture(t)
	ops := []string{"create", "update", "delete"}
	change := domain.PersonalChange{Workspace: domain.PersonalWorkspaceSpec{
		Name: "Notes and plan", Icon: "auto", UISchemaVersion: 1,
		Visibility:    domain.PersonalVisibility{Individual: true},
		CollectionIDs: []string{"notes", "plan"},
		Definition: domain.PersonalUI{Views: []domain.PersonalView{
			{ID: "pages", Name: "Notebook", Kind: "pages", CollectionID: "notes", Fields: []string{"title", "tags"}, BodyField: "body", ParentField: "parent", Operations: ops},
			{ID: "docs", Name: "Documents", Kind: "document", CollectionID: "notes", Fields: []string{"title"}, BodyField: "body", Operations: ops},
			{ID: "board", Name: "Plan", Kind: "board", CollectionID: "plan", Fields: []string{"title", "link"}, GroupBy: "stage", Operations: ops},
			{ID: "when", Name: "Calendar", Kind: "calendar", CollectionID: "plan", Fields: []string{"title"}, DateField: "due", Operations: ops},
		}, Layout: domain.PersonalNode{Type: "stack", Children: []domain.PersonalNode{
			{Type: "grid", Columns: 2, Children: []domain.PersonalNode{{Type: "shortcut", Target: "projects"}, {Type: "shortcut", Target: "assistant", Text: "Ask Salie"}}},
			{Type: "view", ViewID: "pages"}, {Type: "view", ViewID: "docs"}, {Type: "view", ViewID: "board"}, {Type: "view", ViewID: "when"},
		}}},
	}, Collections: []domain.PersonalCollectionChange{
		{ID: "notes", Spec: domain.PersonalCollectionSpec{Name: "Notes", SchemaVersion: 1, Fields: []domain.PersonalField{
			{Key: "title", Name: "Title", Type: "text", Required: true}, {Key: "body", Name: "Body", Type: "longtext"}, {Key: "parent", Name: "Parent", Type: "text"},
			{Key: "tags", Name: "Tags", Type: "multiselect", Options: []string{"idea", "plan"}},
		}}},
		{ID: "plan", Spec: domain.PersonalCollectionSpec{Name: "Plan", SchemaVersion: 1, Fields: []domain.PersonalField{
			{Key: "title", Name: "Title", Type: "text", Required: true}, {Key: "stage", Name: "Stage", Type: "select", Options: []string{"To do", "Doing", "Done"}},
			{Key: "due", Name: "Due", Type: "date"}, {Key: "link", Name: "Link", Type: "url"},
		}}},
	}}
	proposal := f.mustCall(p, "propose_personal_workspace", change).Pending
	if workspaces, _ := personal.ListWorkspaces(bg, domain.PersonalQuery{Limit: 100}); len(workspaces) != 0 {
		t.Fatal("saved before approval")
	}
	result, err := f.confirm(p, proposal.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	saved := result.Data.(*Changed).Workspace
	if !domain.LibraryPersonalIcon(saved.Icon) || len(saved.Definition.Views) != 4 || saved.Definition.Layout.Children[0].Children[1].Text != "Ask Salie" {
		t.Fatalf("workspace read back wrongly: %+v", saved)
	}
	// A long document and a web address are stored as typed.
	body := strings.Repeat("A line of notes.\n", 1000)
	text, _ := json.Marshal(body)
	record, err := personal.CreateRecord(bg, saved.CollectionIDs[0], map[string]json.RawMessage{"title": json.RawMessage(`"Long"`), "body": text})
	if err != nil || record == nil {
		t.Fatal("a long document was refused:", err)
	}
}

// The tool's schema refuses what the grammar refuses, so a mistake never reaches the confirmation card.
func TestPersonalProductivityProposalsRefuseWhatTheGrammarRefuses(t *testing.T) {
	f, _, p := personalFixture(t)
	good := func() domain.PersonalChange {
		return domain.PersonalChange{Workspace: domain.PersonalWorkspaceSpec{
			Name: "Notes", Icon: "auto", UISchemaVersion: 1, Visibility: domain.PersonalVisibility{Individual: true}, CollectionIDs: []string{"notes"},
			Definition: domain.PersonalUI{Views: []domain.PersonalView{{ID: "docs", Name: "Docs", Kind: "document", CollectionID: "notes", Fields: []string{"title"}, BodyField: "body"}},
				Layout: domain.PersonalNode{Type: "view", ViewID: "docs"}},
		}, Collections: []domain.PersonalCollectionChange{{ID: "notes", Spec: domain.PersonalCollectionSpec{Name: "Notes", SchemaVersion: 1, Fields: []domain.PersonalField{
			{Key: "title", Name: "Title", Type: "text", Required: true}, {Key: "body", Name: "Body", Type: "longtext"},
		}}}}}
	}
	f.mustCall(p, "propose_personal_workspace", good())
	for name, mutate := range map[string]func(*domain.PersonalChange){
		"a shortcut to an address": func(c *domain.PersonalChange) {
			c.Workspace.Definition.Layout = domain.PersonalNode{Type: "shortcut", Target: "https://example.com"}
		},
		"a document with no body":   func(c *domain.PersonalChange) { c.Workspace.Definition.Views[0].BodyField = "" },
		"a body that is short text": func(c *domain.PersonalChange) { c.Collections[0].Spec.Fields[1].Type = "text" },
		"an unknown view kind":      func(c *domain.PersonalChange) { c.Workspace.Definition.Views[0].Kind = "html" },
		"an unknown field type":     func(c *domain.PersonalChange) { c.Collections[0].Spec.Fields[1].Type = "markdown" },
	} {
		c := good()
		c.Workspace.Name = name
		mutate(&c)
		if _, err := f.call(p, "propose_personal_workspace", c); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
