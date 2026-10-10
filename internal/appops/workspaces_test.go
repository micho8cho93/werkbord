package appops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConnectedProjectsAndTicketsKeepConfirmationScopeAndIsolation(t *testing.T) {
	f := newFx(t)
	local := f.project("test-site")
	p := f.session("ast_connected")
	w := Workspace{ID: "board_1", Name: "TestTeam"}
	pid := workspaceProjectID(w.ID, "p1")
	created := 0
	f.svc.BindWorkspaces(p.SessionID, []Workspace{w}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "list_projects":
			return json.RawMessage(`[{"id":"p1","name":"Portfolio"}]`), nil
		case "get_project":
			return json.RawMessage(`{"id":"p1","name":"Portfolio"}`), nil
		case "create_ticket":
			created++
			return json.RawMessage(`{"id":"t1","projectId":"p1","title":"Extract CV","state":"backlog","workMode":"human","version":1}`), nil
		case "list_tickets":
			return json.RawMessage(`[{"id":"t1","projectId":"p1","title":"Extract CV","state":"backlog","version":1}]`), nil
		}
		t.Fatalf("unexpected operation: %s", method)
		return nil, nil
	})
	projects, _ := json.Marshal(f.mustCall(p, "list_projects", map[string]any{}).Data)
	if !strings.Contains(string(projects), local.ID) || !strings.Contains(string(projects), pid) || !strings.Contains(string(projects), "TestTeam") {
		t.Fatalf("projects: %s", projects)
	}
	proposal := f.mustCall(p, "create_ticket", map[string]any{"projectId": pid, "title": "Extract CV"}).Pending
	if created != 0 || proposal == nil || !strings.Contains(proposal.Summary, "TestTeam") {
		t.Fatalf("no creation before an explicitly scoped proposal: %+v, %d", proposal, created)
	}
	if _, err := f.confirm(p, proposal.ActionID, true); err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(f.tickets(local.ID)) != 0 {
		t.Fatal("only the connected board should change once")
	}
	if _, err := f.confirm(p, proposal.ActionID, true); err == nil || created != 1 {
		t.Fatal("a replay created a duplicate ticket")
	}
	views, _ := json.Marshal(f.mustCall(p, "list_tickets", map[string]any{"projectId": pid}).Data)
	if !strings.Contains(string(views), workspaceProjectID(w.ID, "t1")) {
		t.Fatalf("unscoped ticket: %s", views)
	}
	other := f.session("ast_other")
	if _, err := f.call(other, "create_ticket", map[string]any{"projectId": pid, "title": "Wrong session"}); err == nil {
		t.Fatal("another session reached a connector")
	}
	p.Projects = []string{local.ID}
	projects, _ = json.Marshal(f.mustCall(p, "list_projects", map[string]any{}).Data)
	if strings.Contains(string(projects), "TestTeam") {
		t.Fatal("project restrictions leaked the connected board")
	}
	if _, err := f.call(p, "create_ticket", map[string]any{"projectId": pid, "title": "Outside scope"}); CodeOf(err) != CodeNotFound {
		t.Fatalf("scope: %v", err)
	}
}

func TestAConnectedMutationStillFailsClosedWhenItsAuditCannotBeWritten(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_audit")
	created := false
	f.svc.BindWorkspaces(p.SessionID, []Workspace{{ID: "board_1", Name: "Team"}}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		if method == "get_project" {
			return json.RawMessage(`{"id":"p1","name":"Portfolio"}`), nil
		}
		created = true
		return nil, nil
	})
	proposal := f.mustCall(p, "create_ticket", map[string]any{"projectId": workspaceProjectID("board_1", "p1"), "title": "Extract CV"}).Pending
	f.st.broken.Store(true)
	if _, err := f.confirm(p, proposal.ActionID, true); CodeOf(err) != CodeAuditUnavailable || created {
		t.Fatalf("unrecorded connected mutation: %v, %v", err, created)
	}
}
