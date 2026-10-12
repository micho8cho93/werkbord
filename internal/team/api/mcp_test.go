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

func marshalForTest(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
