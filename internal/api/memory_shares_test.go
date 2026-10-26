package api

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
	"werkbord/internal/localaccess"
	"werkbord/internal/service"
)

func TestOnlySeparateWorkspaceBoundRevocableGrantReadsApprovedMemoryShares(t *testing.T) {
	const workspace = "ws_team"
	ts := newTestServer(t, func(o *Options) {
		m, err := service.NewMemory(service.Deps{Store: o.Store}, "individual", domain.MemoryLimits{})
		if err != nil {
			t.Fatal(err)
		}
		o.Memory = &service.Knowledge{Memory: m}
		o.AuthRequired = true
		o.Token = ownerToken
		grants, err := localaccess.Open(filepath.Join(t.TempDir(), "grants.json"))
		if err != nil {
			t.Fatal(err)
		}
		o.LocalAccess = grants
	})
	proposal := integration.MemoryProposal{Schema: integration.MemorySchema, Scope: integration.SharedMemoryScope{WorkspaceID: workspace, Kind: "workspace"}, Content: integration.SharedMemoryContent{Kind: "observation", Summary: "Use explicit transactions", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: "review_one"}}, ClientID: "client_one"}
	expiry := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"proposal":%s,"reviewed":true,"expiresAt":%q}`, mustJSON(t, proposal), expiry)
	if code, raw := call(t, "POST", ts.URL+"/api/memory/v1/shares", ownerToken, body); code != 201 {
		t.Fatalf("approve: %d %s", code, raw)
	}
	code, raw := call(t, "POST", ts.URL+"/api/local-access", ownerToken, fmt.Sprintf(`{"name":"Team memory","scope":"memory-v1","workspaceId":%q}`, workspace))
	if code != 201 {
		t.Fatalf("grant: %d %s", code, raw)
	}
	var grant struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	_ = json.Unmarshal(raw, &grant)
	code, raw = call(t, "GET", ts.URL+"/api/memory/v1/shares?workspaceId="+workspace, grant.Token, "")
	var page integration.MemoryShares
	if code != 200 || integration.DecodeMemory(raw, &page) != nil || len(page.Shares) != 1 || page.Shares[0].Proposal.Content.Summary != "Use explicit transactions" {
		t.Fatalf("share page (%d): %s", code, raw)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/memory/v1/shares?workspaceId=ws_other", grant.Token, ""); code != 403 {
		t.Fatalf("grant accessed another workspace: %d", code)
	}
	if code, _ := call(t, "POST", ts.URL+"/api/memory/v1/shares", grant.Token, body); code != 403 {
		t.Fatalf("memory grant approved sharing: %d", code)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/integration/v1/projects", grant.Token, ""); code != 403 {
		t.Fatalf("memory grant inherited integration access: %d", code)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/memory/v1/shares?workspaceId=ws_other", grant.Token, ""); code != 403 {
		t.Fatalf("unbound query accepted: %d", code)
	}
	if code, _ := call(t, "DELETE", ts.URL+"/api/local-access/"+grant.ID, ownerToken, ""); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/memory/v1/shares?workspaceId="+workspace, grant.Token, ""); code != 401 {
		t.Fatalf("revoked grant remained active: %d", code)
	}
}
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
