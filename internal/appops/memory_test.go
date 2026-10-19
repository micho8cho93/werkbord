package appops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/service"
)

func TestMemoryContextPermissionScopeRelevanceAndBudget(t *testing.T) {
	f := newFx(t)
	p := f.project("Visible")
	hidden := f.project("Hidden")
	memory, err := service.NewMemory(service.Deps{Store: f.db}, "individual", domain.MemoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	k := &service.Knowledge{Memory: memory}
	for _, in := range []struct{ project, text, kind string }{{p.ID, "SQLite architecture uses transactions", "decision"}, {p.ID, "SQLite merge requires review", "policy"}, {p.ID, "Prefer SQLite WAL", "preference"}, {hidden.ID, "SQLite SECRET", "fact"}} {
		if _, err = k.SaveManual(bg, in.project, service.ManualMemory{Summary: in.text, Kind: in.kind}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = k.SaveManual(bg, "", service.ManualMemory{Summary: "SQLite PERSONAL preference", Kind: "preference"}); err != nil {
		t.Fatal(err)
	}
	f.svc.b = NewBackend(Services{Projects: f.projects, Tasks: f.tasks, Knowledge: k})
	who := f.session("ast_memory", PermMemoryRead)
	who.Projects = []string{p.ID}
	for _, budget := range []int{512, 1500, 6000} {
		r := f.mustCall(who, "retrieve_memory", map[string]any{"projectId": p.ID, "query": "SQLite", "tokenBudget": budget, "requestType": "architecture"})
		out := r.Data.(service.MemoryContext)
		raw, _ := json.Marshal(out)
		if len(raw) > budget || out.TokenUpperBound > budget {
			t.Fatalf("budget %d: %s", budget, raw)
		}
		if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "PERSONAL") {
			t.Fatal("cross-project leak")
		}
		if len(out.Items) > 0 && out.Items[0].Kind != "policy" {
			t.Fatalf("authority ordering: %+v", out.Items)
		}
	}
	all := who
	all.Projects = nil
	personalResult := f.mustCall(all, "retrieve_memory", map[string]any{"projectId": p.ID, "query": "SQLite", "tokenBudget": 6000})
	personalJSON, _ := json.Marshal(personalResult.Data)
	if !strings.Contains(string(personalJSON), "PERSONAL") {
		t.Fatal("permitted personal context was not retrieved")
	}
	args, _ := json.Marshal(map[string]any{"projectId": hidden.ID})
	if _, err = f.svc.Call(bg, who, "retrieve_memory", args); CodeOf(err) != CodeNotFound {
		t.Fatal(err)
	}
	who.Grants = []Permission{PermProjectsRead}
	if _, err = f.svc.Call(bg, who, "retrieve_memory", args); CodeOf(err) != CodePermissionDenied {
		t.Fatal(err)
	}
	who.Grants = []Permission{PermMemoryRead}
	who.Via = "mcp"
	if _, err = f.svc.Call(bg, who, "retrieve_memory", args); CodeOf(err) != CodeNotFound {
		t.Fatal(err)
	}
}

func TestMemoryContextUnavailableAndDisconnectedWorkspace(t *testing.T) {
	f := newFx(t)
	p := f.project("Empty")
	who := f.session("ast_unavailable", PermMemoryRead)
	out := f.mustCall(who, "retrieve_memory", map[string]any{"projectId": p.ID}).Data.(service.MemoryContext)
	if out.Status != "unavailable" || len(out.Items) != 0 {
		t.Fatal(out)
	}
	raw, _ := json.Marshal(map[string]any{"projectId": "workspace:disconnected/project"})
	if _, err := f.svc.Call(bg, who, "retrieve_memory", raw); err == nil {
		t.Fatal("disconnected workspace read succeeded")
	}
}

func TestConnectedMemoryObeysEveryAssistantProjectGrant(t *testing.T) {
	f := newFx(t)
	who := f.session("ast_cross_memory", PermMemoryRead)
	visible := workspaceProjectID("team:main", "p1")
	who.Projects = []string{visible}
	f.svc.BindWorkspaces(who.SessionID, []Workspace{{ID: "team:main", Name: "Team"}}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		if method == "get_project" {
			return json.Marshal(ProjectView{ID: "p1", Name: "Visible"})
		}
		return json.Marshal(service.MemoryContext{Status: "available", Items: []service.MemoryContextItem{
			{ID: "permitted", Scope: domain.MemoryScope{ProjectID: "p1"}},
			{ID: "cross_project_secret", RelatedScopes: []domain.MemoryScope{{ProjectID: "p1"}, {ProjectID: "p2"}}},
		}})
	})
	out := f.mustCall(who, "retrieve_memory", map[string]any{"projectId": visible, "tokenBudget": 6000}).Data.(service.MemoryContext)
	if len(out.Items) != 1 || out.Items[0].ID != "permitted" {
		t.Fatalf("assistant project scope leaked: %+v", out)
	}
	who.Projects = nil
	out = f.mustCall(who, "retrieve_memory", map[string]any{"projectId": visible, "tokenBudget": 6000}).Data.(service.MemoryContext)
	if len(out.Items) != 2 {
		t.Fatalf("authorised cross-project source missing: %+v", out)
	}
}
