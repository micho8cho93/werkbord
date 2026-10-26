package assistant

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"werkbord/internal/appops"
	"werkbord/internal/assistant/provider/fake"
	"werkbord/internal/domain"
	"werkbord/internal/service"
)

// These five configurations are also used by the browser's scripted provider.
// They exercise the real conversational approval path, not a model quality eval.
func TestSalieComposableAppsCreateInteractReopenEditDelete(t *testing.T) {
	raw, err := os.ReadFile("testdata/personal_apps.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string
		Prompt string
		Change domain.PersonalChange
		Values map[string]json.RawMessage
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			f := newEfx(t)
			personal := &service.Personal{Deps: service.Deps{Store: f.db, Now: f.now}}
			f.eng.ops = appops.New(appops.Config{Store: f.st, Now: f.now, Personal: personal})
			session := f.session()
			change := fixture.Change
			if fixture.Name == "Product Launch Center" {
				project := f.project("Launch project")
				if _, err = f.tasks.Create(bg, project.ID, "Unresolved development task", ""); err != nil {
					t.Fatal(err)
				}
				change.Workspace.ReadBindings = []domain.PersonalReadBinding{{ID: "development", Kind: "individual.tasks", ProjectID: project.ID}}
				change.Workspace.Definition.Layout.Children = append(change.Workspace.Definition.Layout.Children, domain.PersonalNode{Type: "binding", BindingID: "development"})
			}

			f.prov.Queue(fake.Reply(callBlock("find", "list_personal_workspaces", map[string]any{})), fake.Reply(callBlock("propose", "propose_personal_workspace", change)), fake.Reply("Review the proposed tool."))
			evs := f.send(session.ID, fixture.Prompt)
			event := has(evs, EventConfirmationRequired)
			if event == nil || event.Proposal == nil {
				t.Fatal(types(evs))
			}
			saved, err := personal.ListWorkspaces(bg, domain.PersonalQuery{Limit: 100})
			if err != nil || len(saved) != 0 {
				t.Fatal("saved without review", err)
			}
			result, err := f.eng.Resolve(bg, session.ID, event.Proposal.ActionID, true)
			if err != nil {
				t.Fatal(err)
			}
			w := result.Data.(*appops.Changed).Workspace
			record, err := personal.CreateRecord(bg, w.CollectionIDs[0], fixture.Values)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Name == "Sales CRM" && string(record.Values["weighted"]) != "600" {
				t.Fatal(record.Values)
			}
			if fixture.Name == "Engineering Decision Log" && string(record.Values["score"]) != "12" {
				t.Fatal(record.Values)
			}
			if fixture.Name == "Product Launch Center" {
				bound, err := personal.ResolveBinding(bg, w.ID, "development")
				if err != nil || bound.Unresolved != 1 {
					t.Fatal(bound, err)
				}
			}
			view := w.Definition.Views[0]
			for _, action := range view.Actions {
				if action.Prompt != "" {
					continue
				}
				collection, _ := personal.GetCollection(bg, view.CollectionID)
				out, err := personal.ExecuteAction(bg, w.ID, view.ID, action.ID, service.PersonalActionRequest{WorkspaceRevision: w.Revision, CollectionRevision: collection.Revision, Confirmed: true, Records: []service.PersonalActionRecord{{ID: record.ID, Revision: record.Revision}}})
				if err != nil {
					t.Fatal(err)
				}
				record = &out.Saved[0]
				break
			}
			if fixture.Name == "Personal Productivity Dashboard" {
				if err := personal.Maintain(bg); err != nil {
					t.Fatal(err)
				}
				record, _ = personal.GetRecord(bg, w.CollectionIDs[0], record.ID)
			}
			snap, err := personal.Snapshot(bg, w.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			edit := domain.PersonalChange{WorkspaceID: w.ID, Revision: w.Revision, Workspace: snap.Workspace.PersonalWorkspaceSpec}
			for _, c := range snap.Collections {
				edit.Collections = append(edit.Collections, domain.PersonalCollectionChange{ID: c.ID, Revision: c.Revision, Spec: c.PersonalCollectionSpec})
			}
			edit.Workspace.Definition.Layout.Children = append([]domain.PersonalNode{{Type: "text", Text: "Reviewed layout"}}, edit.Workspace.Definition.Layout.Children...)
			f.prov.Queue(fake.Reply(callBlock("open", "get_personal_workspace", map[string]any{"workspaceId": w.ID})), fake.Reply(callBlock("edit", "propose_personal_workspace", edit)), fake.Reply("Review the layout change."))
			evs = f.send(session.ID, "Reopen "+w.ID+" and add an introductory note while keeping all records.")
			event = has(evs, EventConfirmationRequired)
			if event == nil {
				t.Fatal(types(evs))
			}
			if _, err = f.eng.Resolve(bg, session.ID, event.Proposal.ActionID, true); err != nil {
				t.Fatal(err)
			}
			reopened, err := personal.GetRecord(bg, w.CollectionIDs[0], record.ID)
			if err != nil || reopened.Revision != record.Revision {
				t.Fatal("record changed during layout edit", reopened, err)
			}
			w, _ = personal.GetWorkspace(bg, w.ID)
			f.prov.Queue(fake.Reply(callBlock("open", "get_personal_workspace", map[string]any{"workspaceId": w.ID})), fake.Reply(callBlock("delete", "propose_delete_personal_workspace", map[string]any{"workspaceId": w.ID, "revision": w.Revision, "mode": "delete_data"})), fake.Reply("Review deletion."))
			evs = f.send(session.ID, "Delete "+w.ID+" and all its associated personal records.")
			event = has(evs, EventConfirmationRequired)
			if event == nil {
				t.Fatal(types(evs))
			}
			if _, err = f.eng.Resolve(bg, session.ID, event.Proposal.ActionID, true); err != nil {
				t.Fatal(err)
			}
			if _, err = personal.GetWorkspace(bg, w.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal(err)
			}
		})
	}
}
