package shell

import (
	"context"
	"encoding/json"
	"testing"

	"devboard/desktop/internal/teamlink"
	"devboard/internal/workspace"
)

type assistantTeam struct {
	*fakeTeam
	calls int
}

func (a *assistantTeam) AssistantBoard(_ context.Context, _ string, _ string, _ teamlink.BoardArgs) (any, error) {
	a.calls++
	return map[string]string{"id": "t1"}, nil
}

func TestAssistantWorkspaceMutationsRequireNativeConsentAndDoNotReplay(t *testing.T) {
	r := newRig(t, "Create ticket")
	client := &assistantTeam{fakeTeam: r.tm}
	r.o.Team = client
	raw := json.RawMessage(`{"projectId":"p1","title":"Extract CV","description":"Read the CV"}`)
	for i := 0; i < 2; i++ {
		if _, err := r.AssistantWorkspaceCall("wreq_1", "team:main", "create_ticket", raw); err != nil {
			t.Fatal(err)
		}
	}
	if client.calls != 1 || len(r.ui.asked) != 1 {
		t.Fatalf("replayed creation: %d calls, %d dialogs", client.calls, len(r.ui.asked))
	}
	if _, err := r.AssistantWorkspaceCall("wreq_2", "team:main", "create_ticket", raw); err == nil || client.calls != 1 {
		t.Fatal("a declined native confirmation created a ticket")
	}
	r.reg.items[1].State = workspace.StateUnavailable
	if _, err := r.AssistantWorkspaceCall("wreq_3", "team:main", "create_ticket", raw); err == nil {
		t.Fatal("an unavailable workspace was reached")
	}
	for _, id := range []string{"personal", "team:unknown"} {
		if _, err := r.AssistantWorkspaceCall("wreq_4", id, "create_ticket", raw); err == nil {
			t.Errorf("invalid workspace accepted: %s", id)
		}
	}
	for _, method := range []string{"AssistantWorkspaces", "AssistantWorkspaceCall"} {
		if _, err := r.Relay("personal", method, nil); err == nil {
			t.Errorf("workspace frame reached privileged shell method: %s", method)
		}
	}
}
