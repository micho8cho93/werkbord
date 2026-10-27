// Package clientpolicy is the default-deny desktop control contract. Both the
// controller and loopback gateway use it; a new owner route is never inherited.
package clientpolicy

import (
	"net/http"
	"strings"
	"werkbord/internal/domain"
)

var Routes = []string{
	"GET /api/client/v1/self", "GET /api/client/v1/capabilities",
	"GET /api/projects", "GET /api/projects/{pid}",
	"GET /api/labels", "POST /api/labels", "PATCH /api/labels/{id}", "DELETE /api/labels/{id}",
	"GET /api/agents", "GET /api/agents/{id}/options", "GET /api/runners",
	"GET /api/settings", "GET /api/onboarding", "GET /api/control-center", "GET /api/workspace/v1/summary",
	"GET /api/events", "GET /api/analytics", "GET /api/projects/{pid}/analytics",
	"GET /api/projects/{pid}/schedule", "GET /api/projects/{pid}/orchestration", "GET /api/projects/{pid}/timeline",
	"GET /api/projects/{pid}/tasks", "POST /api/projects/{pid}/tasks", "PATCH /api/projects/{pid}/tasks/{id}",
	"POST /api/projects/{pid}/tasks/archive-done", "GET /api/projects/{pid}/tasks/{id}/runs", "POST /api/projects/{pid}/tasks/{id}/runs",
	"GET /api/projects/{pid}/runs", "GET /api/projects/{pid}/activity", "GET /api/projects/{pid}/runs/{id}",
	"GET /api/projects/{pid}/runs/{id}/events", "POST /api/projects/{pid}/runs/{id}/input",
	"POST /api/projects/{pid}/runs/{id}/finish", "POST /api/projects/{pid}/runs/{id}/stop",
	"POST /api/projects/{pid}/runs/{id}/continue", "POST /api/projects/{pid}/runs/{id}/handoff", "PUT /api/projects/{pid}/runs/{id}/handoff",
	"PATCH /api/projects/{pid}/runs/{id}/assessment",
	"GET /api/projects/{pid}/questions", "GET /api/projects/{pid}/questions/{id}", "POST /api/projects/{pid}/questions/{id}/answer",
	"GET /api/projects/{pid}/worktrees/{id}",
}
var manifest = func() *http.ServeMux {
	m := http.NewServeMux()
	for _, p := range Routes {
		m.HandleFunc(p, func(http.ResponseWriter, *http.Request) {})
	}
	return m
}()

func Allowed(r *http.Request) bool {
	// Do not let ServeMux canonicalisation widen the controller/gateway contract.
	if strings.Contains(r.URL.Path, "//") || r.URL.RawPath != "" {
		return false
	}
	for _, s := range strings.Split(r.URL.Path, "/") {
		if s == "." || s == ".." {
			return false
		}
	}
	_, pattern := manifest.Handler(r)
	return pattern != "" && r.Method != "HEAD"
}

// EventAllowed excludes private Personal/assistant/memory events, including
// future event types. Sequence gaps remain valid resume cursors.
func EventAllowed(e domain.Event) bool {
	switch e.Type {
	case domain.EventProjectRegistered, domain.EventProjectInspected, domain.EventProjectUpdated,
		domain.EventLabelCreated, domain.EventLabelUpdated, domain.EventLabelDeleted,
		domain.EventTaskCreated, domain.EventTaskUpdated, domain.EventRunStateChanged,
		domain.EventWorktreeCreated, domain.EventWorktreeRemoving, domain.EventWorktreeRemoved,
		domain.EventQuestionAnswered, domain.EventQuestionCancelled,
		domain.EventAgentStarted, domain.EventAgentOutput, domain.EventAgentQuestion, domain.EventAgentWaiting,
		domain.EventAgentBlocked, domain.EventAgentResumed, domain.EventAgentCompleted, domain.EventAgentFailed, domain.EventAgentStopped:
		return true
	}
	return false
}
