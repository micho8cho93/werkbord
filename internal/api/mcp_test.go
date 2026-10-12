package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"devboard/internal/appops"
	"devboard/internal/mcpaccess"
	"devboard/internal/mcpgrant"
	"devboard/internal/service"
	"devboard/internal/store/sqlite"
)

func TestPrivateListenerServesScopedMCPButNotGrantAdministration(t *testing.T) {
	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	projects := &service.Projects{Deps: service.Deps{Store: db}}
	grants, err := mcpgrant.Open(filepath.Join(t.TempDir(), "mcp-grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops := appops.New(appops.Config{Backend: appops.NewBackend(appops.Services{Projects: projects}), Store: db})
	access := mcpaccess.New(grants, ops, projects)
	main := New(Options{Store: db, Projects: projects, MCP: access, MCPManagement: true, AuthRequired: true, Token: "controller-token"}).Handler()
	private := New(Options{Store: db, Projects: projects, MCP: access, AuthRequired: true, Token: "controller-token"}).Handler()
	request := func(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request(main, http.MethodGet, "/api/mcp/operations", "controller-token"); got.Code != http.StatusOK {
		t.Fatalf("loopback grant management status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(private, http.MethodGet, "/api/mcp/operations", "controller-token"); got.Code != http.StatusNotFound {
		t.Fatalf("private listener exposed grant management: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(private, http.MethodPost, "/mcp", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("private MCP accepted no scoped grant: status=%d body=%s", got.Code, got.Body.String())
	}
}
