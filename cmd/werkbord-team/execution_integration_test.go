package main

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/service"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type lostDispatch struct {
	*localwerkbord.Client
	lose    bool
	offline bool
}

func TestConnectorSchedulingIsExplicitlyOptedInAndRejectsMetadataGrant(t *testing.T) {
	ctx := context.Background()
	local := newIntegrationLocal(t)
	control, err := loadConnectorExecution(ctx, local.url, "")
	if err != nil || control != nil {
		t.Fatal("metadata-only configuration enabled execution", control, err)
	}
	file := filepath.Join(t.TempDir(), "grant")
	metadata, err := localwerkbord.ConnectSync(ctx, local.url, local.owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(metadata), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConnectorExecution(ctx, local.url, file); err == nil {
		t.Fatal("metadata grant accepted for scheduling")
	}
	dispatch, err := localwerkbord.ConnectDispatch(ctx, local.url, local.owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(dispatch), 0600); err != nil {
		t.Fatal(err)
	}
	control, err = loadConnectorExecution(ctx, local.url, file)
	if err != nil || control == nil {
		t.Fatal("dispatch opt-in unavailable", control, err)
	}
	if len(local.adapter.Sessions()) != 0 {
		t.Fatal("connecting granted a launch")
	}
}

func (l *lostDispatch) ExecutionApproval(ctx context.Context, in integration.ExecutionDispatch) (integration.ExecutionApproval, error) {
	if l.offline {
		return integration.ExecutionApproval{}, localwerkbord.ErrNotRunning
	}
	return l.Client.ExecutionApproval(ctx, in)
}

func (l *lostDispatch) DispatchExecution(ctx context.Context, in integration.ExecutionDispatch) (string, error) {
	run, err := l.Client.DispatchExecution(ctx, in)
	if err == nil && l.lose {
		l.lose = false
		return "", localwerkbord.ErrNotRunning
	}
	return run, err
}
func TestScheduledTeamWorkUsesLocalApprovalAndRecoversLostDispatch(t *testing.T) {
	ctx := context.Background()
	local := newIntegrationLocal(t)
	team := newIntegrationTeam(t, "scheduled team")
	journal, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	c := makeIntegrationConnector(t, local, team, journal)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	as, _ := journal.Associations(ctx, team.owner.Workspace.ID)
	if len(as) != 1 {
		t.Fatal(as)
	}
	schedule, err := team.svc.SetSchedule(ctx, team.owner, team.project.ID, team.ticket.ID, service.ScheduleInput{At: time.Now().Add(-time.Minute), Timezone: "UTC", MissedPolicy: "run_late"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := localwerkbord.ConnectExecution(ctx, local.url, local.owner)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := localwerkbord.New(local.url, token)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := execution.TaskDetail(ctx, local.project, as[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	runners := detail["runners"].([]map[string]any)
	if len(runners) != 1 {
		t.Fatal(detail)
	}
	req := integration.ExecutionRequest{ExecutionID: schedule.ExecutionID, ProjectID: local.project, TaskID: as[0].TaskID, Fence: service.ScheduleFence(schedule), RunnerID: runners[0]["id"].(string), AgentID: "fake", Model: "default", Reasoning: "default", Interaction: "interactive", NotBefore: schedule.At}
	preview, err := execution.ExecutionPreview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	dispatchToken, err := localwerkbord.ConnectDispatch(ctx, local.url, local.owner)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := localwerkbord.New(local.url, dispatchToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch.ApproveExecution(ctx, preview, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("connector could authorize itself")
	}
	link := &lostDispatch{Client: execution, lose: true}
	c.Execution = link
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	schedules, err := team.svc.Schedules(ctx, team.member, team.project.ID)
	if err != nil || schedules[0].State != "awaiting_approval" || len(local.adapter.Sessions()) != 0 {
		t.Fatal("ticket granted permission", schedules, err)
	}
	if _, err := execution.ApproveExecution(ctx, preview, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	link.offline = true
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	schedules, err = team.svc.Schedules(ctx, team.member, team.project.ID)
	if err != nil || schedules[0].State != "waiting_for_runner" || len(local.adapter.Sessions()) != 0 {
		t.Fatal("offline runner launched", schedules, err)
	}
	link.offline = false
	team.offline.Store(true)
	if err := c.Tick(ctx); err == nil {
		t.Fatal("offline workspace unexpectedly available")
	}
	if len(local.adapter.Sessions()) != 0 {
		t.Fatal("disconnected schedule launched")
	}
	team.offline.Store(false)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(local.adapter.Sessions()) != 1 {
		t.Fatal("online approved work did not launch")
	}
	// Lost Individual response: the next tick retrieves the committed run identifier.
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	schedules, err = team.svc.Schedules(ctx, team.member, team.project.ID)
	if err != nil || schedules[0].State != "executing" || schedules[0].RunID == "" || len(local.adapter.Sessions()) != 1 {
		t.Fatal("lost dispatch duplicated or lost execution", schedules, err)
	}
	if _, err := team.svc.RevokeDevice(ctx, team.member, team.device.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Tick(ctx); err == nil {
		t.Fatal("revoked member authorized")
	}
	if local.adapter.Last().StopRequested() {
		t.Fatal("revocation silently terminated local process")
	}
	if _, err := local.mgr.Stop(ctx, schedules[0].RunID); err != nil {
		t.Fatal(err)
	}
}
func TestTeamAgentApprovalCannotChangePoliciesOrTaskContext(t *testing.T) {
	ctx := context.Background()
	local := newIntegrationLocal(t)
	team := newIntegrationTeam(t, "policy team")
	journal, err := devicestate.OpenSyncJournal(ctx, filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	c := makeIntegrationConnector(t, local, team, journal)
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	as, _ := journal.Associations(ctx, team.owner.Workspace.ID)
	token, err := localwerkbord.ConnectExecution(ctx, local.url, local.owner)
	if err != nil {
		t.Fatal(err)
	}
	execution, _ := localwerkbord.New(local.url, token)
	detail, err := execution.TaskDetail(ctx, local.project, as[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	runners := detail["runners"].([]map[string]any)
	req := integration.ExecutionRequest{ExecutionID: "exe_manual", ProjectID: local.project, TaskID: as[0].TaskID, Fence: service.ScheduleContext(team.owner.Workspace.ID, team.project, team.ticket), RunnerID: runners[0]["id"].(string), AgentID: "fake", Model: "default", Reasoning: "default", Interaction: "interactive"}
	preview, err := execution.ExecutionPreview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	altered := preview
	altered.Request.Interaction = "autonomous"
	if _, err := execution.ApproveExecution(ctx, altered, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("altered effective policy approved")
	}
	if _, err := execution.ApproveExecution(ctx, preview, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	run, err := execution.DispatchExecution(ctx, integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence})
	if err != nil {
		t.Fatal(err)
	}
	again, err := execution.DispatchExecution(ctx, integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence})
	if err != nil || again != run || len(local.adapter.Sessions()) != 1 {
		t.Fatal(again, err)
	}
	if _, err := local.bridge.DispatchExecution(ctx, integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence}); err == nil {
		t.Fatal("Phase 1 metadata grant launched")
	}
	if _, err := execution.DispatchExecution(ctx, integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: "other_assignment"}); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("wrong assignment dispatched", err)
	}
}
