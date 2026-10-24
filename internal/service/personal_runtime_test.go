package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/store"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPersonalActionsAreAtomicAndScoped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	c, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	spec := personalWorkspaceSpec(c.ID)
	spec.Definition.Views[0].Fields = []string{"title", "done"}
	spec.Definition.Views[0].Operations = []string{"create", "update", "delete"}
	spec.Definition.Views[0].Actions = []domain.PersonalAction{{ID: "finish", Name: "Finish", Steps: []domain.PersonalStep{{Operation: "update", Values: map[string]json.RawMessage{"done": json.RawMessage(`true`)}}}}}
	w, err := s.CreateWorkspace(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.CreateRecord(ctx, c.ID, personalValues("A"))
	b, _ := s.CreateRecord(ctx, c.ID, personalValues("B"))
	request := PersonalActionRequest{WorkspaceRevision: w.Revision, CollectionRevision: c.Revision, Confirmed: true, Records: []PersonalActionRecord{{ID: a.ID, Revision: 1}, {ID: b.ID, Revision: 99}}}
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", request); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	saved, _ := s.GetRecord(ctx, c.ID, a.ID)
	if saved.Revision != 1 || string(saved.Values["done"]) != "false" {
		t.Fatal("partial action persisted", saved)
	}
	request.Records[1].Revision = 1
	result, err := s.ExecuteAction(ctx, w.ID, "list", "finish", request)
	if err != nil || len(result.Saved) != 2 {
		t.Fatal(result, err)
	}
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", request); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("replay was not fenced", err)
	}
	request.Records = nil
	request.Confirmed = false
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", request); err == nil {
		t.Fatal("unreviewed action")
	}
	request.Confirmed = true
	request.WorkspaceRevision = 99
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", request); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	// A later invalid step rolls back the earlier step, including its events.
	spec.Definition.Views[0].Actions[0].Steps = []domain.PersonalStep{{Operation: "delete"}, {Operation: "update", Values: map[string]json.RawMessage{"done": json.RawMessage(`false`)}}}
	w, err = s.UpdateWorkspace(ctx, w.ID, w.Revision, spec)
	if err != nil {
		t.Fatal(err)
	}
	request.WorkspaceRevision = w.Revision
	request.Records = []PersonalActionRecord{{ID: a.ID, Revision: 2}}
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", request); err == nil {
		t.Fatal("used a deleted record")
	}
	saved, err = s.GetRecord(ctx, c.ID, a.ID)
	if err != nil || saved.Revision != 2 {
		t.Fatal("deletion was not rolled back", saved, err)
	}
}
func TestPersonalRulesAndReadBindingsUseCurrentOwnerData(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	spec := personalCollectionSpec()
	spec.Fields = append(spec.Fields, domain.PersonalField{Key: "due", Name: "Due", Type: "date"}, domain.PersonalField{Key: "last", Name: "Last", Type: "datetime"}, domain.PersonalField{Key: "reminder", Name: "Reminder", Type: "boolean"})
	spec.Rules = []domain.PersonalRule{{ID: "remind", Set: map[string]json.RawMessage{"reminder": json.RawMessage(`true`)}, Schedule: &domain.PersonalRuleSchedule{DateField: "due", LastRunField: "last", EveryDays: 1}}}
	c, err := s.CreateCollection(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	values := personalValues("Recurring")
	values["due"] = json.RawMessage(`"2026-10-24"`)
	record, err := s.CreateRecord(ctx, c.ID, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.GetRecord(ctx, c.ID, record.ID)
	if saved.Revision != 2 || string(saved.Values["reminder"]) != "true" {
		t.Fatal(saved)
	}
	if err = s.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetRecord(ctx, c.ID, record.ID)
	if again.Revision != 2 {
		t.Fatal("schedule replayed")
	}
	restarted := &Personal{Deps: s.Deps}
	if err = restarted.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ = s.GetRecord(ctx, c.ID, record.ID)
	if again.Revision != 2 {
		t.Fatal("restart replayed schedule")
	}
	p, err := f.projects.Register(ctx, "/repos/personal-runtime", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, p.ID, "Native ticket", "")
	if err != nil {
		t.Fatal(err)
	}
	ws := personalWorkspaceSpec(c.ID)
	ws.ReadBindings = []domain.PersonalReadBinding{{ID: "tickets", Kind: "individual.tasks", ProjectID: p.ID}}
	ws.Definition.Layout.Children = append(ws.Definition.Layout.Children, domain.PersonalNode{Type: "binding", BindingID: "tickets"})
	w, err := s.CreateWorkspace(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ResolveBinding(ctx, w.ID, "tickets")
	if err != nil || result.Unresolved != 1 || len(result.Tasks) != 1 || result.Tasks[0].ID != task.ID {
		t.Fatal(result, err)
	}
	if _, err = s.ResolveBinding(ctx, w.ID, "undeclared"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("undeclared binding", err)
	}
	// Soft-deleting a native project makes only its read component unavailable.
	if err = s.Store.Update(ctx, func(tx store.Tx) error { return tx.Projects().SetLifecycle(ctx, p.ID, "deleted", now) }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveBinding(ctx, w.ID, "tickets"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted project remained readable", err)
	}
	ws.Definition.Views[0].Operations = []string{"update"}
	ws.Definition.Views[0].Fields = []string{"title", "done"}
	ws.Definition.Views[0].Actions = []domain.PersonalAction{{ID: "finish", Name: "Finish", Steps: []domain.PersonalStep{{Operation: "update", Values: map[string]json.RawMessage{"done": json.RawMessage(`true`)}}}}}
	// Existing references are display/read declarations, never dependencies of personal writes.
	w, err = s.UpdateWorkspace(ctx, w.ID, w.Revision, ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteAction(ctx, w.ID, "list", "finish", PersonalActionRequest{WorkspaceRevision: w.Revision, CollectionRevision: c.Revision, Confirmed: true, Records: []PersonalActionRecord{{ID: record.ID, Revision: again.Revision}}}); err != nil {
		t.Fatal("unavailable read component blocked personal action", err)
	}

}
