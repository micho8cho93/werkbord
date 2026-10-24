package appops

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/service"
	"devboard/internal/store"
)

func personalFixture(t *testing.T) (*fx, *service.Personal, Principal) {
	f := newFx(t)
	personal := &service.Personal{Deps: service.Deps{Store: f.db, Now: f.now}}
	f.svc = New(Config{Backend: f.svc.b, Store: f.st, Now: f.now, Personal: personal})
	return f, personal, f.session("personal")
}
func todoChange() domain.PersonalChange {
	return domain.PersonalChange{Workspace: domain.PersonalWorkspaceSpec{
		Name: "Weekly to-dos", Icon: "check", UISchemaVersion: 1,
		Visibility:    domain.PersonalVisibility{Universal: true, Individual: true, TeamWorkspaceIDs: []string{"team:design", "team:engineering"}},
		CollectionIDs: []string{"tasks"},
		Definition: domain.PersonalUI{Views: []domain.PersonalView{
			{ID: "tasks", Name: "Tasks", Kind: "checklist", CollectionID: "tasks", Fields: []string{"title", "deadline", "priority"}, CheckedField: "done", Operations: []string{"create", "update"}},
			{ID: "progress", Name: "Progress", Kind: "progress", CollectionID: "tasks", Fields: []string{"done"}, CheckedField: "done"},
		}, Layout: domain.PersonalNode{Type: "stack", Children: []domain.PersonalNode{{Type: "view", ViewID: "tasks"}, {Type: "view", ViewID: "progress"}}}},
	}, Collections: []domain.PersonalCollectionChange{{ID: "tasks", Spec: domain.PersonalCollectionSpec{Name: "Weekly tasks", SchemaVersion: 1, Fields: []domain.PersonalField{
		{Key: "title", Name: "Task", Type: "text", Required: true}, {Key: "deadline", Name: "Deadline", Type: "date"}, {Key: "priority", Name: "Priority", Type: "select", Options: []string{"Low", "Normal", "Urgent"}}, {Key: "done", Name: "Done", Type: "boolean"},
	}}}}}
}
func editChange(snapshot *domain.PersonalSnapshot) domain.PersonalChange {
	ch := domain.PersonalChange{WorkspaceID: snapshot.Workspace.ID, Revision: snapshot.Workspace.Revision, Workspace: snapshot.Workspace.PersonalWorkspaceSpec}
	for _, c := range snapshot.Collections {
		ch.Collections = append(ch.Collections, domain.PersonalCollectionChange{ID: c.ID, Revision: c.Revision, Spec: c.PersonalCollectionSpec})
	}
	return ch
}
func createPersonal(t *testing.T, f *fx, p Principal) *domain.PersonalWorkspace {
	t.Helper()
	proposal := f.mustCall(p, "propose_personal_workspace", todoChange()).Pending
	result, err := f.confirm(p, proposal.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	return result.Data.(*Changed).Workspace
}
func TestPersonalCreationRevisionRecordsRollbackAndDurableDedup(t *testing.T) {
	f, personal, p := personalFixture(t)
	first := f.mustCall(p, "propose_personal_workspace", todoChange())
	repeated := f.mustCall(p, "propose_personal_workspace", todoChange())
	if first.Pending.ActionID != repeated.Pending.ActionID {
		t.Fatal("duplicate pending proposal")
	}
	workspaces, _ := personal.ListWorkspaces(bg, domain.PersonalQuery{Limit: 100})
	collections, _ := personal.ListCollections(bg, domain.PersonalQuery{Limit: 100})
	if len(workspaces) != 0 || len(collections) != 0 {
		t.Fatal("saved before approval")
	}
	result, err := f.confirm(p, first.Pending.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	w := result.Data.(*Changed).Workspace
	record, err := personal.CreateRecord(bg, w.CollectionIDs[0], map[string]json.RawMessage{"title": json.RawMessage(`"Ship weekly report"`), "done": json.RawMessage(`false`), "priority": json.RawMessage(`"Urgent"`)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := personal.Snapshot(bg, w.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ch := editChange(snapshot)
	ch.Workspace.Name = "My week"
	ch.Workspace.Icon = "calendar"
	ch.Workspace.Definition.Views = append(ch.Workspace.Definition.Views, domain.PersonalView{ID: "urgent", Name: "Urgent tasks", Kind: "list", CollectionID: w.CollectionIDs[0], Fields: []string{"title", "priority"}, Filters: []domain.PersonalFilter{{Field: "priority", Value: "Urgent"}}})
	ch.Workspace.Definition.Layout.Children = []domain.PersonalNode{{Type: "view", ViewID: "progress"}, {Type: "view", ViewID: "urgent"}, {Type: "view", ViewID: "tasks"}}
	ch.Collections[0].Spec.Fields = append(ch.Collections[0].Spec.Fields, domain.PersonalField{Key: "notes", Name: "Notes", Type: "text"})
	proposal := f.mustCall(p, "propose_personal_workspace", ch).Pending
	before, _ := personal.GetWorkspace(bg, w.ID)
	if before.Revision != 1 {
		t.Fatal("changed without approval")
	}
	result, err = f.confirm(p, proposal.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	revised := result.Data.(*Changed).Workspace
	if revised.ID != w.ID || revised.Revision != 2 || revised.Definition.Layout.Children[0].ViewID != "progress" {
		t.Fatal(revised)
	}
	saved, _ := personal.GetRecord(bg, w.CollectionIDs[0], record.ID)
	if !reflect.DeepEqual(record, saved) {
		t.Fatal("layout edit changed records", saved)
	}
	history, err := personal.Snapshot(bg, w.ID, 1)
	if err != nil || history.Workspace.Name != w.Name {
		t.Fatal("previous definition missing", history, err)
	}
	current, _ := personal.Snapshot(bg, w.ID, 0)
	rollback := editChange(current)
	rollback.Workspace = history.Workspace.PersonalWorkspaceSpec
	pending := f.mustCall(p, "propose_personal_workspace", rollback).Pending
	if _, err = f.confirm(p, pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	restored, _ := personal.GetWorkspace(bg, w.ID)
	if restored.Revision != 3 || restored.Name != w.Name {
		t.Fatal(restored)
	}
	// A new principal/model has no conversation context, but sees the same SQLite state.
	other := f.session("another-model")
	reopened := f.mustCall(other, "get_personal_workspace", map[string]any{"workspaceId": w.ID}).Data.(*domain.PersonalSnapshot)
	if reopened.Workspace.ID != w.ID {
		t.Fatal("model change lost workspace")
	}
	if _, err = f.call(other, "propose_personal_workspace", todoChange()); CodeOf(err) != CodeConflict {
		t.Fatal("completed creation duplicated", err)
	}
	workspaces, _ = personal.ListWorkspaces(bg, domain.PersonalQuery{Limit: 100})
	if len(workspaces) != 1 {
		t.Fatal(workspaces)
	}
	if _, err = f.confirm(p, first.Pending.ActionID, true); CodeOf(err) != CodeNotPending {
		t.Fatal(err)
	}
}
func TestPersonalFailedAndInvalidUpdatesKeepPreviousDefinition(t *testing.T) {
	f, personal, p := personalFixture(t)
	w := createPersonal(t, f, p)
	snapshot, _ := personal.Snapshot(bg, w.ID, 0)
	for _, kind := range []string{"script", "unknown-property", "destructive", "replace-binding", "stale", "bad-filter", "unregistered-view"} {
		t.Run(kind, func(t *testing.T) {
			current, _ := personal.Snapshot(bg, w.ID, 0)
			ch := editChange(current)
			switch kind {
			case "unregistered-view":
				ch.Workspace.Definition.Views[0].Kind = "calendar"
				ch.Workspace.Definition.Views[0].CheckedField = ""
				ch.Workspace.Definition.Views[0].DateField = "deadline"
			case "script":
				ch.Workspace.Definition.Layout = domain.PersonalNode{Type: "javascript", Text: "fetch('https://example.com')"}
			case "destructive":
				ch.Collections[0].Spec.Fields = ch.Collections[0].Spec.Fields[1:]
			case "replace-binding":
				ch.Workspace.CollectionIDs = []string{domain.NewID(domain.PrefixPersonalCollection)}
			case "stale":
				ch.Revision = 99
			case "bad-filter":
				ch.Workspace.Definition.Views[0].Filters = []domain.PersonalFilter{{Field: "done", Value: "approve"}}
			}
			var input any = ch
			if kind == "unknown-property" {
				raw, _ := json.Marshal(ch)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				m["records"] = []any{map[string]any{"title": "injected sample"}}
				input = m
			}
			if _, err := f.call(p, "propose_personal_workspace", input); err == nil {
				t.Fatal("accepted invalid change")
			}
			after, _ := personal.Snapshot(bg, w.ID, 0)
			if !reflect.DeepEqual(snapshot, after) {
				t.Fatal("invalid output mutated SQLite")
			}
		})
	}
	ch := editChange(snapshot)
	ch.Workspace.Name = "Pending edit"
	pending := f.mustCall(p, "propose_personal_workspace", ch).Pending
	// A valid concurrent owner edit makes this proposal stale.
	next := snapshot.Workspace.PersonalWorkspaceSpec
	next.Name = "Owner edit"
	if _, err := personal.UpdateWorkspace(bg, w.ID, w.Revision, next); err != nil {
		t.Fatal(err)
	}
	if _, err := f.confirm(p, pending.ActionID, true); CodeOf(err) != CodeConflict {
		t.Fatal(err)
	}
	after, _ := personal.GetWorkspace(bg, w.ID)
	if after.Name != "Owner edit" || after.Revision != 2 {
		t.Fatal(after)
	}
	old, err := personal.Snapshot(bg, w.ID, 1)
	if err != nil || old.Workspace.Name != w.Name {
		t.Fatal("history lost", err)
	}
}
func TestPersonalPermissionsAuditAndApprovalIsolation(t *testing.T) {
	f, personal, p := personalFixture(t)
	for _, restricted := range []Principal{
		{ID: "mcp", Via: "mcp", Grants: AllPermissions},
		{ID: "scoped", Via: "assistant", Grants: AllPermissions, Projects: []string{}},
		{ID: "read", Via: "assistant", Grants: []Permission{PermPersonalRead}},
	} {
		if _, err := f.call(restricted, "propose_personal_workspace", todoChange()); err == nil {
			t.Fatal("permission bypass")
		}
	}
	for _, name := range []string{"approve_personal_workspace", "execute_sql", "http_request", "run_javascript"} {
		if _, err := f.call(p, name, map[string]any{}); err == nil {
			t.Fatal("unsafe tool exposed")
		}
	}
	pending := f.mustCall(p, "propose_personal_workspace", todoChange()).Pending
	other := f.session("stranger")
	if _, err := f.confirm(other, pending.ActionID, true); CodeOf(err) != CodeNotFound {
		t.Fatal(err)
	}
	f.st.broken.Store(true)
	if _, err := f.confirm(p, pending.ActionID, true); CodeOf(err) != CodeAuditUnavailable {
		t.Fatal(err)
	}
	all, _ := personal.ListWorkspaces(bg, domain.PersonalQuery{Limit: 100})
	if len(all) != 0 {
		t.Fatal("audit bypass")
	}
	f.st.broken.Store(false)
	revoked := p
	revoked.Grants = []Permission{PermPersonalRead}
	if _, err := f.confirm(revoked, pending.ActionID, true); CodeOf(err) != CodePermissionDenied {
		t.Fatal(err)
	}
	if err := f.db.View(bg, func(tx store.Tx) error {
		entries, err := tx.Assistant().ListAudit(bg, "", 0, 100)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			if entry.Operation == "propose_personal_workspace" && string(entry.Outcome) == "proposed" {
				found = true
				if strings.Contains(entry.Detail, "Weekly") {
					t.Fatal("private prose in audit")
				}
			}
		}
		if !found {
			t.Fatal("proposal not audited")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalAtomicFailureRollsBackSchemasAndHistory(t *testing.T) {
	f, personal, p := personalFixture(t)
	w := createPersonal(t, f, p)
	before, _ := personal.Snapshot(bg, w.ID, 0)
	ch := editChange(before)
	ch.Workspace.Name = "Failed revision"
	ch.Collections[0].Spec.Fields = append(ch.Collections[0].Spec.Fields, domain.PersonalField{Key: "notes", Name: "Notes", Type: "text"})
	pending := f.mustCall(p, "propose_personal_workspace", ch).Pending
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER fail_personal_update BEFORE UPDATE ON personal_workspaces BEGIN SELECT RAISE(ABORT, 'simulated storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.confirm(p, pending.ActionID, true); err == nil {
		t.Fatal("expected failed update")
	}
	after, _ := personal.Snapshot(bg, w.ID, 0)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed transaction changed schema or definition")
	}
	if _, err = personal.Snapshot(bg, w.ID, 2); err == nil {
		t.Fatal("failed revision entered history")
	}
}

func TestPersonalCanEditWorkspaceSavedBeforeAssistantSupport(t *testing.T) {
	f, personal, p := personalFixture(t)
	draft := todoChange()
	c, err := personal.CreateCollection(bg, draft.Collections[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	spec := draft.Workspace
	spec.CollectionIDs = []string{c.ID}
	spec.ReadBindings = []domain.PersonalReadBinding{}
	for i := range spec.Definition.Views {
		spec.Definition.Views[i].CollectionID = c.ID
	}
	w, err := personal.CreateWorkspace(bg, spec)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := personal.Snapshot(bg, w.ID, 0)
	ch := editChange(before)
	ch.Workspace.ReadBindings = nil
	ch.Workspace.Icon = "calendar"
	ch.Workspace.Visibility = domain.PersonalVisibility{Individual: true}
	pending := f.mustCall(p, "propose_personal_workspace", ch).Pending
	result, err := f.confirm(p, pending.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	after := result.Data.(*Changed).Workspace
	if after.ID != w.ID || after.Revision != 2 || !domain.GeneratedPersonalIcon(after.Icon) || !reflect.DeepEqual(after.CollectionIDs, w.CollectionIDs) {
		t.Fatal(after)
	}
}

func TestPersonalDeletionProposalRequiresConfirmationAndAllowsRetentionReuse(t *testing.T) {
	f, personal, p := personalFixture(t)
	w := createPersonal(t, f, p)
	record, err := personal.CreateRecord(bg, w.CollectionIDs[0], map[string]json.RawMessage{"title": json.RawMessage(`"Retain me"`)})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"workspaceId": w.ID, "revision": w.Revision, "mode": "keep_data"}
	pending := f.mustCall(p, "propose_delete_personal_workspace", args).Pending
	if pending == nil {
		t.Fatal("no confirmation")
	}
	if _, err = personal.GetWorkspace(bg, w.ID); err != nil {
		t.Fatal("deleted before approval", err)
	}
	if _, err = f.confirm(p, pending.ActionID, false); err != nil {
		t.Fatal(err)
	}
	pending = f.mustCall(p, "propose_delete_personal_workspace", args).Pending
	if _, err = f.confirm(p, pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = personal.GetRecord(bg, record.CollectionID, record.ID); err != nil {
		t.Fatal(err)
	}
	library := f.mustCall(p, "list_personal_collections", map[string]any{}).Data
	if library == nil {
		t.Fatal("no library")
	}
	c, _ := personal.GetCollection(bg, record.CollectionID)
	reuse := todoChange()
	reuse.Workspace.Name = "Recovered planner"
	reuse.Workspace.CollectionIDs = []string{c.ID}
	for i := range reuse.Workspace.Definition.Views {
		reuse.Workspace.Definition.Views[i].CollectionID = c.ID
	}
	reuse.Collections = []domain.PersonalCollectionChange{{ID: c.ID, Revision: c.Revision, Spec: c.PersonalCollectionSpec}}
	proposal := f.mustCall(p, "propose_personal_workspace", reuse).Pending
	result, err := f.confirm(p, proposal.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	recovered := result.Data.(*Changed).Workspace
	args = map[string]any{"workspaceId": recovered.ID, "revision": recovered.Revision, "mode": "delete_data"}
	pending = f.mustCall(p, "propose_delete_personal_workspace", args).Pending
	if _, err = personal.CreateRecord(bg, c.ID, map[string]json.RawMessage{"title": json.RawMessage(`"New task after preview"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.confirm(p, pending.ActionID, true); CodeOf(err) != CodeConflict {
		t.Fatal("stale deletion bypassed review", err)
	}
	if _, err = personal.GetWorkspace(bg, recovered.ID); err != nil {
		t.Fatal(err)
	}
	pending = f.mustCall(p, "propose_delete_personal_workspace", args).Pending
	if _, err = f.confirm(p, pending.ActionID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = personal.GetCollection(bg, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
