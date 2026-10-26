package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"werkbord/internal/appops"
	"werkbord/internal/connections"
	"werkbord/internal/domain"
	"werkbord/internal/store"
	"werkbord/internal/store/sqlite"
)

type connectionTestSecrets map[string]string

func (s connectionTestSecrets) Get(key string) (string, error) { return s[key], nil }
func (s connectionTestSecrets) Set(key, value string) error    { s[key] = value; return nil }
func (s connectionTestSecrets) Delete(key string) error        { delete(s, key); return nil }

func TestAssistantConnectionToolsUseGovernedProposalFlow(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "assistant-mcp.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var calls atomic.Int32
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "create_note", Description: "Create a note.", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`)}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{StructuredContent: map[string]any{"created": true}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	upstream := httptest.NewServer(handler)
	defer upstream.Close()
	broker := connections.New(db, connectionTestSecrets{}, nil, nil)
	connection, err := broker.Save(ctx, connections.SaveInput{Name: "Notes", Transport: connections.TransportHTTP, URL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	connection, err = broker.Test(ctx, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	connection, err = broker.SetEnabled(ctx, connection.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	connection, err = broker.SetContext(ctx, connection.ID, "assistant", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error {
		return tx.Assistant().CreateSession(ctx, &domain.AssistantSession{ID: "ast_mcp", Provider: "test", State: domain.AssistantIdle, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	principal := appops.Principal{ID: "assistant:ast_mcp", Actor: "assistant:test", Via: "assistant", SessionID: "ast_mcp", Grants: []appops.Permission{appops.PermConnectionsUse}, MCPConnections: true}
	ops := appops.New(appops.Config{Store: db, Injected: connectionOperations(broker)})
	var external appops.Spec
	for _, spec := range ops.Catalog(principal) {
		if spec.Permission == appops.PermConnectionsUse {
			external = spec
			break
		}
	}
	if external.Name == "" || !external.RequiresConfirmation {
		t.Fatalf("the default tool was not exposed as a confirmed operation: %+v", external)
	}
	args := json.RawMessage(`{"title":"Decision notes"}`)
	proposal, err := ops.Call(ctx, principal, external.Name, args)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Pending == nil || calls.Load() != 0 {
		t.Fatal("the external service was called before a human approved the proposal")
	}
	if _, err := ops.Resolve(ctx, appops.Decision{SessionID: principal.SessionID, ActionID: proposal.Pending.ActionID, Approve: true, By: "owner", Executor: principal}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("approved external call count = %d", calls.Load())
	}
	if _, err := broker.SetToolPolicy(ctx, connection.ID, "create_note", "assistant", "allow"); err != nil {
		t.Fatal(err)
	}
	var allowed appops.Spec
	for _, spec := range ops.Catalog(principal) {
		if spec.Permission == appops.PermConnectionsUse {
			allowed = spec
			break
		}
	}
	if allowed.Name == "" || allowed.Kind != appops.KindMutation || allowed.RequiresConfirmation {
		t.Fatalf("explicit allow was not represented as an immediate but mutation-classified operation: %+v", allowed)
	}
	preauthorized, err := ops.Call(ctx, principal, allowed.Name, args)
	if err != nil || preauthorized.Pending != nil || calls.Load() != 2 {
		t.Fatalf("explicitly allowed external operation did not run immediately: result=%+v err=%v calls=%d", preauthorized, err, calls.Load())
	}
}
