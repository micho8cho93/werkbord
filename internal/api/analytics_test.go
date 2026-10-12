package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnalyticsPermissionBoundaries(t *testing.T) {
	sc := newScoped(t)
	sc.task(t, "private task")
	program := sc.mint(t, "analytics test")
	paths := []string{"/api/analytics", "/api/projects/" + sc.project.ID + "/analytics"}
	for _, path := range paths {
		for _, entry := range []struct {
			token string
			want  int
		}{{"", 401}, {program, 403}, {sc.owner, 200}} {
			status, body := call(t, http.MethodGet, sc.url+path, entry.token, "")
			if status != entry.want {
				t.Fatalf("%s: status %d wanted %d: %s", path, status, entry.want, body)
			}
			if status == 200 && (strings.Contains(string(body), "private task") || strings.Contains(string(body), "description") || strings.Contains(string(body), "prompt")) {
				t.Fatalf("private content leaked: %s", body)
			}
		}
	}
	status, _ := call(t, "GET", sc.url+paths[1]+"?project=other", sc.owner, "")
	if status != 404 {
		t.Fatalf("project override: %d", status)
	}
	for _, suffix := range []string{"?days=1", "?days=0", "?days=abc"} {
		status, _ := call(t, "GET", sc.url+paths[0]+suffix, sc.owner, "")
		if status != 400 {
			t.Fatalf("range %s: %d", suffix, status)
		}
	}
	status, _ = call(t, "GET", sc.url+"/api/analytics?project=missing", sc.owner, "")
	if status != 404 {
		t.Fatalf("missing project: %d", status)
	}
}
