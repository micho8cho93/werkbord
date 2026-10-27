package clientpolicy

import (
	"net/http/httptest"
	"testing"
	"werkbord/internal/domain"
)

func TestDefaultDenyManifestAndEventProjection(t *testing.T) {
	for _, path := range []string{"/api/connections", "/api/phones", "/api/clients", "/api/mcp/grants", "/api/personal/v1/workspaces", "/api/assistant/sessions", "/api/runner/sync", "/api/folders", "/api/projects/prj_x/git/push", "/api/projects/prj_x/terminals"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if Allowed(httptest.NewRequest(method, path, nil)) {
				t.Fatalf("exposed %s %s", method, path)
			}
		}
	}
	if !Allowed(httptest.NewRequest("POST", "/api/projects/prj_x/tasks/task_x/runs", nil)) {
		t.Fatal("run dispatch missing")
	}
	if Allowed(httptest.NewRequest("GET", "/api/projects//tasks", nil)) {
		t.Fatal("canonicalisation widened scope")
	}
	for _, kind := range []domain.EventType{"personal.record.created", "assistant.message", "memory.updated", "future.unknown", domain.EventSettingsUpdated} {
		if EventAllowed(domain.Event{Type: kind}) {
			t.Fatalf("leaked %s", kind)
		}
	}
	if !EventAllowed(domain.Event{Type: domain.EventAgentQuestion}) {
		t.Fatal("agent questions excluded")
	}
}
