package api

import (
	"devboard/internal/localaccess"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutionScopesCannotMintOrBroadenAuthorization(t *testing.T) {
	cases := []struct {
		scope, method, path, body string
		allowed                   bool
	}{
		{"integration-v1", "POST", "/api/execution/v1/dispatch", `{"executionId":"exe_x","fence":"a"}`, false},
		{"", "POST", "/api/execution/v1/approvals", `{}`, false},
		{"execution-dispatch-v1", "POST", "/api/execution/v1/approvals", `{}`, false},
		{"execution-dispatch-v1", "POST", "/api/execution/v1/preview", `{}`, false},
		{"execution-dispatch-v1", "GET", "/api/projects/prj_x/runs", ``, false},
		{"execution-dispatch-v1", "POST", "/api/projects/prj_x/tasks/tsk_x/runs", `{}`, false},
		{"execution-dispatch-v1", "POST", "/api/execution/v1/dispatch", `{"executionId":"exe_x","fence":"a","policy":{"interaction":"autonomous"}}`, false},
		{"execution-dispatch-v1", "POST", "/api/execution/v1/dispatch", `{"executionId":"exe_x","fence":"a"}`, true},
		{"execution-local-v1", "POST", "/api/execution/v1/approvals", `{"preview":{},"expiresAt":"2026-10-10T00:00:00Z"}`, true},
		{"execution-local-v1", "POST", "/api/projects/prj_x/tasks/tsk_x/runs", `{}`, false},
		{"execution-local-v1", "PATCH", "/api/projects/prj_x/tasks/tsk_x", `{"execution":{"runner":"rnr_other"}}`, false},
	}
	s := New(Options{})
	for _, tc := range cases {
		t.Run(tc.scope+tc.method+tc.path+tc.body, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			s.scoped(localaccess.Entry{Scope: tc.scope}, next).ServeHTTP(w, req)
			if called != tc.allowed {
				t.Fatalf("called=%v code=%d", called, w.Code)
			}
		})
	}
}
