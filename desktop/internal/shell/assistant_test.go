package shell

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"devboard/desktop/internal/teamlink"
	"devboard/internal/workspace"
)

type assistantTeam struct {
	*fakeTeam
	calls int // creations
}

// namedProject is a project as the app’s link describes it, which can say its name.
type namedProject struct{ name string }

func (p namedProject) ProjectName() string { return p.name }

func (a *assistantTeam) AssistantBoard(_ context.Context, _ string, method string, _ teamlink.BoardArgs) (any, error) {
	if method == "get_project" {
		return namedProject{"Personal Portfolio Website"}, nil
	}
	a.calls++
	return map[string]string{"id": "t1"}, nil
}

func TestAssistantWorkspaceMutationsRequireNativeConsentAndDoNotReplay(t *testing.T) {
	r := newRig(t)
	client := &assistantTeam{fakeTeam: r.tm}
	r.o.Team = client
	verifiedArgs := map[string]any{"projectId": "p1", "title": "Extract CV", "description": "Read the CV", "workMode": "", "plannedStart": "", "plannedEnd": "", "milestone": false}
	verifications := 0
	launcher := r.o.Launcher.(*fakeLauncher)
	launcher.assistantWorkspaceRequest = func(_ context.Context, session, request string) ([]byte, error) {
		verifications++
		if session != "ast_test" || request != "wreq_1" {
			return nil, errors.New("not authorised")
		}
		return json.Marshal(map[string]any{"pending": true, "request": map[string]any{
			"id": "wreq_1", "proposalId": "act_1", "workspaceId": "team:main", "method": "create_ticket", "args": verifiedArgs,
		}})
	}
	raw := json.RawMessage(`{"projectId":"p1","title":"Extract CV","description":"Read the CV"}`)
	for i := 0; i < 2; i++ {
		if _, err := r.AssistantWorkspaceCall("ast_test", "wreq_1", "act_1", "team:main", "create_ticket", raw); err != nil {
			t.Fatal(err)
		}
	}
	if client.calls != 1 || verifications != 1 || len(r.ui.asked) != 0 {
		t.Fatalf("replayed creation or requested a second confirmation: %d calls, %d dialogs", client.calls, len(r.ui.asked))
	}
	if _, err := r.AssistantWorkspaceCall("ast_test", "wreq_1", "act_other", "team:main", "create_ticket", raw); err == nil || client.calls != 1 {
		t.Fatal("a bridge call replayed a result for another proposal")
	}
	if _, err := r.AssistantWorkspaceCall("ast_test", "wreq_1", "act_1", "team:main", "create_ticket", json.RawMessage(`{"projectId":"p1","title":"Tampered"}`)); err == nil || client.calls != 1 {
		t.Fatal("a bridge call changed the approved task")
	}
	r.reg.items[1].State = workspace.StateUnavailable
	if _, err := r.AssistantWorkspaceCall("ast_test", "wreq_3", "act_3", "team:main", "create_ticket", raw); err == nil {
		t.Fatal("an unavailable workspace was reached")
	}
	for _, id := range []string{"personal", "team:unknown"} {
		if _, err := r.AssistantWorkspaceCall("ast_test", "wreq_4", "act_4", id, "create_ticket", raw); err == nil {
			t.Errorf("invalid workspace accepted: %s", id)
		}
	}
	for _, method := range []string{"AssistantWorkspaces", "AssistantWorkspaceCall"} {
		if _, err := r.Relay("personal", method, nil); err == nil {
			t.Errorf("workspace frame reached privileged shell method: %s", method)
		}
	}
}

func TestDefiniteWorkspaceFailuresKeepKnownOutcomesActionable(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "invalid_arguments"}, {403, "permission_denied"}, {409, "conflict"}, {422, "invalid_arguments"}} {
		failure, ok := definiteWorkspaceFailure(&teamlink.ResponseError{StatusCode: tc.status, Message: "the request was refused"})
		if !ok || failure.Kind != "werkbord_workspace_failure" || failure.Code != tc.code || failure.Message != "the request was refused" {
			t.Errorf("status %d: %+v, definite %v", tc.status, failure, ok)
		}
	}
	for _, status := range []int{408, 429, 500, 503} {
		if _, ok := definiteWorkspaceFailure(&teamlink.ResponseError{StatusCode: status, Message: "the result is unknown"}); ok {
			t.Errorf("status %d was treated as a definite failure", status)
		}
	}
}
