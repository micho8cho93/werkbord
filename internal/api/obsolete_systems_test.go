package api

import (
	"net/http"
	"testing"
)

func TestGeneratedWorkspaceEndpointsAreAbsent(t *testing.T) {
	s := newTestServer(t, nil)
	for _, path := range []string{
		"/api/personal/v1/capabilities",
		"/api/personal/v1/workspaces",
		"/api/personal/v1/collections",
		"/api/personal/v1/collections/pcl_abcdefghijklmnop/records",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			status, body := call(t, method, s.URL+path, "", "")
			if status != http.StatusNotFound {
				t.Errorf("%s %s: %d %s", method, path, status, body)
			}
		}
	}
}
