package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"werkbord/internal/localaccess"
)

// Every Individual memory, relationship, recommendation and publication route is
// owner-only. A Team connector's memory grant reaches exactly its own workspace's
// publication journal; no other local-access scope, and no phone/MCP path, reaches memory.
func TestAuditLocalAccessScopesNeverReachPrivateMemory(t *testing.T) {
	s := &Server{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	type route struct{ method, path, body string }
	owner := []route{
		{"GET", "/api/projects/p/memory", ""}, {"POST", "/api/projects/p/memory", `{"kind":"fact","summary":"x"}`},
		{"PATCH", "/api/projects/p/memory/m", `{"version":1}`}, {"DELETE", "/api/projects/p/memory/m", `{"version":1}`},
		{"GET", "/api/projects/p/memory/m/sources", ""}, {"GET", "/api/projects/p/memory/context?q=x", ""},
		{"GET", "/api/projects/p/relationships", ""}, {"POST", "/api/projects/p/relationships", `{}`},
		{"POST", "/api/projects/p/relationships/r/accept", `{"version":1}`}, {"POST", "/api/projects/p/relationships/discover", `{}`},
		{"GET", "/api/projects/p/recommendations", ""}, {"POST", "/api/projects/p/recommendations/feedback", `{}`},
		{"POST", "/api/memory/v1/shares", `{}`}, {"DELETE", "/api/memory/v1/shares/s", ""},
		{"GET", "/api/memory/v1/publication/grants", ""}, {"PUT", "/api/memory/v1/publication/grants", `{}`},
		{"GET", "/api/memory/v1/publication", ""}, {"POST", "/api/memory/v1/publication/review", `{}`},
		{"GET", "/api/local-access", ""}, {"POST", "/api/local-access", `{"scope":"memory-v1","workspaceId":"ws_one"}`},
	}
	for _, scope := range []string{"", "integration-v1", "execution-local-v1", "execution-dispatch-v1", "future-scope"} {
		for _, r := range owner {
			w := httptest.NewRecorder()
			s.scoped(localaccess.Entry{Scope: scope}, next).ServeHTTP(w, httptest.NewRequest(r.method, r.path, strings.NewReader(r.body)))
			if w.Code != 403 {
				t.Errorf("scope %q reached %s %s: %d", scope, r.method, r.path, w.Code)
			}
		}
	}
	grant := localaccess.Entry{Scope: "memory-v1", MemoryWorkspaceID: "ws_one"}
	for _, r := range owner {
		w := httptest.NewRecorder()
		s.scoped(grant, next).ServeHTTP(w, httptest.NewRequest(r.method, r.path, strings.NewReader(r.body)))
		if w.Code != 403 {
			t.Errorf("memory grant reached owner route %s %s: %d", r.method, r.path, w.Code)
		}
	}
	for _, tc := range []struct {
		entry              localaccess.Entry
		method, path, body string
		want               int
	}{
		{grant, "GET", "/api/memory/v1/publication/events?workspaceId=ws_one", "", 204},
		{grant, "POST", "/api/memory/v1/publication/events?workspaceId=ws_one", `{"eventId":"e","sequence":1,"record":{}}`, 204},
		{grant, "POST", "/api/memory/v1/publication/events?workspaceId=ws_one", `{"eventId":"e","sequence":1,"record":{},"path":"/etc"}`, 403},
		{grant, "GET", "/api/memory/v1/publication/events?workspaceId=ws_two", "", 403},
		{grant, "GET", "/api/memory/v1/publication/events", "", 403},
		{grant, "GET", "/api/memory/v1/shares?workspaceId=ws_two", "", 403},
		{grant, "GET", "/api/memory/v1/publication/events?workspaceId=ws_one&workspaceId=ws_two", "", 403},
		{localaccess.Entry{Scope: "memory-v1"}, "GET", "/api/memory/v1/publication/events?workspaceId=", "", 403},
		{grant, "GET", "/api/integration/v1/projects", "", 403},
		{grant, "GET", "/api/projects", "", 403},
		{grant, "GET", "/api/runners", "", 403},
		{grant, "GET", "/api/events", "", 403},
	} {
		w := httptest.NewRecorder()
		s.scoped(tc.entry, next).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
}
