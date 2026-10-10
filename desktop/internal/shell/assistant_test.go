package shell

import (
	"context"
	"encoding/json"
	"strings"
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
	// The person is asked about the project by its name, and about the workspace by its name.
	if d := r.ui.asked[0]; !strings.Contains(d.Message, `"Personal Portfolio Website"`) || strings.Contains(d.Message, "p1") || !strings.Contains(d.Title, "Acme") {
		t.Fatalf("the dialog: %+v", d)
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
