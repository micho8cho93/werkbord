package service

import (
	"context"
	"testing"
	"time"

	"devboard/internal/integration"
	"devboard/internal/planning"
	"devboard/internal/team/domain"
)

// runner is a member's own Werkbord, as Team sees it: an enrolled device that reports on a ticket it holds.
type runner struct {
	tm    *team
	ctx   context.Context
	actor Actor
	k     domain.Ticket
	seq   int64
	clock *time.Time
}

func (tm *team) runnerFor(a Actor, k domain.Ticket, name string) *runner {
	tm.t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	tm.svc.SetClock(func() time.Time { return now })
	device := tm.mustRegister(a, laptop(tm.t, name), domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, a.Workspace.ID, device.ID)
	if err != nil {
		tm.t.Fatal(err)
	}
	actor, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		tm.t.Fatal(err)
	}
	return &runner{tm: tm, ctx: AuthenticatedContext(context.Background(), actor), actor: actor, k: k, clock: &now}
}

// report sends one progress report about run n of the ticket, in the given execution state.
func (r *runner) report(run int, state string) error {
	r.tm.t.Helper()
	r.seq++
	started := r.k.ClaimedAt.Add(time.Second)
	e := integration.Execution{RunID: "run_" + string(rune('a'+run)), State: state, StartedAt: &started, RunnerOnline: true}
	if state == "completed" || state == "failed" || state == "canceled" {
		done := started.Add(time.Minute)
		e.CompletedAt, e.Outcome = &done, state
	}
	_, err := r.tm.svc.ReportProgress(r.ctx, r.actor, r.tm.pid(), r.k.ID, domain.Progress{Schema: integration.Schema, TaskID: "tsk_local", ProjectID: "prj_local",
		Assignment: r.k.Assignment, ClaimAt: *r.k.ClaimedAt, Sequence: r.seq, Execution: e})
	return err
}

func (tm *team) status(k domain.Ticket) domain.TicketStatus {
	tm.t.Helper()
	got, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil {
		tm.t.Fatal(err)
	}
	return got.Status
}

func TestAnAgentThatFinishesMovesItsTicketToReview(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Fix login")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")

	// Running, then needing an answer, then failing: the ticket stays where it is, waiting for its owner.
	for _, state := range []string{"running", "needs_input", "blocked", "failed"} {
		if err := r.report(0, state); err != nil {
			t.Fatal(state, err)
		}
		if got := tm.status(k); got != domain.TicketInProgress {
			t.Fatalf("a run that is %s moved the ticket to %s", state, got)
		}
	}
	// A run that completes: the work goes to Review, like a finished run does in the person's own Werkbord.
	if err := r.report(1, "completed"); err != nil {
		t.Fatal(err)
	}
	got, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if got.Status != domain.TicketReview || got.SubmittedAt == nil || got.AssigneeID != tm.bo.Member.ID {
		t.Fatalf("a finished run left the ticket %+v", got)
	}
	kinds := tm.activityKinds()
	if n := len(kinds); n < 2 || kinds[n-2] != domain.ActWorkSubmitted || kinds[n-1] != domain.ActReviewRequested {
		t.Fatalf("the history does not say the work was submitted: %v", kinds)
	}

	// A reviewer sends it back. The same finished run being reported again must not undo that.
	if _, err := tm.svc.RequestChanges(bg, tm.di, tm.pid(), k.ID, "add tests"); err != nil {
		t.Fatal(err)
	}
	if err := r.report(1, "completed"); err != nil {
		t.Fatal(err)
	}
	if got := tm.status(k); got != domain.TicketInProgress {
		t.Fatalf("a report of the run that was already reviewed put the ticket back in %s", got)
	}
	// Its next run finishing does.
	if err := r.report(2, "running"); err != nil {
		t.Fatal(err)
	}
	if err := r.report(2, "completed"); err != nil {
		t.Fatal(err)
	}
	if got := tm.status(k); got != domain.TicketReview {
		t.Fatalf("the next finished run left the ticket in %s", got)
	}
}

func TestOnlyAgentWorkMovesToReviewByItself(t *testing.T) {
	for _, mode := range []planning.ExecutionMode{planning.ModeHuman, planning.ModeHybrid} {
		tm := newTeam(t)
		k := tm.planned(tm.owner, "Mode "+string(mode), TicketInput{Status: domain.TicketAvailable, WorkMode: mode})
		k, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID)
		if err != nil {
			t.Fatal(err)
		}
		r := tm.runnerFor(tm.bo, k, "laptop "+string(mode))
		if err := r.report(0, "completed"); err != nil {
			t.Fatal(err)
		}
		if got := tm.status(k); got != domain.TicketInProgress {
			t.Fatalf("%s work went to %s when an agent finished: a person finishes it", mode, got)
		}
	}
}

func TestNobodyButTheOwnerSeesWhatTheirAgentIsDoing(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Private work")
	r := tm.runnerFor(tm.bo, k, "bo's laptop")
	if err := r.report(0, "running"); err != nil {
		t.Fatal(err)
	}

	own, err := tm.svc.TicketProgress(bg, tm.bo, tm.pid(), k.ID)
	if err != nil || len(own) != 1 || own[0].Execution.State != "running" {
		t.Fatalf("the owner does not see their own run: %+v %v", own, err)
	}
	for name, who := range map[string]Actor{"a teammate": tm.cy, "a reviewer": tm.di, "the workspace owner": tm.owner} {
		seen, err := tm.svc.TicketProgress(bg, who, tm.pid(), k.ID)
		if err != nil || len(seen) != 0 {
			t.Fatalf("%s sees %+v of someone else's agent (%v)", name, seen, err)
		}
	}

	// What they all may see: that the owner's agent is active, and nothing else.
	for name, who := range map[string]Actor{"owner": tm.bo, "a teammate": tm.cy, "the workspace owner": tm.owner} {
		b, err := tm.svc.Board(bg, who, tm.pid())
		if err != nil {
			t.Fatal(err)
		}
		if !b.Active[k.ID] || len(b.Active) != 1 {
			t.Fatalf("%s sees active = %v", name, b.Active)
		}
	}

	// Waiting for an answer is still active. A run that failed, or one the owner has stopped reporting on, is not.
	if err := r.report(0, "needs_input"); err != nil {
		t.Fatal(err)
	}
	if b, _ := tm.svc.Board(bg, tm.cy, tm.pid()); !b.Active[k.ID] {
		t.Fatal("an agent waiting for its owner's answer is not active")
	}
	if err := r.report(0, "failed"); err != nil {
		t.Fatal(err)
	}
	if b, _ := tm.svc.Board(bg, tm.cy, tm.pid()); b.Active[k.ID] {
		t.Fatal("a failed run is active")
	}
	if err := r.report(1, "running"); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(domain.DeviceOnlineWindow + time.Minute)
	if b, _ := tm.svc.Board(bg, tm.cy, tm.pid()); b.Active[k.ID] {
		t.Fatal("a run nobody has reported on for minutes is active")
	}

	// A claimed ticket whose agent was never started is simply not active.
	w := tm.working(tm.cy, "Not started")
	if b, _ := tm.svc.Board(bg, tm.bo, tm.pid()); b.Active[w.ID] {
		t.Fatal("a ticket nobody ran an agent on is active")
	}
}

// Importing the claimed ticket creates a local task in queued state, without a run.
// That task must still offer Start agent and must not look active to teammates.
func TestAQueuedTaskWithoutARunIsNotAnActiveAgent(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Claimed but not started")
	r := tm.runnerFor(tm.bo, k, "Bo's laptop")
	_, err := tm.svc.ReportProgress(r.ctx, r.actor, tm.pid(), k.ID, domain.Progress{
		Schema: integration.Schema, TaskID: "tsk_local", ProjectID: "prj_local",
		Assignment: k.Assignment, ClaimAt: *k.ClaimedAt, Sequence: 1,
		Execution: integration.Execution{State: "queued", RunnerOnline: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := tm.svc.Board(bg, tm.cy, tm.pid())
	if err != nil || b.Active[k.ID] {
		t.Fatalf("unstarted task is active: %v %v", b.Active, err)
	}
}
