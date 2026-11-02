package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
)

func TestProgressOrdersReportsAndCannotCompleteOrResurrectOwnership(t *testing.T) {
	tm := newTeam(t)
	clock := time.Now().UTC().Truncate(time.Millisecond)
	tm.svc.SetClock(func() time.Time { return clock })
	k := tm.working(tm.bo, "sync")
	identity := laptop(t, "connector")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := AuthenticatedContext(context.Background(), actor)
	in := domain.Progress{Schema: integration.Schema, TaskID: "tsk_local", ProjectID: "prj_local", Assignment: k.Assignment, ClaimAt: *k.ClaimedAt, Sequence: 2, Execution: integration.Execution{State: "completed", Outcome: "completed"}}
	ack, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in)
	if err != nil || !ack.Applied || ack.Sequence != 2 {
		t.Fatalf("ack %+v %v", ack, err)
	}
	ack, err = tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in)
	if err != nil || ack.Applied {
		t.Fatalf("duplicate %+v %v", ack, err)
	}
	in.Sequence = 1
	in.Execution.State = "running"
	in.Execution.Outcome = ""
	ack, err = tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in)
	if err != nil || ack.Applied || ack.Sequence != 2 {
		t.Fatalf("old %+v %v", ack, err)
	}
	in.Sequence = 2
	if _, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed duplicate: %v", err)
	}
	current, _ := tm.svc.GetTicket(bg, tm.bo, tm.pid(), k.ID)
	if current.Status != domain.TicketInProgress {
		t.Fatal("execution completed the Team ticket")
	}
	if _, err := tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Assignment <= k.Assignment || !reclaimed.ClaimedAt.Equal(*k.ClaimedAt) {
		t.Fatal("test must reclaim within the same timestamp with a new assignment")
	}
	in.Sequence = 3
	if _, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("past assignment accepted: %v", err)
	}
	in.Assignment = reclaimed.Assignment
	if _, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.RevokeDevice(bg, tm.bo, device.ID); err != nil {
		t.Fatal(err)
	}
	in.Sequence = 4
	if _, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("revoked device accepted: %v", err)
	}
	rs, err := tm.svc.TicketProgress(bg, tm.owner, tm.pid(), k.ID)
	if err != nil || len(rs) != 0 {
		t.Fatalf("revoked progress visible: %v %v", rs, err)
	}
}

func TestProgressRequiresQuorumAndRetriesAfterRecovery(t *testing.T) {
	if sharedCluster == nil || len(sharedCluster.Nodes()) != 3 {
		t.Skip("requires the existing three-node replicated service harness")
	}
	tm := newTeam(t)
	k := tm.working(tm.bo, "quorum progress")
	identity := laptop(t, "quorum connector")
	device := tm.mustRegister(tm.bo, identity, domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	in := domain.Progress{Schema: integration.Schema, TaskID: "tsk_quorum", ProjectID: "prj_local", Assignment: k.Assignment, ClaimAt: *k.ClaimedAt, Sequence: 1, Execution: integration.Execution{State: "running"}}
	ctx := AuthenticatedContext(context.Background(), actor)
	if _, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, in); err != nil {
		t.Fatal(err)
	}
	sharedCluster.Partition([]int{0}, []int{1}, []int{2})
	defer sharedCluster.Heal()
	time.Sleep(2 * time.Second)
	in.Sequence = 2
	in.Execution = integration.Execution{State: "completed", Outcome: "completed"}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	ack, err := tm.svc.ReportProgress(timeout, actor, tm.pid(), k.ID, in)
	cancel()
	if err == nil || ack.Applied {
		t.Fatalf("no-quorum update acknowledged: %+v %v", ack, err)
	}
	sharedCluster.Heal()
	sharedCluster.WaitLeader(30 * time.Second)
	timeout, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ack, err = tm.svc.ReportProgress(timeout, actor, tm.pid(), k.ID, in)
	if err != nil || ack.Sequence != 2 {
		t.Fatalf("quorum recovery: %+v %v", ack, err)
	}
	records, err := tm.svc.TicketProgress(timeout, tm.owner, tm.pid(), k.ID)
	if err != nil || len(records) != 1 || records[0].Execution.State != "completed" {
		t.Fatal(records, err)
	}
}

// A host that has this change accepts a report with or without the agent and "starting", and refuses a malformed agent id.
func TestProgressAcceptsAnAgentAndStartingAndRefusesAMalformedAgent(t *testing.T) {
	tm := newTeam(t)
	clock := time.Now().UTC().Truncate(time.Millisecond)
	tm.svc.SetClock(func() time.Time { return clock })
	k := tm.working(tm.bo, "agent")
	device := tm.mustRegister(tm.bo, laptop(t, "connector"), domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, tm.bo.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := AuthenticatedContext(context.Background(), actor)
	report := func(seq int64, e integration.Execution) error {
		_, err := tm.svc.ReportProgress(ctx, actor, tm.pid(), k.ID, domain.Progress{Schema: integration.Schema, TaskID: "tsk_local", ProjectID: "prj_local", Assignment: k.Assignment, ClaimAt: *k.ClaimedAt, Sequence: seq, Execution: e})
		return err
	}
	if err := report(1, integration.Execution{State: "running"}); err != nil {
		t.Fatalf("a report without an agent (an older connector): %v", err)
	}
	if err := report(2, integration.Execution{RunID: "run_1", State: "starting", AgentID: "codex"}); err != nil {
		t.Fatalf("starting with an agent: %v", err)
	}
	for i, bad := range []string{"Codex", "co dex", "../x", "a--b"} {
		if err := report(int64(10+i), integration.Execution{State: "running", AgentID: bad}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

// The host reads the API with DisallowUnknownFields. This is what lets a newer connector send agentId later without a 400.
func TestProgressDecodesWithAndWithoutTheAgentOnAStrictDecoder(t *testing.T) {
	for _, body := range []string{
		`{"assignment":1,"schema":"` + integration.Schema + `","taskId":"t","localProjectId":"p","claimAt":"2026-11-02T10:00:00Z","sequence":1,"execution":{"state":"running","runnerOnline":true,"runnerAvailable":true,"handoffAvailable":false},"gitUnavailable":true}`,
		`{"assignment":1,"schema":"` + integration.Schema + `","taskId":"t","localProjectId":"p","claimAt":"2026-11-02T10:00:00Z","sequence":1,"execution":{"state":"starting","agentId":"codex","runnerOnline":true,"runnerAvailable":true,"handoffAvailable":false},"gitUnavailable":true}`,
	} {
		dec := json.NewDecoder(strings.NewReader(body))
		dec.DisallowUnknownFields()
		var p domain.Progress
		if err := dec.Decode(&p); err != nil || !p.Execution.Valid() {
			t.Fatalf("%v %s", err, body)
		}
	}
}
