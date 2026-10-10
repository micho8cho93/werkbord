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
	// A read is answered quickly or not at all. A creation waits for the person, whose time is not the service's: the service is
	// asked with a fresh limit once they have answered.
	ctx, cancel := s.timeout(20 * time.Second)
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
	// The person is shown the project by its name, as they know it, and not by an identifier.
	project := in.ProjectID
	if v, perr := client.AssistantBoard(ctx, id, "get_project", teamlink.BoardArgs{ProjectID: in.ProjectID}); perr == nil {
		if p, ok := v.(interface{ ProjectName() string }); ok && p.ProjectName() != "" {
			project = p.ProjectName()
		}
	}
	choice := s.o.UI.Ask(Dialog{Kind: Question, Title: fmt.Sprintf("Create a ticket in %s?", name),
		Message: fmt.Sprintf("Create %q in the project %q.\n\n%s\n\nThis adds a ticket to Backlog. It starts no agent work.", clipDialog(in.Title, 200), clipDialog(project, 120), clipDialog(in.Description, 600)),
		Buttons: []string{"Create ticket", "Cancel"}, Default: "Cancel", Cancel: "Cancel"})
	var result any
	if choice != "Create ticket" {
		err = errors.New("the ticket was not created")
	} else {
		cctx, ccancel := s.timeout(30 * time.Second)
		defer ccancel()
		result, err = client.AssistantBoard(cctx, id, method, in)
	}
	s.workspaceCalls.results[request] = workspaceCallResult{data: result, err: err}
	return result, err
}

// clipDialog shortens text for a dialog, which has no scroll bar.
func clipDialog(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
