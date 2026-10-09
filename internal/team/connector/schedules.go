package connector

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/service"
	"errors"
	"strings"
)

type ExecutionLocal interface {
	ExecutionApproval(context.Context, integration.ExecutionDispatch) (integration.ExecutionApproval, error)
	DispatchExecution(context.Context, integration.ExecutionDispatch) (string, error)
	Snapshot(context.Context, string, string) (integration.Snapshot, error)
}

// CoordinateSchedule is a request/acknowledgment loop. Only Individual dispatches
// approved work, with its normal scheduler/runtime gates. Every retry first
// refreshes workspace authority. There is no launch during a disconnected tick.
func CoordinateSchedule(ctx context.Context, host Workspace, local ExecutionLocal, device string, v domain.Schedule, accept func(integration.ExecutionRequest) bool) error {
	if v.Terminal() || v.State == "blocked" && v.RunID == "" || v.DeviceID != "" && v.DeviceID != device {
		return nil
	}
	in := integration.ExecutionDispatch{ExecutionID: v.ExecutionID, Fence: service.ScheduleFence(v)}
	a, err := local.ExecutionApproval(ctx, in)
	state := "awaiting_approval"
	if errors.Is(err, localwerkbord.ErrNotRunning) {
		state = "waiting_for_runner"
	}
	if err == nil && (a.Preview.Request.Fence != in.Fence || a.Preview.Request.ExecutionID != in.ExecutionID || !accept(a.Preview.Request)) {
		return errors.New("local schedule approval association changed")
	}
	path := "/projects/" + v.ProjectID + "/tickets/" + v.TicketID + "/schedule/dispatch"
	update := func(state, run string) error {
		var ack domain.Schedule
		err := host.Do(ctx, "POST", path, service.ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: state, RunID: run}, &ack)
		if err == nil {
			v = ack
		}
		return err
	}
	if err != nil {
		if !errors.Is(err, localwerkbord.ErrNotRunning) && !hostclient.IsStatus(err, 404) && !hostclient.IsStatus(err, 403) {
			var status *localwerkbord.StatusError
			if !errors.As(err, &status) || status.Code != 404 && status.Code != 403 {
				return err
			}
		}
		if v.RunID != "" {
			return nil
		}
		return update(state, "")
	}
	if a.RunID == "" {
		// Quorum-protected dispatch binds this execution to one enrolled owner device.
		if err := update("queued", ""); err != nil {
			return err
		}
		run, err := local.DispatchExecution(ctx, in)
		if err != nil {
			if errors.Is(err, localwerkbord.ErrNotRunning) {
				return update("waiting_for_runner", "")
			}
			var status *localwerkbord.StatusError
			if errors.As(err, &status) && status.Code == 409 {
				if strings.Contains(status.Message, "capacity") || strings.Contains(status.Message, "offline") || strings.Contains(status.Message, "unavailable") {
					return update("waiting_for_runner", "")
				}
				return update("blocked", "")
			}
			// Approval/run claim may have committed despite a lost answer. Preserve queued;
			// the next tick recovers the durable Individual run ID before doing anything.
			return err
		}
		a.RunID = run
	}
	snap, err := local.Snapshot(ctx, a.Preview.Request.ProjectID, a.Preview.Request.TaskID)
	if err != nil {
		return err
	}
	state = "executing"
	if snap.Execution.RunID != a.RunID {
		return errors.New("schedule execution no longer matches latest local run")
	}
	switch snap.Execution.State {
	case "completed":
		state = "completed"
	case "blocked", "failed", "canceled":
		state = "blocked"
	case "queued":
		state = "queued"
	}
	return update(state, a.RunID)
}
