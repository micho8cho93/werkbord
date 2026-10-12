package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpBearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c := r.Clone(r.Context())
	c.Header = r.Header.Clone()
	c.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(c)
}

func TestTeamMCPGrantScopesBoardOperationsAndRevocation(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	shop := owner.want(201, "POST", v1+"/projects", `{"name":"Shop"}`)
	secret := owner.want(201, "POST", v1+"/projects", `{"name":"Private roadmap"}`)
	shopID, secretID := str(shop, "id"), str(secret, "id")
	created := owner.want(201, "POST", v1+"/mcp/grants", map[string]any{
		"name": "planning agent", "projects": []string{shopID},
		"operations": []string{"list_projects", "list_tickets", "create_ticket"},
	})
	token := str(created, "token")
	grant := sub(created, "grant")
	grantID := str(grant, "id")
	if len(token) < 20 || grantID == "" {
		t.Fatalf("MCP grant wasn't issued: %v", created)
	}
	if _, _, raw := owner.do("GET", v1+"/mcp/grants", nil); len(raw) == 0 || strings.Contains(string(raw), token) || strings.Contains(string(raw), "tokenHash") {
		t.Fatalf("grant listing exposed a credential or its hash: %s", raw)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "team-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: mcpBearerTransport{token: token, base: http.DefaultTransport}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("tools do not match this grant: %+v", tools.Tools)
	}
	projects, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{}})
	if err != nil || projects.IsError || projects.StructuredContent == nil {
		t.Fatalf("project read failed: result=%+v error=%v", projects, err)
	}
	if text := marshalForTest(t, projects.StructuredContent); strings.Contains(text, secretID) || !strings.Contains(text, shopID) {
		t.Fatalf("MCP scope leaked an ungranted project: %s", text)
	}
	denied, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_tickets", Arguments: map[string]any{"projectId": secretID}})
	if err != nil || !denied.IsError {
		t.Fatalf("out-of-scope project was not refused: result=%+v error=%v", denied, err)
	}
	newTask, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_ticket", Arguments: map[string]any{"projectId": shopID, "title": "From external agent"}})
	if err != nil || newTask.IsError {
		t.Fatalf("explicitly granted task creation failed: result=%+v error=%v", newTask, err)
	}
	board := owner.want(200, "GET", v1+"/projects/"+shopID+"/board", nil)
	tickets, _ := board["tickets"].([]any)
	if len(tickets) != 1 || str(tickets[0].(map[string]any), "title") != "From external agent" {
		t.Fatalf("MCP-created task not on board: %v", board)
	}

	owner.want(204, "DELETE", v1+"/mcp/grants/"+grantID, nil)
	if _, err := session.ListTools(context.Background(), &mcp.ListToolsParams{}); err == nil {
		t.Fatal("revoked Team MCP grant remained usable in an established session")
	}
}

func TestTeamMCPTaskPlanIsEditableAndRequiresHumanApproval(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	project := owner.want(201, "POST", v1+"/projects", `{"name":"Roadmap"}`)
	projectID := str(project, "id")
	created := owner.want(201, "POST", v1+"/mcp/grants", map[string]any{
		"name": "planning agent", "projects": []string{projectID},
		"operations": []string{"propose_task_plan", "list_pending_proposals", "get_proposal_status"},
	})
	grantID, token := str(sub(created, "grant"), "id"), str(created, "token")
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "team-plan-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: mcpBearerTransport{token: token, base: http.DefaultTransport}}
	session, err := mcpClient.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	request := map[string]any{
		"projectId": projectID, "idempotencyKey": "meeting-2026-10-12", "tasks": []any{
			map[string]any{"tempId": "tmp_prepare", "title": "Prepare agenda", "plan": map[string]any{"start": "2026-11-02", "end": "2026-11-03"}, "sourceRef": "mail:meeting-42"},
			map[string]any{"tempId": "tmp_followup", "title": "Send follow-up", "dependencies": []string{"tmp_prepare"}, "plan": map[string]any{"start": "2026-11-04", "milestone": true}},
		},
	}
	proposalResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "propose_task_plan", Arguments: request})
	if err != nil || proposalResult.IsError || proposalResult.StructuredContent == nil {
		t.Fatalf("task plan proposal failed: result=%+v error=%v", proposalResult, err)
	}
	var proposed map[string]any
	b, _ := json.Marshal(proposalResult.StructuredContent)
	_ = json.Unmarshal(b, &proposed)
	proposalID := str(proposed, "id")
	if proposalID == "" || str(proposed, "state") != "pending" {
		t.Fatalf("proposal was not pending: %v", proposed)
	}
	board := owner.want(200, "GET", v1+"/projects/"+projectID+"/board", nil)
	tickets, _ := board["tickets"].([]any)
	if len(tickets) != 0 {
		t.Fatalf("proposed tasks were applied before a person approved them: %v", tickets)
	}

	plans := owner.want(200, "GET", v1+"/mcp/proposals", nil)
	entries, _ := plans["proposals"].([]any)
	if len(entries) != 1 || str(entries[0].(map[string]any), "id") != proposalID {
		t.Fatalf("human review list does not contain the plan: %v", plans)
	}
	plan := entries[0].(map[string]any)["plan"].(map[string]any)
	plan["tasks"].([]any)[0].(map[string]any)["description"] = "Edited by a person before approval"
	owner.want(200, "PUT", v1+"/mcp/proposals/"+proposalID, plan)

	status, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_proposal_status", Arguments: map[string]any{"proposalId": proposalID}})
	if err != nil || status.IsError || !strings.Contains(marshalForTest(t, status.StructuredContent), `"state":"pending"`) {
		t.Fatalf("external client could not inspect its own pending status: result=%+v error=%v", status, err)
	}
	approved := owner.want(200, "POST", v1+"/mcp/proposals/"+proposalID+"/approve", nil)
	if str(approved, "state") != "approved" {
		t.Fatalf("proposal was not approved: %v", approved)
	}
	replayedApproval := owner.want(200, "POST", v1+"/mcp/proposals/"+proposalID+"/approve", nil)
	if str(replayedApproval, "state") != "approved" {
		t.Fatalf("approval retry changed the decision: %v", replayedApproval)
	}
	board = owner.want(200, "GET", v1+"/projects/"+projectID+"/board", nil)
	tickets, _ = board["tickets"].([]any)
	if len(tickets) != 2 {
		t.Fatalf("approval didn't apply the complete task plan: %v", board)
	}
	byTitle := map[string]map[string]any{}
	for _, raw := range tickets {
		item := raw.(map[string]any)
		byTitle[str(item, "title")] = item
	}
	prep, follow := byTitle["Prepare agenda"], byTitle["Send follow-up"]
	if prep == nil || follow == nil || str(prep, "description") != "Edited by a person before approval" || str(prep, "sourceRef") != "mail:meeting-42" {
		t.Fatalf("edited task content or source reference did not persist: %v", tickets)
	}
	deps, _ := follow["dependencies"].([]any)
	if len(deps) != 1 || deps[0] != prep["id"] {
		t.Fatalf("temporary dependency wasn't resolved: prep=%v follow=%v", prep, follow)
	}
	status, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_proposal_status", Arguments: map[string]any{"proposalId": proposalID}})
	if err != nil || status.IsError || !strings.Contains(marshalForTest(t, status.StructuredContent), `"state":"approved"`) {
		t.Fatalf("external client could not inspect approval status: result=%+v error=%v", status, err)
	}
	if grantID == "" {
		t.Fatal("MCP grant ID was not returned")
	}
	replayed, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "propose_task_plan", Arguments: request})
	if err != nil || replayed.IsError || str(mustMapResult(t, replayed.StructuredContent), "id") != proposalID || str(mustMapResult(t, replayed.StructuredContent), "state") != "approved" {
		t.Fatalf("same idempotency key duplicated or reset the approved plan: result=%+v error=%v", replayed, err)
	}
}

func mustMapResult(t *testing.T, raw any) map[string]any {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTeamMCPTaskPlanRejectsCyclesAndRechecksStaleVersions(t *testing.T) {
	ts := newServer(t)
	owner := client{t: t, base: ts.URL, token: ownerToken}
	project := owner.want(201, "POST", v1+"/projects", `{"name":"Planning"}`)
	projectID := str(project, "id")
	ticket := owner.want(201, "POST", v1+"/projects/"+projectID+"/tickets", map[string]any{"title": "Existing"})
	ticketID := str(ticket, "id")
	created := owner.want(201, "POST", v1+"/mcp/grants", map[string]any{
		"name": "planning agent", "projects": []string{projectID}, "operations": []string{"propose_task_plan"},
	})
	token := str(created, "token")
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "team-plan-conflict-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: mcpBearerTransport{token: token, base: http.DefaultTransport}}
	session, err := mcpClient.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	cycle, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "propose_task_plan", Arguments: map[string]any{
		"projectId": projectID, "idempotencyKey": "cycle", "tasks": []any{map[string]any{"tempId": "tmp_loop", "title": "Loop", "dependencies": []string{"tmp_loop"}}},
	}})
	if err != nil || !cycle.IsError {
		t.Fatalf("cyclic task plan was not rejected: result=%+v error=%v", cycle, err)
	}
	missing, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "propose_task_plan", Arguments: map[string]any{
		"projectId": projectID, "idempotencyKey": "missing-dependency", "tasks": []any{map[string]any{"tempId": "tmp_missing", "title": "Missing dependency", "dependencies": []string{"ttk_missing"}}},
	}})
	if err != nil || !missing.IsError {
		t.Fatalf("missing dependency was not rejected: result=%+v error=%v", missing, err)
	}
	board := owner.want(200, "GET", v1+"/projects/"+projectID+"/board", nil)
	if tickets, _ := board["tickets"].([]any); len(tickets) != 1 {
		t.Fatalf("rejected plan changed the board: %v", board)
	}
	proposal, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "propose_task_plan", Arguments: map[string]any{
		"projectId": projectID, "idempotencyKey": "stale-version", "tasks": []any{
			map[string]any{"tempId": "tmp_must_rollback", "title": "Must not be partially created"},
			map[string]any{"taskId": ticketID, "version": int64(1), "title": "Proposed title"},
		},
	}})
	if err != nil || proposal.IsError || proposal.StructuredContent == nil {
		t.Fatalf("valid edit proposal failed: result=%+v error=%v", proposal, err)
	}
	proposalID := str(mustMapResult(t, proposal.StructuredContent), "id")
	owner.want(200, "PATCH", v1+"/projects/"+projectID+"/tickets/"+ticketID, map[string]any{"version": int64(1), "title": "Concurrent human edit"})
	owner.want(409, "POST", v1+"/mcp/proposals/"+proposalID+"/approve", nil)
	board = owner.want(200, "GET", v1+"/projects/"+projectID+"/board", nil)
	tickets, _ := board["tickets"].([]any)
	if len(tickets) != 1 || str(tickets[0].(map[string]any), "title") != "Concurrent human edit" {
		t.Fatalf("stale proposal overwrote a newer task version: %v", board)
	}
}

func marshalForTest(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
