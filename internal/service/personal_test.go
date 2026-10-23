package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func personalCollectionSpec() domain.PersonalCollectionSpec {
	return domain.PersonalCollectionSpec{Name: "Tasks", SchemaVersion: 1, Fields: []domain.PersonalField{{Key: "title", Name: "Title", Type: "text", Required: true}, {Key: "done", Name: "Done", Type: "boolean"}}}
}
func personalWorkspaceSpec(id string) domain.PersonalWorkspaceSpec {
	return domain.PersonalWorkspaceSpec{Name: "My tasks", Icon: "board", UISchemaVersion: 1, Visibility: domain.PersonalVisibility{Individual: true, Universal: true, TeamWorkspaceIDs: []string{"team:design"}}, CollectionIDs: []string{id}, Definition: domain.PersonalUI{Views: []domain.PersonalView{
		{ID: "list", Name: "List", Kind: "list", CollectionID: id, Fields: []string{"title"}},
		{ID: "checklist", Name: "Checklist", Kind: "checklist", CollectionID: id, Fields: []string{"title"}, CheckedField: "done"},
	}, Layout: domain.PersonalNode{Type: "stack", Children: []domain.PersonalNode{{Type: "view", ViewID: "list"}, {Type: "view", ViewID: "checklist"}}}}}
}
func personalValues(title string) map[string]json.RawMessage {
	b, _ := json.Marshal(title)
	return map[string]json.RawMessage{"title": b, "done": json.RawMessage("false")}
}
func TestPersonalLifecycleAndExistingRecordIsolation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return at }
	p, err := f.projects.Register(ctx, "/repos/personal-isolation", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, p.ID, "Existing task", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.CreateRecord(ctx, c.ID, personalValues("Private campaign"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Revision != 1 || record.Revision != 1 || w.Revision != 1 || w.CreatedAt != at {
		t.Fatal(c, record, w)
	}
	// Two independent workspaces and multiple views can reference exactly one dataset.
	w2, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Minute)
	ws := w.PersonalWorkspaceSpec
	ws.Name = "Renamed"
	updated, err := s.UpdateWorkspace(ctx, w.ID, w.Revision, ws)
	if err != nil || updated.Revision != 2 || updated.CreatedAt != w.CreatedAt || updated.UpdatedAt != at {
		t.Fatal(updated, err)
	}
	if _, err = s.UpdateWorkspace(ctx, w.ID, 1, ws); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DeleteWorkspace(ctx, w.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DeleteCollection(ctx, c.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("referenced collection deleted", err)
	}
	changed, err := s.UpdateRecord(ctx, c.ID, record.ID, 1, personalValues("Updated campaign"))
	if err != nil || changed.Revision != 2 {
		t.Fatal(changed, err)
	}
	if _, err = s.UpdateRecord(ctx, c.ID, record.ID, 1, personalValues("Stale")); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DeleteRecord(ctx, c.ID, record.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	other, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"get", "update", "delete"} {
		switch method {
		case "get":
			_, err = s.GetRecord(ctx, other.ID, record.ID)
		case "update":
			_, err = s.UpdateRecord(ctx, other.ID, record.ID, 2, personalValues("stolen"))
		case "delete":
			err = s.DeleteRecord(ctx, other.ID, record.ID, 2)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(method, err)
		}
	}
	if _, err = s.GetRecord(ctx, c.ID, task.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("accepted task as personal record", err)
	}
	if err = s.DeleteWorkspace(ctx, w.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteWorkspace(ctx, w2.ID, 1); err != nil {
		t.Fatal(err)
	}
	if v, err := s.GetRecord(ctx, c.ID, record.ID); err != nil || v.Revision != 2 {
		t.Fatal("workspace deletion lost data", v, err)
	}
	if err = s.DeleteCollection(ctx, c.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("nonempty collection deleted", err)
	}
	// Additive schema evolution preserves the saved record and all view bindings.
	next := personalCollectionSpec()
	next.Fields = append(next.Fields, domain.PersonalField{Key: "notes", Name: "Notes", Type: "text"})
	cc, err := s.UpdateCollection(ctx, c.ID, 1, next)
	if err != nil || cc.Revision != 2 {
		t.Fatal(cc, err)
	}
	if _, err = s.UpdateCollection(ctx, c.ID, 1, next); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DeleteRecord(ctx, c.ID, record.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteCollection(ctx, c.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetCollection(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err = f.deps.Store.View(ctx, func(tx store.Tx) error {
		got, e := tx.Tasks().Get(ctx, task.ID)
		if e != nil || got.Title != task.Title || got.Version != task.Version {
			t.Fatal("existing task modified", got, e)
		}
		events, e := tx.Events().ListAfter(ctx, 0, 100)
		if e != nil {
			return e
		}
		for _, ev := range events {
			if len(ev.Type) > 9 && string(ev.Type[:9]) == "personal." {
				var payload map[string]any
				if e = json.Unmarshal(ev.Payload, &payload); e != nil {
					return e
				}
				if len(payload) != 2 || payload["id"] == nil || payload["revision"] == nil {
					t.Fatal("private data entered events", string(ev.Payload))
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPersonalConcurrentUpdatesAndDeletes(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	c, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecord(ctx, c.ID, personalValues("initial"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"workspace", "collection", "record"} {
		t.Run(kind, func(t *testing.T) {
			start := make(chan struct{})
			results := make(chan error, 8)
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					<-start
					var e error
					switch kind {
					case "workspace":
						_, e = s.UpdateWorkspace(ctx, w.ID, 1, w.PersonalWorkspaceSpec)
					case "collection":
						_, e = s.UpdateCollection(ctx, c.ID, 1, c.PersonalCollectionSpec)
					case "record":
						_, e = s.UpdateRecord(ctx, c.ID, r.ID, 1, personalValues("raced"))
					}
					results <- e
				})
			}
			close(start)
			wg.Wait()
			close(results)
			success, conflicts := 0, 0
			for e := range results {
				if e == nil {
					success++
				} else if errors.Is(e, domain.ErrConflict) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if success != 1 || conflicts != 7 {
				t.Fatal(success, conflicts)
			}
		})
	}
	if err = s.DeleteRecord(ctx, c.ID, r.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
}
func TestPersonalValidationPaginationAndReadBindings(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	c, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	bad := personalWorkspaceSpec(c.ID)
	bad.Definition.Layout.Type = "script"
	if _, err = s.CreateWorkspace(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	bad = personalWorkspaceSpec(c.ID)
	bad.ReadBindings = []domain.PersonalReadBinding{{ID: "tasks", Kind: "individual.tasks", ProjectID: domain.NewID(domain.PrefixProject)}}
	if _, err = s.CreateWorkspace(ctx, bad); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	p, err := f.projects.Register(ctx, "/repos/binding", "")
	if err != nil {
		t.Fatal(err)
	}
	bad.ReadBindings[0].ProjectID = p.ID
	if _, err = s.CreateWorkspace(ctx, bad); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRecord(ctx, c.ID, map[string]json.RawMessage{"title": json.RawMessage(`42`)}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.UpdateCollection(ctx, c.ID, 0, c.PersonalCollectionSpec); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	for range 5 {
		if _, err = s.CreateRecord(ctx, c.ID, personalValues("bounded")); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	after := ""
	for {
		page, e := s.ListRecords(ctx, c.ID, domain.PersonalQuery{After: after, Limit: 2})
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		for _, v := range page {
			if seen[v.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[v.ID] = true
		}
		after = page[len(page)-1].ID
	}
	if len(seen) != 5 {
		t.Fatal(len(seen))
	}
	for _, q := range []domain.PersonalQuery{{Limit: 0}, {Limit: 101}, {Limit: 1, After: "../"}, {Limit: 1, After: c.ID}} {
		if _, e := s.ListRecords(ctx, c.ID, q); !errors.Is(e, domain.ErrInvalid) {
			t.Fatal(q, e)
		}
	}
	// A second controller database never sees the current owner's personal data.
	other := &Personal{Deps: newFixture(t).deps}
	if _, err = other.GetCollection(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
