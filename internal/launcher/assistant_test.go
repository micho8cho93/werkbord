package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"devboard/internal/config"
)

func TestAssistantWorkspaceRequestUsesTheControllerTokenAndOnlyValidIDs(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/assistant/sessions/ast_test/workspace-requests/wreq_1" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "wrong request", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pending":true,"request":{"id":"wreq_1"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Addr = server.Listener.Addr().String()
	cfg.Token = "fixture-token"
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	l := New(Options{DataDir: dir, HTTP: server.Client()})

	body, err := l.AssistantWorkspaceRequest(context.Background(), "ast_test", "wreq_1")
	if err != nil || string(body) != `{"pending":true,"request":{"id":"wreq_1"}}` || requests != 1 {
		t.Fatalf("authorization response %s, requests %d, error %v", body, requests, err)
	}
	for _, pair := range [][2]string{{"../admin", "wreq_2"}, {"ast_test", "../create"}} {
		if _, err := l.AssistantWorkspaceRequest(context.Background(), pair[0], pair[1]); err == nil {
			t.Errorf("accepted unsafe request IDs %q / %q", pair[0], pair[1])
		}
	}
	if requests != 1 {
		t.Fatalf("invalid IDs reached the controller: %d requests", requests)
	}
}
