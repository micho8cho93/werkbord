package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"devboard/desktop/internal/teamlink"
	"devboard/internal/workspace"
)

type workspaceCallResult struct {
	data any
	err  error
}
type workspaceCallCache struct {
	sync.Mutex
	results map[string]workspaceCallResult
}

var workspaceRequestID = regexp.MustCompile(`^wreq_[A-Za-z0-9_-]{1,64}$`)

// AssistantWorkspaces is callable by the app's own shell only. Workspace frames have no relay for it.
func (s *Shell) AssistantWorkspaces() ([]map[string]string, error) {
	ctx, cancel := s.timeout(10 * time.Second)
	defer cancel()
	view := s.o.Workspaces.Refresh(ctx)
	out := []map[string]string{}
	for _, w := range view.Items {
		if w.Kind == workspace.KindTeam && w.State == workspace.StateReady {
			out = append(out, map[string]string{"id": w.ID, "name": w.Name})
		}
	}
	return out, nil
}

// AssistantWorkspaceCall is reachable only through the assistant frame's trusted shell. It rechecks the app's
// workspace registry, validates a fixed operation, strips the API result and asks in a native dialog before creation.
// A request ID is executed at most once even when a frame reloads or its response is lost.
func (s *Shell) AssistantWorkspaceCall(request, id, method string, raw json.RawMessage) (any, error) {
	if !workspaceRequestID.MatchString(request) {
		return nil, errors.New("invalid workspace request")
	}
	in, err := teamlink.DecodeBoardArgs(method, raw)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.timeout(45 * time.Second)
	defer cancel()
	view := s.o.Workspaces.Refresh(ctx)
	name := ""
	for _, w := range view.Items {
		if w.ID == id && w.Kind == workspace.KindTeam && w.State == workspace.StateReady {
			name = w.Name
		}
	}
	if name == "" {
		return nil, errors.New("that workspace is not connected on this computer")
	}
	client, ok := s.o.Team.(interface {
		AssistantBoard(context.Context, string, string, teamlink.BoardArgs) (any, error)
	})
	if !ok {
		return nil, errors.New("workspace operations are unavailable")
	}
	if method != "create_ticket" {
		return client.AssistantBoard(ctx, id, method, in)
	}
	s.workspaceCalls.Lock()
	defer s.workspaceCalls.Unlock()
	if s.workspaceCalls.results == nil {
		s.workspaceCalls.results = map[string]workspaceCallResult{}
	}
	if r, ok := s.workspaceCalls.results[request]; ok {
		return r.data, r.err
	}
	if len(s.workspaceCalls.results) >= 1000 {
		return nil, errors.New("restart Werkbord before creating more workspace tickets")
	}
	choice := s.o.UI.Ask(Dialog{Kind: Question, Title: fmt.Sprintf("Create a ticket in %s?", name),
		Message: fmt.Sprintf("Create %q in project %s.\n\n%s\n\nThis adds a ticket to Backlog. It starts no agent work.", in.Title, in.ProjectID, in.Description),
		Buttons: []string{"Create ticket", "Cancel"}, Default: "Cancel", Cancel: "Cancel"})
	var result any
	if choice != "Create ticket" {
		err = errors.New("the ticket was not created")
	} else {
		result, err = client.AssistantBoard(ctx, id, method, in)
	}
	s.workspaceCalls.results[request] = workspaceCallResult{data: result, err: err}
	return result, err
}
