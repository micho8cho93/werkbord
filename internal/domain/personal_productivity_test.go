package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func productivitySchema() PersonalCollectionSpec {
	return PersonalCollectionSpec{Name: "Notes", SchemaVersion: 1, Fields: []PersonalField{
		{Key: "title", Name: "Title", Type: "text", Required: true},
		{Key: "body", Name: "Body", Type: "longtext"},
		{Key: "parent", Name: "Parent", Type: "text"},
		{Key: "tags", Name: "Tags", Type: "multiselect", Options: []string{"idea", "plan"}},
		{Key: "language", Name: "Language", Type: "select", Options: []string{"Go", "Shell"}},
		{Key: "link", Name: "Link", Type: "url"},
		{Key: "due", Name: "Due", Type: "date"},
	}}
}

func productivityWorkspace() PersonalWorkspaceSpec {
	id := personalTestCollectionID
	return PersonalWorkspaceSpec{Name: "Notes", Icon: "auto", UISchemaVersion: 1, Visibility: PersonalVisibility{Individual: true}, CollectionIDs: []string{id}, Definition: PersonalUI{
		Views: []PersonalView{
			{ID: "doc", Name: "Documents", Kind: "document", CollectionID: id, Fields: []string{"title", "tags"}, BodyField: "body", Operations: []string{"create", "update", "delete"}},
			{ID: "pages", Name: "Pages", Kind: "pages", CollectionID: id, Fields: []string{"title", "tags"}, BodyField: "body", ParentField: "parent", Operations: []string{"create", "update", "delete"}},
			{ID: "code", Name: "Snippets", Kind: "snippets", CollectionID: id, Fields: []string{"title", "language"}, BodyField: "body", GroupBy: "language"},
			{ID: "files", Name: "Material", Kind: "files", CollectionID: id, Fields: []string{"title", "link"}, BodyField: "body"},
			{ID: "board", Name: "Plan", Kind: "board", CollectionID: id, Fields: []string{"title"}, GroupBy: "language", Operations: []string{"create", "update"}},
		}, Layout: PersonalNode{Type: "stack", Children: []PersonalNode{
			{Type: "view", ViewID: "doc"}, {Type: "shortcut", Target: "projects", Text: "Projects"}, {Type: "shortcut", Target: "analytics"},
		}},
	}}
}

func TestPersonalProductivityKindsAndShortcuts(t *testing.T) {
	schemas := map[string]PersonalCollectionSpec{personalTestCollectionID: productivitySchema()}
	if err := ValidatePersonalWorkspace(productivityWorkspace(), schemas); err != nil {
		t.Fatal(err)
	}
	for _, kind := range PersonalViewKinds() {
		if kind == "" {
			t.Fatal("empty kind")
		}
	}
	tests := map[string]func(*PersonalWorkspaceSpec){
		"document without a body": func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].BodyField = "" },
		"body that is short text": func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].BodyField = "parent" },
		"body that is the title":  func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].BodyField = "title" },
		"title that is long text": func(s *PersonalWorkspaceSpec) {
			s.Definition.Views[0].Fields = []string{"body"}
			s.Definition.Views[0].BodyField = "title"
		},
		"parent on a document":    func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].ParentField = "parent" },
		"parent that is the body": func(s *PersonalWorkspaceSpec) { s.Definition.Views[1].ParentField = "body" },
		"parent that is not text": func(s *PersonalWorkspaceSpec) { s.Definition.Views[1].ParentField = "due" },
		"body on a table": func(s *PersonalWorkspaceSpec) {
			s.Definition.Views[4].Kind = "table"
			s.Definition.Views[4].GroupBy = ""
			s.Definition.Views[4].BodyField = "body"
		},
		"snippets grouped by text":       func(s *PersonalWorkspaceSpec) { s.Definition.Views[2].GroupBy = "title" },
		"pages grouped":                  func(s *PersonalWorkspaceSpec) { s.Definition.Views[1].GroupBy = "language" },
		"files with a checked field":     func(s *PersonalWorkspaceSpec) { s.Definition.Views[3].CheckedField = "title" },
		"files named by long text":       func(s *PersonalWorkspaceSpec) { s.Definition.Views[3].Fields = []string{"body"} },
		"unknown shortcut":               func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Children[1].Target = "https://example.com" },
		"shortcut without a destination": func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Children[1].Target = "" },
		"shortcut with a view":           func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Children[1].ViewID = "doc" },
		"shortcut with children": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout.Children[1].Children = []PersonalNode{{Type: "text", Text: "x"}}
		},
		"overlong shortcut label": func(s *PersonalWorkspaceSpec) { s.Definition.Layout.Children[1].Text = strings.Repeat("x", 81) },
		"target on a text node": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "text", Text: "x", Target: "projects"}
		},
		"target on a view node": func(s *PersonalWorkspaceSpec) {
			s.Definition.Layout = PersonalNode{Type: "view", ViewID: "doc", Target: "projects"}
		},
		"create without a bound title": func(s *PersonalWorkspaceSpec) { s.Definition.Views[0].Fields = []string{"tags"} },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			w := productivityWorkspace()
			change(&w)
			if err := ValidatePersonalWorkspace(w, schemas); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
}

func TestPersonalLongTextAndURLValues(t *testing.T) {
	schema := productivitySchema()
	check := func(key string, value any) error {
		raw, _ := json.Marshal(value)
		return ValidatePersonalValues(schema, map[string]json.RawMessage{"title": json.RawMessage(`"t"`), key: raw})
	}
	for _, ok := range []struct {
		key   string
		value any
	}{
		{"body", "# Heading\n\n- [ ] a task\n\tindented\r\n"}, {"body", strings.Repeat("é", 20000)}, {"body", strings.Repeat("x", PersonalMaxLongTextBytes)},
		{"link", "https://example.com/a?b=c#d"}, {"link", "http://localhost:8080/"},
	} {
		if err := check(ok.key, ok.value); err != nil {
			t.Errorf("%s %.20q rejected: %v", ok.key, ok.value, err)
		}
	}
	for _, bad := range []struct {
		key   string
		value any
	}{
		{"body", strings.Repeat("x", PersonalMaxLongTextBytes+1)}, {"body", "a\x01b"}, {"body", "a\x7fb"}, {"body", "a\u0000b"}, {"body", 4}, {"body", []string{"x"}},
		{"link", "javascript:alert(1)"}, {"link", "data:text/html,<b>x</b>"}, {"link", "file:///etc/passwd"}, {"link", "ftp://example.com"}, {"link", "https://user:pw@example.com"},
		{"link", "https://exa mple.com"}, {"link", "https://"}, {"link", "example.com"}, {"link", ""}, {"link", "https://example.com/\n"}, {"link", "https://example.com/" + strings.Repeat("a", PersonalMaxURLBytes)},
	} {
		if err := check(bad.key, bad.value); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %.30q accepted: %v", bad.key, bad.value, err)
		}
	}
	// A record holding a long document may exceed a definition's 32 KiB, but not the record bound.
	long, _ := json.Marshal(strings.Repeat("x", PersonalMaxLongTextBytes))
	extra, _ := json.Marshal(strings.Repeat("y", 16000))
	if err := ValidatePersonalValues(schema, map[string]json.RawMessage{"title": json.RawMessage(`"t"`), "body": long, "parent": extra}); err != nil {
		t.Fatalf("a full document with a long title was refused: %v", err)
	}
}

func TestPersonalLongTextFieldIsPlainDataInSchemas(t *testing.T) {
	if err := ValidatePersonalCollection(productivitySchema()); err != nil {
		t.Fatal(err)
	}
	next := productivitySchema()
	next.Fields[1].Type = "text" // A field keeps its type: a text document cannot be quietly shortened.
	if err := ValidatePersonalEvolution(productivitySchema(), next); !errors.Is(err, ErrInvalid) {
		t.Fatal("a long text field was retyped", err)
	}
	withOptions := productivitySchema()
	withOptions.Fields[1].Options = []string{"x"}
	if err := ValidatePersonalCollection(withOptions); !errors.Is(err, ErrInvalid) {
		t.Fatal("options on long text were accepted", err)
	}
}
