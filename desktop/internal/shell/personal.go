package shell

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"devboard/desktop/internal/teamlink"
	"devboard/desktop/internal/workspaces"
	"devboard/internal/domain"
	"devboard/internal/workspace"
)

// PersonalWorkspaceCall is shell-only. No workspace/assistant frame relay exposes it.
func (s *Shell) PersonalWorkspaceCall(method, path string, body json.RawMessage) (workspaces.PersonalResponse, error) {
	client, ok := s.o.Grants.(interface {
		PersonalWorkspaceCall(context.Context, string, string, json.RawMessage) (workspaces.PersonalResponse, error)
	})
	if !ok {
		return workspaces.PersonalResponse{}, errors.New("saved workspaces are unavailable")
	}
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	return client.PersonalWorkspaceCall(ctx, method, path, body)
}

// PersonalTeamProjects rechecks both the saved display association and live membership.
// The only Team operation is the existing authenticated, permission-filtered list_projects.
// Generated UI receives plain project names/IDs and an observation time, never bridge methods.
func (s *Shell) PersonalTeamProjects(workspaceID, teamID string) (any, error) {
	if !domain.ValidPersonalID(workspaceID, domain.PrefixPersonalWorkspace) {
		return nil, errors.New("invalid personal workspace")
	}
	reader, ok := s.o.Grants.(interface {
		PersonalWorkspace(context.Context, string) (workspaces.PersonalResponse, error)
	})
	if !ok {
		return nil, errors.New("saved workspaces are unavailable")
	}
	ctx, cancel := s.timeout(15 * time.Second)
	defer cancel()
	response, err := reader.PersonalWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if response.Status != 200 {
		return nil, errors.New("personal workspace is unavailable")
	}
	var saved domain.PersonalWorkspace
	if json.Unmarshal(response.Body, &saved) != nil {
		return nil, errors.New("invalid personal workspace")
	}
	assigned := false
	for _, id := range saved.Visibility.TeamWorkspaceIDs {
		if id == teamID {
			assigned = true
		}
	}
	if !assigned {
		return nil, errors.New("this Team is not assigned to the personal workspace")
	}
	connected := false
	for _, w := range s.o.Workspaces.Refresh(ctx).Items {
		if w.ID == teamID && w.Kind == workspace.KindTeam && w.State == workspace.StateReady {
			connected = true
		}
	}
	if !connected {
		return nil, errors.New("Team access is unavailable. Reconnect or check your membership")
	}
	client, ok := s.o.Team.(interface {
		AssistantBoard(context.Context, string, string, teamlink.BoardArgs) (any, error)
	})
	if !ok {
		return nil, errors.New("Team project reads are unavailable")
	}
	projects, err := client.AssistantBoard(ctx, teamID, "list_projects", teamlink.BoardArgs{})
	if err != nil {
		return nil, errors.New("Team projects could not be read. Reconnect or check your permissions")
	}
	return map[string]any{"projects": projects, "at": time.Now().UTC().Format(time.RFC3339)}, nil
}
