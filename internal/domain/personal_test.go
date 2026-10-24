package domain

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const personalTestCollectionID = "pcl_abcdefghijklmnop"

func personalTestSchema() PersonalCollectionSpec {
	return PersonalCollectionSpec{Name: "Campaigns", SchemaVersion: 1, Fields: []PersonalField{
		{Key: "title", Name: "Title", Type: "text", Required: true},
		{Key: "done", Name: "Done", Type: "boolean"},
		{Key: "status", Name: "Status", Type: "select", Options: []string{"todo", "done"}},
		{Key: "due", Name: "Due", Type: "date"},
		{Key: "cost", Name: "Cost", Type: "number"},
		{Key: "publish", Name: "Publish at", Type: "datetime"},
		{Key: "channels", Name: "Channels", Type: "multiselect", Options: []string{"web", "email"}},
	}}
}
func personalTestWorkspace() PersonalWorkspaceSpec {
	return PersonalWorkspaceSpec{Name: "Marketing", Icon: "calendar", UISchemaVersion: 1, Visibility: PersonalVisibility{Universal: true, Individual: true, TeamWorkspaceIDs: []string{"team:design", "tws_abcdefghijklmnop"}}, CollectionIDs: []string{personalTestCollectionID}, Definition: PersonalUI{
		Views: []PersonalView{
			{ID: "table", Name: "All", Kind: "table", CollectionID: personalTestCollectionID, Fields: []string{"title", "due"}},
			{ID: "board", Name: "Status", Kind: "board", CollectionID: personalTestCollectionID, Fields: []string{"title"}, GroupBy: "status"},
			{ID: "calendar", Name: "Calendar", Kind: "calendar", CollectionID: personalTestCollectionID, Fields: []string{"title"}, DateField: "due"},
			{ID: "checklist", Name: "Checklist", Kind: "checklist", CollectionID: personalTestCollectionID, Fields: []string{"title"}, CheckedField: "done"},
		}, Layout: PersonalNode{Type: "grid", Columns: 2, Children: []PersonalNode{{Type: "view", ViewID: "table"}, {Type: "view", ViewID: "board"}}},
	}}
}
func TestPersonalLayoutValidation(t *testing.T) {
	schemas := map[string]PersonalCollectionSpec{personalTestCollectionID: personalTestSchema()}
	if err := ValidatePersonalWorkspace(personalTestWorkspace(), schemas); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*PersonalWorkspaceSpec){
		"schema version":          func(s *PersonalWorkspaceSpec) { s.UISchemaVersion = 2 },
		"SVG icon":                func(s *PersonalWorkspaceSpec) { s.Icon = "<svg onload=alert(1)>" },
		"URL icon":                func(s *PersonalWorkspaceSpec) { s.Icon = "https://example.com/icon.svg" },
		"unknown icon":            func(s *PersonalWorkspaceSpec) { s.Icon = "random" },
		"location URL":            func(s *PersonalWorkspaceSpec) { s.Visibility.TeamWorkspaceIDs = []string{"https://example.com"} },
		"individual as Team":      func(s *PersonalWorkspaceSpec) { s.Visibility.TeamWorkspaceIDs = []string{"personal"} },
		"duplicate location":      func(s *PersonalWorkspaceSpec) { s.Visibility.TeamWorkspaceIDs = []string{"team:x", "team:x"} },
		"too many locations":      func(s *PersonalWorkspaceSpec) { s.Visibility.TeamWorkspaceIDs = make([]string, 65) },
		"duplicate collection":    func(s *PersonalWorkspaceSpec) { s.CollectionIDs = append(s.CollectionIDs, s.CollectionIDs[0]) },
		"invalid collection":      func(s *PersonalWorkspaceSpec) { s.CollectionIDs[0] = "tasks" },
		"absent collection":       func(s *PersonalWorkspaceSpec) { s.CollectionIDs[0] = NewID(PrefixPersonalCollection) },
		"duplicate view":          func(s *PersonalWorkspaceSpec) { s.Definition.Views = append(s.Definition.Views, s.Definition.Views[0]) },
		"unknown view kind":       func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Kind = "html" },
		"unknown field":           func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Fields = []string{"sql"} },
		"duplicate field":         func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Fields = []string{"title", "title"} },
		"wrong group type":        func(s *PersonalWorkspaceSpec) { s.Definition.Views[1].GroupBy = "title" },
		"wrong date type":         func(s *PersonalWorkspaceSpec) { s.Definition.Views[2].DateField = "title" },
		"wrong checkbox type":     func(s *PersonalWorkspaceSpec) { s.Definition.Views[3].CheckedField = "title" },
		"irrelevant view binding": func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].GroupBy = "status" },
		"unknown node":            func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Type = "script" },
		"missing view":            func(s *PersonalWorkspaceSpec) { s.Definition.Layout = PersonalNode{Type: "view", ViewID: "missing"} },
		"missing binding": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "binding", BindingID: "missing"}
		},
		"leaf children": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "text", Text: "hello", Children: []PersonalNode{{Type: "text", Text: "world"}}}
		},
		"empty container": func(s *PersonalWorkspaceSpec) { s.Definition.Layout = PersonalNode{Type: "stack"} },
		"grid columns":    func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Columns = 500 },
		"stack columns":   func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Type = "stack" },
		"text too long": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "heading", Text: strings.Repeat("a", 2001)}
		},
		"container text": func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Text = "a" },
		"deep": func(s *PersonalWorkspaceSpec) {
			n := PersonalNode{Type: "text", Text: "deep"}
			for range 9 {
				n = PersonalNode{Type: "stack", Children: []PersonalNode{n}}
			}
			s.Definition.Layout = n
		},
		"wide": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "stack"}
			for range 129 {
				s.Definition.Layout.Children = append(s.Definition.Layout.Children, PersonalNode{Type: "text", Text: "wide"})
			}
		},
		"external URL": func(s *PersonalWorkspaceSpec) {
			s.ReadBindings = []PersonalReadBinding{{ID: "external", Kind: "https://example.com", ProjectID: NewID(PrefixProject)}}
		},
		"external write": func(s *PersonalWorkspaceSpec) {
			s.ReadBindings = []PersonalReadBinding{{ID: "external", Kind: "individual.tasks.create", ProjectID: NewID(PrefixProject)}}
		},
		"binding project": func(s *PersonalWorkspaceSpec) {
			s.ReadBindings = []PersonalReadBinding{{ID: "external", Kind: "individual.tasks", ProjectID: "../secrets"}}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			s := personalTestWorkspace()
			change(&s)
			if err := ValidatePersonalWorkspace(s, schemas); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted malformed workspace: %v", err)
			}
		})
	}
	s := personalTestWorkspace()
	s.ReadBindings = []PersonalReadBinding{{ID: "tasks", Kind: "individual.tasks", ProjectID: NewID(PrefixProject)}}
	s.Definition.Layout = PersonalNode{Type: "binding", BindingID: "tasks"}
	if err := ValidatePersonalWorkspace(s, schemas); err != nil {
		t.Fatal(err)
	}
}
func TestPersonalTypedValuesAndEvolution(t *testing.T) {
	schema := personalTestSchema()
	if err := ValidatePersonalCollection(schema); err != nil {
		t.Fatal(err)
	}
	var valid map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"title":"<script>plain text</script>","done":false,"status":"todo","due":"2028-02-29","cost":1.5,"publish":"2026-10-23T12:00:00Z","channels":["web","email"]}`), &valid)
	if err := ValidatePersonalValues(schema, valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `null`, `{"title":null}`, `{"title":42}`, `{"title":"x","unknown":true}`, `{"title":"x","done":1}`, `{"title":"x","cost":"1"}`, `{"title":"x","due":"2026-02-29"}`, `{"title":"x","publish":"tomorrow"}`, `{"title":"x","status":"other"}`, `{"title":"x","channels":["web","web"]}`, `{"title":"x","channels":["other"]}`, `{"title":"x","cost":1e400}`, `{"title":"x","channels":{}}`} {
		var values map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &values)
		if err := ValidatePersonalValues(schema, values); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
	for name, change := range map[string]func(*PersonalCollectionSpec){
		"remove":        func(s *PersonalCollectionSpec) { s.Fields = s.Fields[1:] },
		"retype":        func(s *PersonalCollectionSpec) { s.Fields[0].Type = "number" },
		"strengthen":    func(s *PersonalCollectionSpec) { s.Fields[1].Required = true },
		"remove choice": func(s *PersonalCollectionSpec) { s.Fields[2].Options = []string{"done"} },
		"add required": func(s *PersonalCollectionSpec) {
			s.Fields = append(s.Fields, PersonalField{Key: "new", Name: "New", Type: "text", Required: true})
		},
	} {
		t.Run(name, func(t *testing.T) {
			next := personalTestSchema()
			change(&next)
			if err := ValidatePersonalEvolution(schema, next); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	next := personalTestSchema()
	next.Name = "Renamed"
	next.Fields[0].Required = false
	next.Fields[2].Options = append(next.Fields[2].Options, "paused")
	next.Fields = append(next.Fields, PersonalField{Key: "notes", Name: "Notes", Type: "text"})
	if err := ValidatePersonalEvolution(schema, next); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePersonalValues(next, valid); err != nil {
		t.Fatal(err)
	}
}
func TestPersonalCollectionMalformedSchemas(t *testing.T) {
	for name, change := range map[string]func(*PersonalCollectionSpec){
		"version": func(s *PersonalCollectionSpec) { s.SchemaVersion = 0 }, "empty": func(s *PersonalCollectionSpec) { s.Fields = nil }, "duplicate": func(s *PersonalCollectionSpec) { s.Fields = append(s.Fields, s.Fields[0]) }, "SQL key": func(s *PersonalCollectionSpec) { s.Fields[0].Key = "x); DROP TABLE tasks;" }, "unknown type": func(s *PersonalCollectionSpec) { s.Fields[0].Type = "html" }, "options on text": func(s *PersonalCollectionSpec) { s.Fields[0].Options = []string{"x"} }, "no options": func(s *PersonalCollectionSpec) { s.Fields[2].Options = nil }, "duplicate options": func(s *PersonalCollectionSpec) { s.Fields[2].Options = []string{"x", "x"} }, "too large": func(s *PersonalCollectionSpec) { s.Name = strings.Repeat("x", PersonalMaxDocumentBytes) },
	} {
		t.Run(name, func(t *testing.T) {
			s := personalTestSchema()
			change(&s)
			if err := ValidatePersonalCollection(s); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
}
func TestPersonalIdentifiersAndIcons(t *testing.T) {
	for _, prefix := range []string{PrefixPersonalWorkspace, PrefixPersonalCollection, PrefixPersonalRecord, PrefixProject} {
		if !ValidPersonalID(NewID(prefix), prefix) {
			t.Fatal(prefix)
		}
	}
	for _, id := range []string{"", "pws_abc", "pws_abcdefghijklmnop/../tasks", "pws_abcdefghijklmnop'", "pws_0000000000000000", "pcl_abcdefghijklmnop"} {
		if ValidPersonalID(id, PrefixPersonalWorkspace) {
			t.Fatal(id)
		}
	}
	b, err := os.ReadFile("../../web/src/lib/personal/icon-components.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, icon := range PersonalIcons() {
		if !strings.Contains(string(b), "'"+icon+"':") {
			t.Fatalf("unregistered icon %s", icon)
		}
	}
}

func TestPersonalDocumentSizeLimits(t *testing.T) {
	schema := PersonalCollectionSpec{Name: "Large", SchemaVersion: 1, Fields: []PersonalField{
		{Key: "a", Name: "A", Type: "text"}, {Key: "b", Name: "B", Type: "text"}, {Key: "c", Name: "C", Type: "text"},
		{Key: "d", Name: "D", Type: "text"}, {Key: "e", Name: "E", Type: "text"},
	}}
	value, _ := json.Marshal(strings.Repeat("x", 15000))
	if err := ValidatePersonalValues(schema, map[string]json.RawMessage{"a": value, "b": value, "c": value, "d": value, "e": value}); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized value object accepted", err)
	}
	s := personalTestWorkspace()
	s.Definition.Layout = PersonalNode{Type: "stack"}
	for range 20 {
		s.Definition.Layout.Children = append(s.Definition.Layout.Children, PersonalNode{Type: "text", Text: strings.Repeat("x", 1900)})
	}
	if err := ValidatePersonalWorkspace(s, map[string]PersonalCollectionSpec{personalTestCollectionID: personalTestSchema()}); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized definition accepted", err)
	}
}
