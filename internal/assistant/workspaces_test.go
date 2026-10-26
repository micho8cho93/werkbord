package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"werkbord/internal/appops"
	"werkbord/internal/assistant/provider/fake"
	"werkbord/internal/domain"
)

func TestWorkspaceProblemCodesKeepPermissionAndValidationOutcomesDefinite(t *testing.T) {
	for _, code := range []string{appops.CodePermissionDenied, appops.CodeInvalidArguments, appops.CodeConflict} {
		if got := workspaceProblemCode(code); got != code {
			t.Errorf("workspace problem %q mapped to %q", code, got)
		}
	}
	if got := workspaceProblemCode(""); got != appops.CodeUnavailable {
		t.Fatalf("unknown workspace outcome mapped to %q", got)
	}
}

func TestWorkspaceRequestsAreBoundToTheirSessionAndAnsweredOnce(t *testing.T) {
	f := newEfx(t)
	s := f.session()
	if err := f.eng.ConnectWorkspaces(bg, s.ID, []appops.Workspace{{ID: "board_1", Name: "TestTeam"}}); err != nil {
		t.Fatal(err)
	}
	f.prov.Queue(fake.Reply(strings.TrimSuffix(callBlock("p1", "list_projects", map[string]any{}), "\n")), fake.Reply("The portfolio project is in TestTeam."))
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	ch, err := f.eng.Subscribe(ctx, s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.eng.Send(bg, s.ID, "Find my portfolio project"); err != nil {
		t.Fatal(err)
	}
	var request string
	for e := range ch {
		if e.Type == EventWorkspaceRequest {
			request = e.Workspace.ID
			if e.Workspace.Method != "list_projects" || !f.eng.WorkspaceRequestPending(s.ID, request) {
				t.Fatalf("request: %+v", e.Workspace)
			}
			data := json.RawMessage(`[{"id":"p1","name":"Portfolio"}]`)
			if err := f.eng.AnswerWorkspace("ast_other", request, data, "", ""); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("cross-session reply accepted: %v", err)
			}
			if err := f.eng.AnswerWorkspace(s.ID, request, data, "", ""); err != nil {
				t.Fatal(err)
			}
			if f.eng.WorkspaceRequestPending(s.ID, request) {
				t.Fatal("answered request still pending")
			}
			if err := f.eng.AnswerWorkspace(s.ID, request, data, "", ""); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal("replayed reply accepted")
			}
		}
		if isTerminal(e.Type) {
			if e.Type != EventCompleted || request == "" || !strings.Contains(f.lastPrompt(), "workspace:board_1/p1") {
				t.Fatalf("connected tool loop failed: %+v, %s", e, f.lastPrompt())
			}
			return
		}
	}
	t.Fatal("workspace turn did not finish")
}

func TestCancelledWorkspaceRequestsAreRemovedAndNeverRetried(t *testing.T) {
	f := newEfx(t)
	s := f.session()
	ctx, cancel := context.WithCancel(bg)
	errch := make(chan error, 1)
	go func() {
		_, err := f.eng.requestWorkspace(ctx, s.ID, "act_test", "board_1", "create_ticket", map[string]string{"title": "Example"})
		errch <- err
	}()
	deadline := time.Now().Add(time.Second)
	var id string
	for time.Now().Before(deadline) {
		f.eng.wrequests.Lock()
		for key := range f.eng.wrequests.pending {
			id = key
		}
		f.eng.wrequests.Unlock()
		if id != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-errch; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if id == "" || f.eng.WorkspaceRequestPending(s.ID, id) {
		t.Fatal("cancelled request still reachable")
	}
}

// WorkspaceRequestPending reports whether the desktop workspace request id is waiting for an answer in session.
func (e *Engine) WorkspaceRequestPending(session, id string) bool {
	e.wrequests.Lock()
	defer e.wrequests.Unlock()
	w, ok := e.wrequests.pending[id]
	return ok && w.session == session
}
