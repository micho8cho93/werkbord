package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"werkbord/internal/appops"
	"werkbord/internal/domain"
)

// WorkspaceRequest is a semantic request to the person's connected desktop window. No service URL or credential
// is held by the controller. Mutations reach this transport only after appops has claimed the person's confirmation.
type WorkspaceRequest struct {
	ID          string `json:"id"`
	ProposalID  string `json:"proposalId,omitempty"`
	WorkspaceID string `json:"workspaceId"`
	Method      string `json:"method"`
	Args        any    `json:"args"`
}

type workspaceReply struct {
	data    json.RawMessage
	problem string
	code    string
}
type workspaceWait struct {
	session string
	request WorkspaceRequest
	reply   chan workspaceReply
}
type workspaceRequests struct {
	sync.Mutex
	pending map[string]workspaceWait
}

var workspaceIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,40}$`)

func (e *Engine) ConnectWorkspaces(ctx context.Context, session string, ws []appops.Workspace) error {
	if _, err := e.Session(ctx, session); err != nil {
		return err
	}
	if len(ws) > 20 {
		return fmt.Errorf("%w: connect at most 20 workspaces", domain.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, w := range ws {
		if !workspaceIDPattern.MatchString(w.ID) || w.Name == "" || len(w.Name) > 120 || seen[w.ID] {
			return fmt.Errorf("%w: invalid connected workspace", domain.ErrInvalid)
		}
		seen[w.ID] = true
	}
	e.ops.BindWorkspaces(session, ws, func(ctx context.Context, w appops.Workspace, method string, args any) (json.RawMessage, error) {
		return e.requestWorkspace(ctx, session, appops.ApprovedActionID(ctx), w.ID, method, args)
	})
	return nil
}

func (e *Engine) requestWorkspace(ctx context.Context, session, proposal, workspace, method string, args any) (json.RawMessage, error) {
	id := domain.NewID("wreq")
	request := WorkspaceRequest{ID: id, ProposalID: proposal, WorkspaceID: workspace, Method: method, Args: args}
	wait := workspaceWait{session: session, request: request, reply: make(chan workspaceReply, 1)}
	e.wrequests.Lock()
	if e.wrequests.pending == nil {
		e.wrequests.pending = map[string]workspaceWait{}
	}
	e.wrequests.pending[id] = wait
	e.wrequests.Unlock()
	defer func() { e.wrequests.Lock(); delete(e.wrequests.pending, id); e.wrequests.Unlock() }()
	e.hub(session).publish(Event{SessionID: session, Type: EventWorkspaceRequest, Workspace: &request})
	// Reads are answered at once. A creation stays live only while the desktop verifies and carries out the approved request.
	limit := 25 * time.Second
	if method == "create_ticket" || method == "save_work_plan" {
		limit = 120 * time.Second
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case reply := <-wait.reply:
		if reply.problem != "" {
			code := workspaceProblemCode(reply.code)
			return nil, &appops.Error{Code: code, Message: clipText(reply.problem, 500)}
		}
		return reply.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.base.Done():
		return nil, context.Canceled
	case <-timer.C:
		return nil, &appops.Error{Code: appops.CodeUnavailable, Message: "the desktop workspace did not answer; open Werkbord and check its board before retrying a change"}
	}
}

// WorkspaceRequestStatus is the controller-issued request the desktop verifies before carrying it out.
// It exists only while an approved assistant operation is waiting for the desktop to answer.
func (e *Engine) WorkspaceRequestStatus(session, id string) (WorkspaceRequest, bool) {
	e.wrequests.Lock()
	defer e.wrequests.Unlock()
	w, ok := e.wrequests.pending[id]
	return w.request, ok && w.session == session
}

func workspaceProblemCode(code string) string {
	switch code {
	case appops.CodePermissionDenied, appops.CodeInvalidArguments, appops.CodeConflict:
		return code
	default:
		return appops.CodeUnavailable
	}
}

func (e *Engine) AnswerWorkspace(session, id string, data json.RawMessage, problem, code string) error {
	if len(data) > 48<<10 || len(problem) > 1000 || (problem == "" && (!json.Valid(data) || code != "")) || (code != "" && code != appops.CodePermissionDenied && code != appops.CodeInvalidArguments && code != appops.CodeConflict) {
		return fmt.Errorf("%w: invalid workspace response", domain.ErrInvalid)
	}
	e.wrequests.Lock()
	defer e.wrequests.Unlock()
	w, ok := e.wrequests.pending[id]
	if !ok || w.session != session {
		return domain.ErrNotFound
	}
	delete(e.wrequests.pending, id)
	w.reply <- workspaceReply{data: data, problem: problem, code: code}
	return nil
}
