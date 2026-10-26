package shell

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"werkbord/desktop/internal/teamlink"
	"werkbord/desktop/internal/workspaces"
	"werkbord/internal/workspace"
)

type personalGrants struct {
	*fakeGrants
	status int
}

func (g *personalGrants) PersonalWorkspace(_ context.Context, id string) (workspaces.PersonalResponse, error) {
	if id != "pws_abcdefghijklmnop" {
		return workspaces.PersonalResponse{}, errors.New("unexpected request")
	}
	return workspaces.PersonalResponse{Status: g.status, Body: json.RawMessage(`{"visibility":{"teamWorkspaceIds":["team:main"]}}`)}, nil
}

type personalTeam struct {
	*fakeTeam
	reads   int
	revoked bool
}

func (t *personalTeam) AssistantBoard(_ context.Context, _ string, method string, _ teamlink.BoardArgs) (any, error) {
	if method != "list_projects" {
		return nil, errors.New("unexpected Team authority")
	}
	t.reads++
	if t.revoked {
		return nil, errors.New("permission denied")
	}
	return []map[string]string{{"id": "p1", "name": "Permitted project"}}, nil
}
func TestGeneratedTeamReadsRequireSavedAssociationAndLiveAccess(t *testing.T) {
	r := newRig(t)
	g := &personalGrants{fakeGrants: r.gr, status: 200}
	r.o.Grants = g
	team := &personalTeam{fakeTeam: r.tm}
	r.o.Team = team
	id := "pws_abcdefghijklmnop"
	if _, err := r.PersonalTeamProjects(id, "team:main"); err != nil {
		t.Fatal(err)
	}
	if team.reads != 1 {
		t.Fatal("not using existing authenticated bridge")
	}
	for _, target := range []string{"personal", "team:other", "https://bad.test"} {
		if _, err := r.PersonalTeamProjects(id, target); err == nil {
			t.Fatal("unassigned context allowed")
		}
	}
	for _, method := range []string{"PersonalWorkspaceCall", "PersonalTeamProjects"} {
		for _, frame := range []string{"personal", "team:main"} {
			if _, err := r.Relay(frame, method, nil); err == nil {
				t.Fatal("frame gained shell capability")
			}
		}
	}
	r.reg.items[1].State = workspace.StateOffline
	if _, err := r.PersonalTeamProjects(id, "team:main"); err == nil {
		t.Fatal("offline read")
	}
	if team.reads != 1 {
		t.Fatal("offline request reached Team")
	}
	r.reg.items[1].State = workspace.StateReady
	team.revoked = true
	if _, err := r.PersonalTeamProjects(id, "team:main"); err == nil {
		t.Fatal("revoked access displayed")
	}
	g.status = 404
	if _, err := r.PersonalTeamProjects(id, "team:main"); err == nil {
		t.Fatal("deleted workspace read Team")
	}
}
