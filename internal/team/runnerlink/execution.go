package runnerlink

import (
	"context"
	"devboard/internal/envelope"
	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

type executionBridge interface {
	ExecutionApproval(context.Context, integration.ExecutionDispatch) (integration.ExecutionApproval, error)
	DispatchExecution(context.Context, integration.ExecutionDispatch) (string, error)
}
type contextHost interface {
	Do(context.Context, string, string, any, any) error
}

// A valid signature conveys identity only. It cannot choose a model, permissions
// or runner, and cannot create approval. Current ticket ownership is rechecked.
func (h *Handler) startAuthorized(ctx context.Context, workspace string, p envelope.StartAuthorizedExecution) Result {
	if h.State.Settings().RemoteStart == devicestate.RemoteStartOff {
		return refuse("remote starts are turned off")
	}
	bridge, ok := h.Bridge.(executionBridge)
	if !ok {
		return refuse("update and reconnect this device's Individual controller")
	}
	host, ok := h.Handoffs.(contextHost)
	if !ok {
		return refuse("workspace context unavailable")
	}
	var ref *devicestate.ExecutionRef
	for _, v := range h.State.Executions() {
		if v.ExecutionID == p.ExecutionID && v.TeamProjectID == p.ProjectID && v.TicketID == p.TicketID && v.Fence == p.FenceID {
			v := v
			ref = &v
			break
		}
	}
	if ref == nil {
		return refuse("approve this exact execution locally first")
	}
	var project domain.Project
	var ticket domain.Ticket
	if err := host.Do(ctx, "GET", "/projects/"+p.ProjectID, nil, &project); err != nil {
		return refuse("current project authority unavailable")
	}
	if err := host.Do(ctx, "GET", "/projects/"+p.ProjectID+"/tickets/"+p.TicketID, nil, &ticket); err != nil {
		return refuse("current ticket authority unavailable")
	}
	if ticket.AssigneeID != h.Self.MemberID || !ticket.Status.Held() || ticket.ArchivedAt != nil || project.Archived || service.ScheduleContext(workspace, project, ticket) != ref.Context {
		return refuse("ticket assignment or context changed")
	}
	in := integration.ExecutionDispatch{ExecutionID: p.ExecutionID, Fence: p.FenceID}
	approval, err := bridge.ExecutionApproval(ctx, in)
	if err != nil {
		return h.fromBridge(err, nil)
	}
	if approval.RunID != "" {
		// Recovery reports the committed run, including after schedule cancellation;
		// it does not make another claim or grant another execution.
		return done(map[string]any{"runId": approval.RunID})
	}
	// Scheduled work must also claim its durable dispatch on this exact device.
	if p.FenceID != ref.Context {
		var schedules []domain.Schedule
		if err := host.Do(ctx, "GET", "/projects/"+p.ProjectID+"/schedules", nil, &schedules); err != nil {
			return refuse("schedule authority unavailable")
		}
		found := false
		for _, v := range schedules {
			if v.ExecutionID == p.ExecutionID && service.ScheduleFence(v) == p.FenceID && !v.Terminal() {
				var ack domain.Schedule
				if err := host.Do(ctx, "POST", "/projects/"+p.ProjectID+"/tickets/"+p.TicketID+"/schedule/dispatch", service.ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "queued", RunID: v.RunID}, &ack); err != nil {
					return refuse("schedule not eligible")
				}
				found = true
				break
			}
		}
		if !found {
			return refuse("schedule changed or canceled")
		}
	}
	run, err := bridge.DispatchExecution(ctx, in)
	return h.fromBridge(err, map[string]any{"runId": run})
}
