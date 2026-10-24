package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PersonalSchemaVersion    = 1
	PersonalMaxDocumentBytes = 32 << 10
	// A record holding a long text (a note, a page, a snippet) may be larger than a definition. Both stay far below what the
	// desktop bridge and the phone relay carry, so a whole page of records always loads in one answer.
	PersonalMaxRecordBytes   = 64 << 10
	PersonalMaxLongTextBytes = 48000
	PersonalMaxURLBytes      = 2000
	PersonalMaxPage          = 100
	PrefixPersonalWorkspace  = "pws"
	PrefixPersonalCollection = "pcl"
	PrefixPersonalRecord     = "prec"
)

// PersonalMeta is controller-assigned. Revisions guard every replacement and deletion.
type PersonalMeta struct {
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type PersonalWorkspace struct {
	PersonalMeta
	PersonalWorkspaceSpec
}
type PersonalWorkspaceSpec struct {
	Name            string                `json:"name"`
	Icon            string                `json:"icon"`
	UISchemaVersion int                   `json:"uiSchemaVersion"`
	Definition      PersonalUI            `json:"definition"`
	Visibility      PersonalVisibility    `json:"visibility"`
	CollectionIDs   []string              `json:"collectionIds"`
	ReadBindings    []PersonalReadBinding `json:"readBindings"`
}

// TeamWorkspaceIDs are opaque display associations, never authority or destinations.
type PersonalVisibility struct {
	Universal        bool     `json:"universal"`
	Individual       bool     `json:"individual"`
	TeamWorkspaceIDs []string `json:"teamWorkspaceIds"`
}

// Bindings declare approved reads outside personal collections. Phase 1 stores
// references only; no resolver, network capability, credential or write is granted.
type PersonalReadBinding struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // individual.project or individual.tasks
	ProjectID string `json:"projectId"`
}
type PersonalUI struct {
	Views  []PersonalView `json:"views"`
	Layout PersonalNode   `json:"layout"`
}
type PersonalView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	CollectionID string   `json:"collectionId"`
	Fields       []string `json:"fields"`
	GroupBy      string   `json:"groupBy,omitempty"`
	DateField    string   `json:"dateField,omitempty"`
	CheckedField string   `json:"checkedField,omitempty"`
	// BodyField is the long text a document, page or snippet is written in; ParentField is the text field a page keeps
	// its parent page's record ID in. Both belong to the document-style kinds only.
	BodyField   string `json:"bodyField,omitempty"`
	ParentField string `json:"parentField,omitempty"`
	// Empty means read-only. These are personal-record operations, never appops.
	Operations []string         `json:"operations,omitempty"`
	Filters    []PersonalFilter `json:"filters,omitempty"`
}

type PersonalFilter struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// This is data, not OpenUI conversation source. There are no arbitrary props,
// expressions, markup, style, actions, URLs or executable components.
type PersonalNode struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ViewID    string `json:"viewId,omitempty"`
	BindingID string `json:"bindingId,omitempty"`
	// Target is a shortcut's destination: one of PersonalShortcutTargets, never an address.
	Target   string         `json:"target,omitempty"`
	Columns  int            `json:"columns,omitempty"`
	Children []PersonalNode `json:"children,omitempty"`
}
type PersonalCollection struct {
	PersonalMeta
	PersonalCollectionSpec
}
type PersonalCollectionSpec struct {
	Name          string          `json:"name"`
	SchemaVersion int             `json:"schemaVersion"`
	Fields        []PersonalField `json:"fields"`
}
type PersonalField struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}
type PersonalRecord struct {
	PersonalMeta
	CollectionID string                     `json:"collectionId"`
	Values       map[string]json.RawMessage `json:"values"`
}
type PersonalQuery struct {
	After string
	Limit int
}

var personalID = regexp.MustCompile(`^(pws|pcl|prec|prj)_[a-z2-7]{16}$`)
var personalKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Matches the generic desktop workspace identifier contract without importing Team.
var personalLocation = regexp.MustCompile(`^(team:)?[A-Za-z0-9][A-Za-z0-9_-]{0,119}$`)

func ValidPersonalID(id, prefix string) bool {
	return strings.HasPrefix(id, prefix+"_") && personalID.MatchString(id)
}
func personalInvalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func personalText(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsRune(s, 0)
}
func personalSize(v any) bool { return personalSizeWithin(v, PersonalMaxDocumentBytes) }
func personalSizeWithin(v any, limit int) bool {
	b, e := json.Marshal(v)
	return e == nil && len(b) <= limit
}

// personalLongText accepts what people write in a document: text with line breaks and tabs, but no other control characters.
func personalLongText(s string) bool {
	if !utf8.ValidString(s) || len(s) > PersonalMaxLongTextBytes {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' || r == 0x7f {
			return false
		}
	}
	return true
}

// personalURL accepts a web address a person can open: http or https, with a host, no credentials, no spaces.
func personalURL(s string) bool {
	if len(s) == 0 || len(s) > PersonalMaxURLBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}

// PersonalShortcutTargets are the places a shortcut can open. They are names the window resolves itself, not addresses.
func PersonalShortcutTargets() []string {
	return []string{"home", "projects", "analytics", "calendar", "data-library", "settings", "assistant"}
}

// PersonalViewKinds lists every kind of view a workspace can show.
func PersonalViewKinds() []string {
	return []string{"table", "list", "checklist", "progress", "status", "chart", "board", "calendar", "document", "pages", "snippets", "files"}
}

// PersonalIcons is the finite set of locally bundled Lucide identifiers.
func PersonalIcons() []string {
	icons := make([]string, len(personalIconCatalogue))
	for i, icon := range personalIconCatalogue {
		icons[i] = "lucide-" + icon.Name
	}
	return icons
}
func inPersonalList(s string, values []string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func ValidatePersonalQuery(q PersonalQuery, prefix string) error {
	if q.Limit < 1 || q.Limit > PersonalMaxPage || q.After != "" && !ValidPersonalID(q.After, prefix) {
		return personalInvalid("invalid pagination")
	}
	return nil
}
func ValidatePersonalCollection(s PersonalCollectionSpec) error {
	if !personalText(s.Name, 120) || s.SchemaVersion != 1 || len(s.Fields) < 1 || len(s.Fields) > 64 || !personalSize(s) {
		return personalInvalid("invalid collection schema")
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if !personalKey.MatchString(f.Key) || seen[f.Key] || !personalText(f.Name, 120) {
			return personalInvalid("invalid or duplicate field")
		}
		seen[f.Key] = true
		switch f.Type {
		case "text", "longtext", "url", "number", "boolean", "date", "datetime":
			if len(f.Options) != 0 {
				return personalInvalid("options require a selection field")
			}
		case "select", "multiselect":
			if len(f.Options) < 1 || len(f.Options) > 100 {
				return personalInvalid("selection needs 1 to 100 options")
			}
			opts := map[string]bool{}
			for _, o := range f.Options {
				if !personalText(o, 120) || opts[o] {
					return personalInvalid("invalid or duplicate option")
				}
				opts[o] = true
			}
		default:
			return personalInvalid("unknown field type")
		}
	}
	return nil
}

// Evolution never invalidates a saved record or a view. Required fields may be
// relaxed, choices added, and optional fields appended; destructive migrations
// require an explicit future migration operation.
func ValidatePersonalEvolution(old, next PersonalCollectionSpec) error {
	if err := ValidatePersonalCollection(next); err != nil {
		return err
	}
	fields := map[string]PersonalField{}
	for _, f := range next.Fields {
		fields[f.Key] = f
	}
	previous := map[string]bool{}
	for _, f := range old.Fields {
		previous[f.Key] = true
		n, ok := fields[f.Key]
		if !ok || f.Type != n.Type || !f.Required && n.Required {
			return personalInvalid("incompatible collection schema change")
		}
		for _, o := range f.Options {
			if !inPersonalList(o, n.Options) {
				return personalInvalid("cannot remove a selection option")
			}
		}
	}
	for _, f := range next.Fields {
		if !previous[f.Key] && f.Required {
			return personalInvalid("new fields must be optional")
		}
	}
	return nil
}
func ValidatePersonalValues(schema PersonalCollectionSpec, values map[string]json.RawMessage) error {
	if values == nil || !personalSizeWithin(values, PersonalMaxRecordBytes) {
		return personalInvalid("values must be a bounded object")
	}
	fields := map[string]PersonalField{}
	for _, f := range schema.Fields {
		fields[f.Key] = f
		if f.Required {
			if _, ok := values[f.Key]; !ok {
				return personalInvalid("missing required field: " + f.Key)
			}
		}
	}
	for key, raw := range values {
		f, ok := fields[key]
		if !ok {
			return personalInvalid("unknown field: " + key)
		}
		var value any
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return personalInvalid("invalid field value: " + key)
		}
		valid := false
		switch f.Type {
		case "text":
			v, ok := value.(string)
			valid = ok && utf8.ValidString(v) && len(v) <= 16000 && !strings.ContainsRune(v, 0)
		case "longtext":
			v, ok := value.(string)
			valid = ok && personalLongText(v)
		case "url":
			v, ok := value.(string)
			valid = ok && personalURL(v)
		case "number":
			v, ok := value.(float64)
			valid = ok && !math.IsInf(v, 0) && !math.IsNaN(v)
		case "boolean":
			_, valid = value.(bool)
		case "date":
			v, ok := value.(string)
			_, err := time.Parse("2006-01-02", v)
			valid = ok && len(v) == 10 && err == nil
		case "datetime":
			v, ok := value.(string)
			_, err := time.Parse(time.RFC3339Nano, v)
			valid = ok && len(v) <= 40 && err == nil
		case "select":
			v, ok := value.(string)
			valid = ok && inPersonalList(v, f.Options)
		case "multiselect":
			list, ok := value.([]any)
			valid = ok && len(list) <= len(f.Options)
			seen := map[string]bool{}
			for _, item := range list {
				v, ok := item.(string)
				if !ok || seen[v] || !inPersonalList(v, f.Options) {
					valid = false
				}
				seen[v] = true
			}
		}
		if !valid {
			return personalInvalid("wrong type or value for field: " + key)
		}
	}
	return nil
}

// ValidatePersonalWorkspace checks the complete closed grammar and all bindings
// against the collections fetched in the same controller transaction.
func ValidatePersonalWorkspace(s PersonalWorkspaceSpec, collections map[string]PersonalCollectionSpec) error {
	if !personalText(s.Name, 120) || !ValidPersonalIcon(s.Icon) || s.UISchemaVersion != PersonalSchemaVersion || !personalSize(s) {
		return personalInvalid("invalid workspace name, icon, version or size")
	}
	if len(s.Visibility.TeamWorkspaceIDs) > 64 || len(s.CollectionIDs) > 32 || len(s.ReadBindings) > 16 || len(s.Definition.Views) > 32 {
		return personalInvalid("too many locations or references")
	}
	locations := map[string]bool{}
	for _, id := range s.Visibility.TeamWorkspaceIDs {
		if !personalLocation.MatchString(id) || locations[id] || id == "individual" || id == "universal" || id == "personal" {
			return personalInvalid("invalid or duplicate display location")
		}
		locations[id] = true
	}
	refs := map[string]bool{}
	for _, id := range s.CollectionIDs {
		if !ValidPersonalID(id, PrefixPersonalCollection) || refs[id] {
			return personalInvalid("invalid or duplicate collection reference")
		}
		if _, ok := collections[id]; !ok {
			return personalInvalid("collection is unavailable")
		}
		refs[id] = true
	}
	bindings := map[string]bool{}
	for _, b := range s.ReadBindings {
		if !personalKey.MatchString(b.ID) || bindings[b.ID] || !ValidPersonalID(b.ProjectID, PrefixProject) || b.Kind != "individual.project" && b.Kind != "individual.tasks" {
			return personalInvalid("invalid read binding")
		}
		bindings[b.ID] = true
	}
	views := map[string]bool{}
	for _, v := range s.Definition.Views {
		if !personalKey.MatchString(v.ID) || views[v.ID] || !personalText(v.Name, 120) || !refs[v.CollectionID] || len(v.Fields) < 1 || len(v.Fields) > 64 {
			return personalInvalid("invalid view")
		}
		views[v.ID] = true
		fields := map[string]PersonalField{}
		for _, f := range collections[v.CollectionID].Fields {
			fields[f.Key] = f
		}
		seen := map[string]bool{}
		for _, key := range v.Fields {
			if _, ok := fields[key]; !ok || seen[key] {
				return personalInvalid("invalid view field")
			}
			seen[key] = true
		}
		if len(v.Filters) > 16 {
			return personalInvalid("too many saved filters")
		}
		filterFields := map[string]bool{}
		for _, filter := range v.Filters {
			f, exists := fields[filter.Field]
			if !exists || filterFields[filter.Field] || len(filter.Value) > 16000 {
				return personalInvalid("invalid saved filter")
			}
			filterFields[filter.Field] = true
			raw, _ := json.Marshal(filter.Value)
			if f.Type == "number" || f.Type == "boolean" {
				raw = json.RawMessage(filter.Value)
			}
			f.Required = false
			if f.Type == "multiselect" || f.Type == "longtext" {
				return personalInvalid("equality filters are unsupported for this field type")
			}
			if err := ValidatePersonalValues(PersonalCollectionSpec{Fields: []PersonalField{f}}, map[string]json.RawMessage{f.Key: raw}); err != nil {
				return err
			}
		}
		ops := map[string]bool{}
		for _, op := range v.Operations {
			if !inPersonalList(op, []string{"create", "update", "delete"}) || ops[op] {
				return personalInvalid("invalid or duplicate personal record operation")
			}
			ops[op] = true
		}
		// A view also writes the fields its kind binds (the column a card moves between, the body a document is written in).
		bound := map[string]bool{v.CheckedField: true, v.GroupBy: true, v.DateField: true, v.BodyField: true, v.ParentField: true}
		if ops["create"] {
			for key, f := range fields {
				if f.Required && !seen[key] && !bound[key] {
					return personalInvalid("create view must include required fields")
				}
			}
		}
		if inPersonalList(v.Kind, []string{"progress", "status", "chart"}) && len(v.Operations) != 0 {
			return personalInvalid("summary views are read-only")
		}
		// Only the document-style kinds are written in a body; only pages have a parent.
		document := inPersonalList(v.Kind, []string{"document", "pages", "snippets", "files"})
		if !document && (v.BodyField != "" || v.ParentField != "") || v.Kind != "pages" && v.ParentField != "" {
			return personalInvalid("unexpected document binding")
		}
		textual := func(key string, types ...string) bool { return inPersonalList(fields[key].Type, types) }
		switch v.Kind {
		case "table", "list":
			if v.GroupBy != "" || v.DateField != "" || v.CheckedField != "" {
				return personalInvalid("unexpected view binding")
			}
		case "board":
			if fields[v.GroupBy].Type != "select" || v.DateField != "" || v.CheckedField != "" {
				return personalInvalid("board needs a select group field")
			}
		case "calendar":
			if fields[v.DateField].Type != "date" && fields[v.DateField].Type != "datetime" || v.GroupBy != "" || v.CheckedField != "" {
				return personalInvalid("calendar needs a date field")
			}
		case "checklist", "progress", "status":
			if fields[v.CheckedField].Type != "boolean" || v.GroupBy != "" || v.DateField != "" {
				return personalInvalid("checklist needs a boolean field")
			}
		case "chart":
			if fields[v.GroupBy].Type != "select" || v.DateField != "" || v.CheckedField != "" || len(fields[v.GroupBy].Options) > 20 {
				return personalInvalid("chart needs a select field with at most 20 choices")
			}
		case "document", "pages", "snippets":
			// The first field is the title; the body is the long text it is written in.
			if !textual(v.Fields[0], "text") || !textual(v.BodyField, "longtext") || v.BodyField == v.Fields[0] || v.DateField != "" || v.CheckedField != "" {
				return personalInvalid(v.Kind + " needs a text title as its first field and a long text body")
			}
			if v.Kind == "snippets" {
				if v.GroupBy != "" && fields[v.GroupBy].Type != "select" {
					return personalInvalid("snippets group by a select field")
				}
			} else if v.GroupBy != "" {
				return personalInvalid("unexpected view binding")
			}
			if v.ParentField != "" && (!textual(v.ParentField, "text") || v.ParentField == v.Fields[0] || v.ParentField == v.BodyField) {
				return personalInvalid("a page's parent is kept in a separate text field")
			}
		case "files":
			// A file catalogue: the first field names each file; notes are optional. The files themselves stay on the device.
			if !textual(v.Fields[0], "text") || v.BodyField != "" && !textual(v.BodyField, "longtext") || v.GroupBy != "" && fields[v.GroupBy].Type != "select" || v.DateField != "" || v.CheckedField != "" {
				return personalInvalid("files need a text name as their first field")
			}
		default:
			return personalInvalid("unknown view kind")
		}
	}
	count := 0
	var walk func(PersonalNode, int) error
	walk = func(n PersonalNode, depth int) error {
		count++
		if depth > 8 || count > 128 {
			return personalInvalid("layout is too deep or large")
		}
		switch n.Type {
		case "shortcut":
			if !inPersonalList(n.Target, PersonalShortcutTargets()) || n.Text != "" && !personalText(n.Text, 80) || n.ViewID != "" || n.BindingID != "" || n.Columns != 0 || len(n.Children) != 0 {
				return personalInvalid("invalid shortcut")
			}
		case "stack", "grid":
			if n.Target != "" || n.Text != "" || n.ViewID != "" || n.BindingID != "" || len(n.Children) < 1 || n.Type == "stack" && n.Columns != 0 || n.Type == "grid" && (n.Columns < 2 || n.Columns > 4) {
				return personalInvalid("invalid container")
			}
		case "card":
			if n.Target != "" || !personalText(n.Text, 160) || n.ViewID != "" || n.BindingID != "" || n.Columns != 0 || len(n.Children) < 1 {
				return personalInvalid("invalid card")
			}
		case "text", "heading":
			if n.Target != "" || !personalText(n.Text, 2000) || n.ViewID != "" || n.BindingID != "" || n.Columns != 0 || len(n.Children) != 0 {
				return personalInvalid("invalid text node")
			}
		case "view":
			if n.Target != "" || !views[n.ViewID] || n.BindingID != "" || n.Text != "" || n.Columns != 0 || len(n.Children) != 0 {
				return personalInvalid("invalid view node")
			}
		case "binding":
			if n.Target != "" || !bindings[n.BindingID] || n.ViewID != "" || n.Text != "" || n.Columns != 0 || len(n.Children) != 0 {
				return personalInvalid("invalid binding node")
			}
		default:
			return personalInvalid("unknown layout node")
		}
		for _, child := range n.Children {
			if e := walk(child, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	return walk(s.Definition.Layout, 1)
}
