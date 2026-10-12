package mcpaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"devboard/internal/appops"
	"devboard/internal/mcpcontract"
	"devboard/internal/mcpgrant"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	c := r.Clone(r.Context())
	c.Header = r.Header.Clone()
	c.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(c)
}

func TestStreamableEndpointExposesOnlyScopedOperationsAndRevokesSessions(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := &service.Projects{Deps: service.Deps{Store: db}}
	ops := appops.New(appops.Config{Backend: appops.NewBackend(appops.Services{Projects: projects}), Store: db})
	grants, err := mcpgrant.Open(filepath.Join(t.TempDir(), "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := grants.Create(mcpcontract.Principal{Name: "read-only client", Projects: []string{"project-a"}, Operations: []string{"list_projects"}})
	if err != nil {
		t.Fatal(err)
	}
	access := New(grants, ops, projects)
	ts := httptest.NewServer(access.Handler())
	defer ts.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{token: created.Token, base: http.DefaultTransport}}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "list_projects" {
		t.Fatalf("tools leaked outside the grant: %+v", tools.Tools)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("scoped board read failed: result=%+v err=%v", result, err)
	}
	if result.StructuredContent == nil {
		t.Fatalf("missing structured response: %+v", result)
	}
	if !grants.Revoke(created.Grant.ID) {
		t.Fatal("could not revoke the MCP grant")
	}
	if _, err := session.ListTools(ctx, &mcp.ListToolsParams{}); err == nil {
		t.Fatal("revoked grant remained usable in an established MCP session")
	}
	_ = session.Close()
}

func TestStreamableEndpointRejectsMissingCredential(t *testing.T) {
	grants, err := mcpgrant.Open(filepath.Join(t.TempDir(), "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := &service.Projects{Deps: service.Deps{Store: db}}
	ops := appops.New(appops.Config{Backend: appops.NewBackend(appops.Services{Projects: projects}), Store: db})
	access := New(grants, ops, projects)
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	access.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated endpoint status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProposalToolsExposeOnlyPendingAndRejectUnknownFields(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := &service.Projects{Deps: service.Deps{Store: db}}
	project, err := projects.CreateWork(context.Background(), "MCP proposals")
	if err != nil {
		t.Fatal(err)
	}
	tasks := &service.Tasks{Deps: service.Deps{Store: db}}
	ops := appops.New(appops.Config{Backend: appops.NewBackend(appops.Services{Projects: projects, Tasks: tasks}), Store: db})
	grants, err := mcpgrant.Open(filepath.Join(t.TempDir(), "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	access := New(grants, ops, projects)
	created, err := access.Create(context.Background(), mcpcontract.Principal{Name: "planner", Projects: []string{project.ID}, Operations: []string{"create_ticket", "list_pending_proposals", "get_proposal_status"}})
	if err != nil {
		t.Fatal(err)
	}
	p := access.appPrincipal(created.Grant.Principal)
	args, _ := json.Marshal(map[string]string{"projectId": project.ID, "title": "Review launch plan"})
	proposed, err := ops.Call(context.Background(), p, "create_ticket", args)
	if err != nil || proposed.Pending == nil {
		t.Fatalf("create did not produce a pending proposal: result=%+v err=%v", proposed, err)
	}
	listed, err := access.call(context.Background(), created.Grant.Principal, "list_pending_proposals", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	statuses, ok := listed.([]appops.ActionStatus)
	if !ok || len(statuses) != 1 || statuses[0].State != "pending" {
		t.Fatalf("pending proposal list = %#v", listed)
	}
	if _, err := access.call(context.Background(), created.Grant.Principal, "get_proposal_status", json.RawMessage(`{"proposalId":"x","extra":true}`)); err == nil {
		t.Fatal("proposal status accepted an unknown argument")
	}
	if _, err := ops.Resolve(context.Background(), appops.Decision{SessionID: p.SessionID, ActionID: proposed.Pending.ActionID, Approve: true, By: "owner", Executor: p}); err != nil {
		t.Fatal(err)
	}
	listed, err = access.call(context.Background(), created.Grant.Principal, "list_pending_proposals", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	statuses = listed.([]appops.ActionStatus)
	if len(statuses) != 0 {
		t.Fatalf("resolved proposal remained in pending list: %+v", statuses)
	}
}
