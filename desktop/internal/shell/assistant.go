package shell

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"werkbord/desktop/internal/teamlink"
	"werkbord/internal/workspace"
)

type workspaceCallResult struct {
	data       any
	err        error
	session    string
	proposalID string
	workspace  string
	method     string
	args       teamlink.BoardArgs
}
type workspaceCallCache struct {
	sync.Mutex
	results map[string]workspaceCallResult
}

var workspaceRequestID = regexp.MustCompile(`^wreq_[A-Za-z0-9_-]{1,64}$`)

type workspaceAuthorization struct {
	Pending bool `json:"pending"`
	Request struct {
		ID          string `json:"id"`
		ProposalID  string `json:"proposalId"`
		WorkspaceID string `json:"workspaceId"`
		Method      string `json:"method"`
		Args        any    `json:"args"`
	} `json:"request"`
}

type workspaceFailure struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func definiteWorkspaceFailure(err error) (workspaceFailure, bool) {
	var response *teamlink.ResponseError
	if !errors.As(err, &response) {
		return workspaceFailure{}, false
	}
	code := ""
	switch response.StatusCode {
	case 400, 422:
		code = "invalid_arguments"
	case 401, 403:
		code = "permission_denied"
	case 404, 409:
		code = "conflict"
	default:
		return workspaceFailure{}, false
	}
	return workspaceFailure{Kind: "werkbord_workspace_failure", Code: code, Message: response.Message}, true
}

// verifyWorkspaceRequest checks the bridge call against the controller's live request. Such a request is published only
// while appops is carrying out the stored proposal after the person confirmed it.
func (s *Shell) verifyWorkspaceRequest(ctx context.Context, session, request, proposalID, workspaceID, method string, args teamlink.BoardArgs) error {
	body, err := s.o.Launcher.AssistantWorkspaceRequest(ctx, session, request)
	if err != nil {
		return errors.New("the controller could not verify this request; check the board before retrying")
	}
	var auth workspaceAuthorization
	if err := json.Unmarshal(body, &auth); err != nil || !auth.Pending {
		return errors.New("this approved request is no longer active")
	}
	canonical, err := json.Marshal(auth.Request.Args)
	if err != nil {
		return errors.New("the approved request could not be checked")
	}
	approved, err := teamlink.DecodeBoardArgs(method, canonical)
	if err != nil || auth.Request.ID != request || auth.Request.ProposalID != proposalID || auth.Request.WorkspaceID != workspaceID || auth.Request.Method != method || approved != args {
		return errors.New("the desktop request does not match the approved task")
	}
	return nil
}

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

// AssistantWorkspaceCall is reachable only through the assistant frame's trusted shell. For creation, it verifies the
// controller's live request, which exists only after the stored proposal was approved. A request ID runs at most once.
func (s *Shell) AssistantWorkspaceCall(session, request, proposalID, id, method string, raw json.RawMessage) (any, error) {
	if !workspaceRequestID.MatchString(request) {
		return nil, errors.New("invalid workspace request")
	}
	if session == "" || len(session) > 100 {
		return nil, errors.New("invalid assistant session")
	}
	in, err := teamlink.DecodeBoardArgs(method, raw)
	if err != nil {
		return nil, err
	}
	// A read is answered quickly or not at all. Approved task creation is bounded separately from the person's confirmation.
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
	if method != "create_ticket" && method != "save_work_plan" {
		return client.AssistantBoard(ctx, id, method, in)
	}
	if proposalID == "" {
		return nil, errors.New("this task has no approved proposal")
	}
	s.workspaceCalls.Lock()
	defer s.workspaceCalls.Unlock()
	if s.workspaceCalls.results == nil {
		s.workspaceCalls.results = map[string]workspaceCallResult{}
	}
	if r, ok := s.workspaceCalls.results[request]; ok {
		if r.session != session || r.proposalID != proposalID || r.workspace != id || r.method != method || r.args != in {
			return nil, errors.New("this request ID was already used for another approved task")
		}
		return r.data, r.err
	}
	if err := s.verifyWorkspaceRequest(ctx, session, request, proposalID, id, method, in); err != nil {
		return nil, err
	}
	if len(s.workspaceCalls.results) >= 1000 {
		return nil, errors.New("restart Werkbord before creating more workspace tickets")
	}
	var result any
	cctx, ccancel := s.timeout(30 * time.Second)
	defer ccancel()
	result, err = client.AssistantBoard(cctx, id, method, in)
	if err != nil {
		if failure, definite := definiteWorkspaceFailure(err); definite {
			result, err = failure, nil
		}
	}
	s.workspaceCalls.results[request] = workspaceCallResult{data: result, err: err, session: session, proposalID: proposalID, workspace: id, method: method, args: in}
	return result, err
}
