// Package runnerlink carries a person's request from one of their devices to the Werkbord on another, without the
// workspace in between being able to forge it, and without Team running anything.
//
// The asking side (Sender) signs a request with its own device key and gives it to a Workspace Host. The device it is for
// (Handler) collects it and checks it all again itself: the signature against the sender's registered public key, the
// expiry, that it is addressed to this device, that it was made by a device of this device's own person, that this
// computer's person has approved that device as a sender, and that it has not been seen before. Only then does it pass the
// request, as one of five meanings, to the person's own Werkbord through the narrow bridge (internal/team/localwerkbord),
// which applies Werkbord's own execution policies and approvals.
//
// Starting work needs one more thing: a permission the person gave on this computer for that task. The request names it; the
// sender cannot make one, and the workspace cannot either.
//
// There is no path through here that runs a command, reads a file or reaches a computer: the five actions are identifiers
// (internal/envelope), and what the bridge can send is the list the individual product's local access allows.
package runnerlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"devboard/internal/envelope"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/localwerkbord"
)

// Bridge is what the handler needs of the person's own Werkbord. *localwerkbord.Client is one.
type Bridge interface {
	ProjectFor(ctx context.Context, repository string) (localwerkbord.Project, error)
	CreateTask(ctx context.Context, projectID string, t localwerkbord.NewTask) (string, error)
	StartRun(ctx context.Context, projectID, taskID string) (string, error)
	StopRun(ctx context.Context, runID string) error
	Answer(ctx context.Context, runID, questionID, option, reply string) error
	Status(ctx context.Context) (localwerkbord.Status, error)
}

var _ Bridge = (*localwerkbord.Client)(nil)

// Handoffs is where the context of a ticket the person holds comes from: their workspace.
type Handoffs interface {
	Handoff(ctx context.Context, projectID, ticketID string) (hostclient.Handoff, error)
}

// Self is the device the handler runs on.
type Self struct {
	DeviceID string
	// MemberID is the person who owns it.
	MemberID string
}

// Handler acts, on this device, on the requests its person made from their other devices.
type Handler struct {
	Self     Self
	Verifier *envelope.Verifier
	State    *devicestate.State
	// Bridge is the person's own Werkbord, or nil when it is not connected.
	Bridge   Bridge
	Handoffs Handoffs
	// SourceRef is where a task opened here says it came from (a link a person can follow); optional.
	SourceRef func(projectID, ticketID string) string
	Log       *slog.Logger

	// verified holds requests that passed their checks and are waiting to be retried because Werkbord was not running, so that
	// the retry is not mistaken for a replay. It is in memory only: after a restart such a request is refused, never done twice.
	mu       sync.Mutex
	workMu   sync.Mutex
	verified map[string]envelope.Verified
}

// Result is what to tell the workspace about a request.
type Result struct {
	State domain.MessageState
	Body  map[string]any
	// Retry: nothing was decided (Werkbord is not running right now); leave the request waiting, and try again, until it expires.
	Retry bool
}

func refuse(format string, a ...any) Result {
	return Result{State: domain.MessageRefused, Body: map[string]any{"reason": clip(fmt.Sprintf(format, a...), 200)}}
}

func done(body map[string]any) Result {
	if body == nil {
		body = map[string]any{}
	}
	return Result{State: domain.MessageDone, Body: body}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Handle checks a request and does what it asks, or says why not. It does each request at most once, however many times the
// workspace hands it over.
func (h *Handler) Handle(ctx context.Context, m domain.DeviceMessage) Result {
	h.workMu.Lock()
	defer h.workMu.Unlock()
	if o, ok := h.State.Outcome(m.ID); ok {
		var body map[string]any
		_ = json.Unmarshal(o.Result, &body)
		return Result{State: domain.MessageState(o.State), Body: body}
	}
	res, verified := h.process(ctx, m)
	if res.Retry {
		return res
	}
	h.mu.Lock()
	delete(h.verified, m.ID)
	h.mu.Unlock()
	if !verified {
		// A request that did not verify is refused each time it is handed over and remembered as nothing: only what a verified
		// request was given is kept, so that a damaged copy cannot use up the ID of a genuine one.
		return res
	}
	raw, _ := json.Marshal(res.Body)
	if err := h.State.RecordOutcome(devicestate.Outcome{MessageID: m.ID, State: string(res.State), Result: raw}); err != nil && h.Log != nil {
		h.Log.Warn("could not remember what was done with a request", "err", err)
	}
	return res
}

// process returns the result and whether the request verified (everything after that is the request's own business).
func (h *Handler) process(ctx context.Context, m domain.DeviceMessage) (Result, bool) {
	var env envelope.Envelope
	if err := json.Unmarshal(m.Envelope, &env); err != nil || env.MessageID != m.ID {
		return refuse("the request is not well formed"), false
	}
	h.mu.Lock()
	ver, ok := h.verified[m.ID]
	h.mu.Unlock()
	var err error
	if !ok {
		ver, err = h.Verifier.Verify(ctx, env)
	} else {
		// A retry remains the exact signed request, within its lifetime, from a device that is still valid.
		if !bytes.Equal(env.SigningBytes(), ver.Envelope.SigningBytes()) || env.Signature != ver.Envelope.Signature {
			return refuse("the request changed while it was waiting"), false
		}
		now := time.Now()
		if h.Verifier.Now != nil {
			now = h.Verifier.Now()
		}
		if now.After(env.ExpiryTime().Add(envelope.DefaultSkew)) {
			return refuse("the request had expired"), true
		}
		dev, derr := h.Verifier.Directory.Device(ctx, env.WorkspaceID, env.DeviceID)
		if derr != nil || dev.Revoked || dev.OwnerID != ver.Device.OwnerID || dev.WorkspaceID != env.WorkspaceID || !bytes.Equal(dev.PublicKey, ver.Device.PublicKey) {
			return refuse("the sending device is no longer trusted"), true
		}
	}
	if err != nil {
		// What is wrong with it, and nothing about anyone's key.
		switch {
		case errors.Is(err, envelope.ErrExpired):
			return refuse("the request had expired"), false
		case errors.Is(err, envelope.ErrReplay):
			return refuse("this request was already handled"), false
		case errors.Is(err, envelope.ErrRevoked):
			return refuse("the device that sent it has been revoked"), false
		}
		return refuse("the request does not verify (%v)", err), false
	}
	if ver.Device.OwnerID != h.Self.MemberID {
		return refuse("the request is from someone who does not own this computer"), true
	}
	if env.DeviceID != h.Self.DeviceID && !h.State.IsApprovedSender(env.DeviceID) {
		return refuse("this computer has not approved requests from that device yet: approve it in Werkbord Team on this computer"), true
	}
	h.mu.Lock()
	if h.verified == nil {
		h.verified = map[string]envelope.Verified{}
	}
	h.verified[m.ID] = ver
	h.mu.Unlock()
	if h.Bridge == nil {
		return Result{Retry: true, State: domain.MessageQueued}, true
	}
	var res Result
	switch p := ver.Payload.(type) {
	case envelope.FetchRunnerStatus:
		res = h.status(ctx)
	case envelope.OpenTicketOnRunner:
		res = h.open(ctx, env, p)
	case envelope.StartApprovedRun:
		res = h.start(ctx, p)
	case envelope.CancelRun:
		res = h.fromBridge(h.Bridge.StopRun(ctx, p.RunID), nil)
	case envelope.RespondToAgentQuestion:
		res = h.fromBridge(h.Bridge.Answer(ctx, p.RunID, p.QuestionID, p.OptionID, p.Reply), nil)
	default:
		res = refuse("this computer does not know that request")
	}
	return res, true
}

// fromBridge turns Werkbord's answer into the result.
func (h *Handler) fromBridge(err error, body map[string]any) Result {
	switch {
	case err == nil:
		return done(body)
	case errors.Is(err, localwerkbord.ErrNotRunning):
		return Result{Retry: true, State: domain.MessageQueued}
	case errors.Is(err, localwerkbord.ErrAccessDenied):
		return refuse("Werkbord on this computer no longer accepts Werkbord Team: connect it again in Werkbord Team")
	}
	var se *localwerkbord.StatusError
	if errors.As(err, &se) {
		return refuse("%s", se.Message)
	}
	return refuse("Werkbord could not do that: %v", err)
}

func (h *Handler) status(ctx context.Context) Result {
	st, err := h.Bridge.Status(ctx)
	if err != nil {
		return h.fromBridge(err, nil)
	}
	var approvals []map[string]string
	for _, a := range h.State.Approvals() {
		if len(approvals) < 8 {
			approvals = append(approvals, map[string]string{"taskId": a.TaskID, "approvalId": a.ID, "ticket": a.Ticket})
		}
	}
	return done(map[string]any{"status": st, "approvals": approvals, "remoteStart": string(h.State.Settings().RemoteStart)})
}

func (h *Handler) open(ctx context.Context, env envelope.Envelope, p envelope.OpenTicketOnRunner) Result {
	set := h.State.Settings()
	if !set.RemoteOpen {
		return refuse("opening tickets from other devices is turned off on this computer")
	}
	ho, err := h.Handoffs.Handoff(ctx, p.ProjectID, p.TicketID)
	if err != nil {
		if errors.Is(err, hostclient.ErrUnreachable) {
			return Result{Retry: true, State: domain.MessageQueued}
		}
		return refuse("%v", err)
	}
	proj, err := h.Bridge.ProjectFor(ctx, ho.Git.Repository)
	if err != nil {
		return h.fromBridge(err, nil)
	}
	title := []rune(ho.Ticket.Key + ": " + ho.Ticket.Title)
	if len(title) > 200 {
		title = title[:200]
	}
	task := localwerkbord.NewTask{Title: string(title), Description: ho.Prompt, WorkBranch: ho.Git.Branch, BaseBranch: ho.Git.BaseBranch}
	if h.SourceRef != nil {
		task.SourceRef = h.SourceRef(p.ProjectID, p.TicketID)
	}
	id, err := h.Bridge.CreateTask(ctx, proj.ID, task)
	if err != nil {
		return h.fromBridge(err, nil)
	}
	_ = h.State.NoteOpened(devicestate.Opened{TaskID: id, ProjectID: proj.ID, Ticket: ho.Ticket.Key, FromName: env.DeviceID})
	body := map[string]any{"taskId": id, "ticket": ho.Ticket.Key}
	if set.RemoteStart == devicestate.RemoteStartAuto {
		a, err := h.State.Allow(id, proj.ID, ho.Ticket.Key)
		if err == nil {
			body["approvalId"] = a.ID
		}
	}
	return done(body)
}

func (h *Handler) start(ctx context.Context, p envelope.StartApprovedRun) Result {
	if h.State.Settings().RemoteStart == devicestate.RemoteStartOff {
		return refuse("starting work from another device is turned off on this computer")
	}
	a, err := h.State.Approval(p.ApprovalID, p.TaskID)
	if err != nil {
		return refuse("this computer has not approved starting that task: approve it in Werkbord Team on this computer")
	}
	// Spend before dispatch. A crash or a lost answer must never turn one approval into two starts.
	if _, err := h.State.Consume(p.ApprovalID, p.TaskID); err != nil {
		return refuse("that approval was already used")
	}
	runID, err := h.Bridge.StartRun(ctx, a.ProjectID, p.TaskID)
	if err != nil {
		return h.fromBridge(err, nil)
	}
	return done(map[string]any{"runId": runID})
}

// ---- the asking side ----

// Host is what a sender needs of the workspace.
type Host interface {
	Send(ctx context.Context, e envelope.Envelope) (domain.DeviceMessage, error)
	Message(ctx context.Context, id string) (domain.DeviceMessage, error)
}

var _ Host = (*hostclient.Client)(nil)

// Sender makes a request from this device, signs it with this device's own key, and gives it to the workspace.
type Sender struct {
	Signer      envelope.Signer
	WorkspaceID string
	// UserID is the person who owns this device.
	UserID string
	Host   Host
	Now    func() time.Time
}

// Ask signs a request for one of this person's runners and hands it to the workspace. It returns the stored message, whose
// ID is how its outcome is looked up (Host.Message).
func (s *Sender) Ask(ctx context.Context, target string, action envelope.Action, payload any) (domain.DeviceMessage, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	e, err := envelope.Sign(s.Signer, envelope.Request{WorkspaceID: s.WorkspaceID, UserID: s.UserID, TargetDeviceID: target, Action: action, Payload: payload}, now)
	if err != nil {
		return domain.DeviceMessage{}, err
	}
	return s.Host.Send(ctx, e)
}
