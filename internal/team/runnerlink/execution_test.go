package runnerlink

import (
	"context"
	"devboard/internal/envelope"
	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type authorizedBridge struct {
	*fakeBridge
	deny   bool
	starts int
	runID  string
}

func (b *authorizedBridge) ExecutionApproval(context.Context, integration.ExecutionDispatch) (integration.ExecutionApproval, error) {
	if b.deny {
		return integration.ExecutionApproval{}, errors.New("local approval revoked")
	}
	return integration.ExecutionApproval{RunID: b.runID}, nil
}
func (b *authorizedBridge) DispatchExecution(context.Context, integration.ExecutionDispatch) (string, error) {
	b.starts++
	b.runID = "run_owned"
	return "run_owned", nil
}

type executionHost struct {
	fakeHandoffs
	p domain.Project
	k domain.Ticket
}

func (h *executionHost) Do(_ context.Context, _ string, path string, _ any, out any) error {
	switch v := out.(type) {
	case *domain.Project:
		*v = h.p
	case *domain.Ticket:
		*v = h.k
	default:
		return errors.New("unexpected route " + path)
	}
	return nil
}
func authorizedRig(t *testing.T) (*rig, *authorizedBridge, *executionHost, envelope.StartAuthorizedExecution) {
	r := newRig(t)
	bridge := &authorizedBridge{fakeBridge: r.bridge}
	host := &executionHost{p: domain.Project{ID: pr, Repository: rep}, k: domain.Ticket{ID: tk, ProjectID: pr, AssigneeID: bo, Assignment: 1, Title: "owned", Status: domain.TicketInProgress}}
	fence := service.ScheduleContext(ws, host.p, host.k)
	ref := devicestate.ExecutionRef{ExecutionID: "exe_approved", TeamProjectID: pr, TicketID: tk, Fence: fence, Context: fence, ExpiresAt: time.Now().Add(time.Hour)}
	if err := r.state.NoteExecution(ref); err != nil {
		t.Fatal(err)
	}
	r.h.Bridge = bridge
	r.h.Handoffs = host
	return r, bridge, host, envelope.StartAuthorizedExecution{ProjectID: pr, TicketID: tk, ExecutionID: ref.ExecutionID, FenceID: fence}
}
func TestAuthorizedMailboxEnforcesOwnerAssignmentLocalApprovalAndSignedIntegrity(t *testing.T) {
	for _, kind := range []string{"owner", "other_member", "forged", "altered", "expired", "replayed", "local_policy", "reassigned"} {
		t.Run(kind, func(t *testing.T) {
			r, bridge, host, payload := authorizedRig(t)
			signer, user := r.phone, bo
			if kind == "other_member" {
				signer, user = r.cyPhone, cy
			}
			m := r.msg(signer, user, envelope.ActionStartAuthorizedExecution, payload)
			var env envelope.Envelope
			_ = json.Unmarshal(m.Envelope, &env)
			switch kind {
			case "forged":
				env.Signature = "invalid"
			case "altered":
				env.UserID = cy
			case "expired":
				env, _ = envelope.Sign(r.phone, envelope.Request{WorkspaceID: ws, UserID: bo, TargetDeviceID: r.mac.DeviceID(), Action: envelope.ActionStartAuthorizedExecution, Payload: payload}, time.Now().Add(-time.Hour))
				m.ID = env.MessageID
			case "local_policy":
				bridge.deny = true
			case "reassigned":
				host.k.Assignment++
			}
			m.Envelope, _ = json.Marshal(env)
			res := r.h.Handle(bg, m)
			if kind == "owner" || kind == "replayed" {
				if res.State != domain.MessageDone || bridge.starts != 1 {
					t.Fatal(res, bridge.starts)
				}
				if kind == "replayed" {
					r.h.verified = nil
					res = r.h.Handle(bg, m)
					if res.State != domain.MessageDone || bridge.starts != 1 {
						t.Fatal("replay launched", res)
					}
				}
			} else if res.State != domain.MessageRefused || bridge.starts != 0 {
				t.Fatal("unauthorized remote start", res, bridge.starts)
			}
		})
	}
}
func TestAlteredCompletedMailboxCannotRetrieveAnOwnersCachedResult(t *testing.T) {
	r, _, _, payload := authorizedRig(t)
	m := r.msg(r.phone, bo, envelope.ActionStartAuthorizedExecution, payload)
	if res := r.h.Handle(bg, m); res.State != domain.MessageDone {
		t.Fatal(res)
	}
	var env envelope.Envelope
	_ = json.Unmarshal(m.Envelope, &env)
	env.UserID = cy
	m.Envelope, _ = json.Marshal(env)
	if res := r.h.Handle(bg, m); res.State != domain.MessageRefused {
		t.Fatal("altered request obtained cached result", res)
	}
}

func TestFreshSignedRequestRecoversCommittedScheduledRunWithoutAnotherClaim(t *testing.T) {
	r, bridge, host, payload := authorizedRig(t)
	payload.FenceID = "schedule_fence"
	if err := r.state.NoteExecution(devicestate.ExecutionRef{ExecutionID: payload.ExecutionID, TeamProjectID: pr, TicketID: tk, Fence: payload.FenceID, Context: service.ScheduleContext(ws, host.p, host.k), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	bridge.runID = "run_committed"
	// The host intentionally cannot return a schedule/claim. Recovery of a used
	// local authorization must not claim the request or launch again.
	for range 2 {
		m := r.msg(r.phone, bo, envelope.ActionStartAuthorizedExecution, payload)
		res := r.h.Handle(bg, m)
		if res.State != domain.MessageDone || res.Body["runId"] != bridge.runID || bridge.starts != 0 {
			t.Fatal("recovery attempted another claim", res, bridge.starts)
		}
	}
}
