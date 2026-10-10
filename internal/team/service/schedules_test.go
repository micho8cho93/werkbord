package service

import (
	"context"
	"crypto/sha256"
	"devboard/internal/planning"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitsFor makes a ticket wait for others, the way the ticket form does.
func (tm *team) waitsFor(k domain.Ticket, deps ...string) domain.Ticket {
	tm.t.Helper()
	ids := append([]string{}, deps...)
	got, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{Dependencies: &ids})
	if err != nil {
		tm.t.Fatal(err)
	}
	return got
}

// scheduleOf is a ticket's request as the board shows it, with the reason it is blocked.
func (tm *team) scheduleOf(k domain.Ticket) domain.Schedule {
	tm.t.Helper()
	all, err := tm.svc.Schedules(bg, tm.owner, tm.pid())
	if err != nil {
		tm.t.Fatal(err)
	}
	for _, v := range all {
		if v.TicketID == k.ID {
			return v
		}
	}
	tm.t.Fatalf("%s has no request", k.Key)
	return domain.Schedule{}
}

func (tm *team) dispatch(a Actor, k domain.Ticket, v domain.Schedule, state string) (domain.Schedule, error) {
	return tm.svc.DispatchSchedule(AuthenticatedContext(bg, a), a, tm.pid(), k.ID, ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: state})
}

func scheduleActor(t *testing.T, tm *team, owner Actor, name string) Actor {
	t.Helper()
	device := tm.mustRegister(owner, laptop(t, name), domain.CapabilityRunner)
	token, err := tm.svc.IssueLocalDeviceCredential(bg, owner.Workspace.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := tm.svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func scheduleInput() ScheduleInput {
	return ScheduleInput{At: time.Now().Add(-time.Minute), Timezone: "Europe/Madrid", MissedPolicy: "run_late", Priority: 1}
}
func TestSchedulesAreRequestsWithGuardedOwnerDeviceClaims(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "scheduled")
	v, err := tm.svc.SetSchedule(bg, tm.owner, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	bo1 := scheduleActor(t, tm, tm.bo, "one")
	bo2 := scheduleActor(t, tm, tm.bo, "two")
	cy := scheduleActor(t, tm, tm.cy, "other member")
	input := ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "queued"}
	if _, err := tm.svc.DispatchSchedule(AuthenticatedContext(bg, cy), cy, tm.pid(), k.ID, input); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("another member could launch", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, actor := range []Actor{bo1, bo2} {
		wg.Add(1)
		go func(a Actor) {
			defer wg.Done()
			_, err := tm.svc.DispatchSchedule(AuthenticatedContext(bg, a), a, tm.pid(), k.ID, input)
			results <- err
		}(actor)
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, domain.ErrConflict) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal("claim not atomic", won)
	}
	all, err := tm.svc.Schedules(bg, tm.bo, tm.pid())
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
	v = all[0]
	loser := bo1
	if v.DeviceID == bo1.Device.ID {
		loser = bo2
	}
	input.Version = v.Version
	if _, err := tm.svc.DispatchSchedule(AuthenticatedContext(bg, loser), loser, tm.pid(), k.ID, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("failover moved authority", err)
	}
	if err := tm.svc.CancelSchedule(bg, tm.owner, tm.pid(), k.ID, v.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.DispatchSchedule(AuthenticatedContext(bg, bo1), bo1, tm.pid(), k.ID, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("canceled dispatch accepted", err)
	}
	next := scheduleInput()
	next.Version = v.Version + 1
	revised, err := tm.svc.SetSchedule(bg, tm.owner, tm.pid(), k.ID, next)
	if err != nil || revised.ExecutionID == v.ExecutionID || revised.ID == v.ID {
		t.Fatal("reschedule reused authorization", revised, err)
	}
}
func TestScheduleTimeDependenciesAssignmentAndRevocation(t *testing.T) {
	for _, kind := range []string{"early", "missed", "dependency", "order", "assignment", "revoked", "timezone", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			tm := newTeam(t)
			k := tm.claimed(tm.bo, "scheduled")
			a := scheduleActor(t, tm, tm.bo, "runner")
			in := scheduleInput()
			switch kind {
			case "early":
				in.At = time.Now().Add(time.Hour)
			case "missed":
				in.MissedPolicy = "skip"
				in.GraceSeconds = 1
			case "timezone":
				in.Timezone = "Unknown/Zone"
			case "cycle":
				// A request that names its own ticket as a dependency. The same thing on the ticket is refused too
				// (see TestTicketDependenciesCannotMakeACircle).
				in.Dependencies = []string{k.ID}
			case "dependency":
				// What a request waits for is the ticket's own list.
				dep := tm.claimed(tm.bo, "dep")
				tm.waitsFor(k, dep.ID)
			case "order":
				earlier := tm.claimed(tm.bo, "first")
				if _, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), earlier.ID, scheduleInput()); err != nil {
					t.Fatal(err)
				}
				in.Order = 1
			}
			v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, in)
			if kind == "timezone" || kind == "cycle" {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "assignment" {
				if _, err := tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "revoked" {
				if _, err := tm.svc.RevokeDevice(bg, tm.bo, a.Device.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err = tm.svc.DispatchSchedule(AuthenticatedContext(bg, a), a, tm.pid(), k.ID, ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "queued"})
			if err == nil {
				t.Fatal("ineligible schedule dispatched")
			}
		})
	}
}
func TestScheduleClaimSurvivesClusterFailoverAndRefusesNoQuorum(t *testing.T) {
	if sharedCluster == nil || len(sharedCluster.Nodes()) != 3 {
		t.Skip("requires three-node service harness")
	}
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "failover schedule")
	a := scheduleActor(t, tm, tm.bo, "runner")
	v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	ctx := AuthenticatedContext(bg, a)
	v, err = tm.svc.DispatchSchedule(ctx, a, tm.pid(), k.ID, ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	leader := sharedCluster.WaitLeader(30 * time.Second)
	leader.Kill()
	restarted := false
	defer func() {
		if !restarted {
			leader.Start()
		}
	}()
	sharedCluster.WaitLeader(30 * time.Second)
	input := ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "executing", RunID: "run_committed"}
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	v, err = tm.svc.DispatchSchedule(timeout, a, tm.pid(), k.ID, input)
	cancel()
	if err != nil || v.DeviceID != a.Device.ID || v.RunID != "run_committed" {
		t.Fatal("failover lost claim", v, err)
	}
	leader.Start()
	restarted = true
	sharedCluster.WaitMembers(30*time.Second, 3)
	sharedCluster.Partition([]int{0}, []int{1}, []int{2})
	defer sharedCluster.Heal()
	time.Sleep(2 * time.Second)
	timeout, cancel = context.WithTimeout(ctx, 3*time.Second)
	_, err = tm.svc.DispatchSchedule(timeout, a, tm.pid(), k.ID, ScheduleDispatch{ExecutionID: v.ExecutionID, Version: v.Version, State: "completed", RunID: v.RunID})
	cancel()
	if err == nil {
		t.Fatal("no quorum dispatch acknowledged")
	}
	sharedCluster.Heal()
	sharedCluster.WaitLeader(30 * time.Second)
	timeout, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	all, err := tm.svc.Schedules(timeout, a, tm.pid())
	if err != nil || len(all) != 1 || all[0].RunID != v.RunID || all[0].ExecutionID != v.ExecutionID {
		t.Fatal("reconnect lost stable execution", all, err)
	}
}

func TestCompletedScheduleUnlocksDependentOrderedWork(t *testing.T) {
	tm := newTeam(t)
	first, second := tm.claimed(tm.bo, "first"), tm.claimed(tm.bo, "second")
	a := scheduleActor(t, tm, tm.bo, "runner")
	ctx := AuthenticatedContext(bg, a)
	one, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), first.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	tm.waitsFor(second, first.ID)
	in := scheduleInput()
	in.Order = 1
	two, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), second.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.DispatchSchedule(ctx, a, tm.pid(), second.ID, ScheduleDispatch{ExecutionID: two.ExecutionID, Version: two.Version, State: "queued"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("dependency ignored", err)
	}
	one, err = tm.svc.DispatchSchedule(ctx, a, tm.pid(), first.ID, ScheduleDispatch{ExecutionID: one.ExecutionID, Version: one.Version, State: "executing", RunID: "run_first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.DispatchSchedule(ctx, a, tm.pid(), first.ID, ScheduleDispatch{ExecutionID: one.ExecutionID, Version: one.Version, State: "completed", RunID: one.RunID}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.DispatchSchedule(ctx, a, tm.pid(), second.ID, ScheduleDispatch{ExecutionID: two.ExecutionID, Version: two.Version, State: "queued"}); err != nil {
		t.Fatal("completed dependency did not unlock next request", err)
	}
}

// A ticket's dependencies are what its shared request waits for: while one is neither Done nor has a completed request,
// the request is blocked and cannot be dispatched; once it is, it can.
func TestTicketDependenciesGateASharedRequest(t *testing.T) {
	tm := newTeam(t)
	first, second := tm.claimed(tm.bo, "first"), tm.claimed(tm.bo, "second")
	second = tm.waitsFor(second, first.ID)
	a := scheduleActor(t, tm, tm.bo, "runner")
	if _, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), first.ID, scheduleInput()); err != nil {
		t.Fatal(err)
	}
	// No dependencies are named on the request: the ticket's own are used.
	two, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), second.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(two.Dependencies, []string{first.ID}) {
		t.Fatalf("the request does not record what it was proposed with: %v", two.Dependencies)
	}
	got := tm.scheduleOf(second)
	if got.State != "blocked" || got.Reason != "waiting for dependency "+first.Key {
		t.Fatalf("the request is not waiting for its dependency: %s %q", got.State, got.Reason)
	}
	if _, err := tm.dispatch(a, second, two, "queued"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dispatched while the dependency is not finished: %v", err)
	}
	// The dependency is finished by hand, not by its request.
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), first.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.CompleteTicket(bg, tm.owner, tm.pid(), first.ID); err != nil {
		t.Fatal(err)
	}
	if got := tm.scheduleOf(second); got.State == "blocked" || got.Reason != "" {
		t.Fatalf("still waiting for a Done dependency: %s %q", got.State, got.Reason)
	}
	if _, err := tm.dispatch(a, second, two, "queued"); err != nil {
		t.Fatalf("not dispatched once the dependency is Done: %v", err)
	}
}

// Dependencies that were only ever set in the ticket form, never on a request, are enforced now.
func TestDependenciesSetOnlyOnTheTicketAreEnforcedBySharedRequests(t *testing.T) {
	tm := newTeam(t)
	first := tm.claimed(tm.bo, "first")
	k, err := tm.svc.CreateTicket(bg, tm.owner, tm.pid(), TicketInput{Title: "second", Status: domain.TicketAvailable, Dependencies: []string{first.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if k, err = tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	a := scheduleActor(t, tm, tm.bo, "runner")
	v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	if got := tm.scheduleOf(k); got.State != "blocked" || !strings.Contains(got.Reason, "waiting for dependency "+first.Key) {
		t.Fatalf("the form's dependency was not enforced: %s %q", got.State, got.Reason)
	}
	if _, err := tm.dispatch(a, k, v, "queued"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dispatched past a dependency set on the ticket: %v", err)
	}
}

// Changing what a ticket waits for after a request was made makes the request stale: it no longer matches what was
// proposed, says so, and cannot be dispatched until it is proposed again.
func TestEditingDependenciesMakesALiveRequestStaleUntilItIsProposedAgain(t *testing.T) {
	tm := newTeam(t)
	first, other, k := tm.claimed(tm.bo, "first"), tm.claimed(tm.bo, "other"), tm.claimed(tm.bo, "k")
	a := scheduleActor(t, tm, tm.bo, "runner")
	k = tm.waitsFor(k, first.ID, other.ID)
	v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	fence := ScheduleFence(v)

	// The same dependencies in another order are the same request.
	tm.waitsFor(k, other.ID, first.ID)
	if got := tm.scheduleOf(k); strings.Contains(got.Reason, "propose it again") || strings.Contains(got.Reason, "changed") {
		t.Fatalf("reordering made the request stale: %q", got.Reason)
	}

	for name, deps := range map[string][]string{"one removed": {first.ID}, "all removed": {}, "one added": {first.ID, other.ID, tm.claimed(tm.bo, "third").ID}} {
		t.Run(name, func(t *testing.T) {
			tm.waitsFor(k, deps...)
			got := tm.scheduleOf(k)
			if got.State != "blocked" || !strings.Contains(got.Reason, "dependencies changed") || !strings.Contains(got.Reason, "propose it again") {
				t.Fatalf("not stale after the edit: %s %q", got.State, got.Reason)
			}
			if _, err := tm.dispatch(a, k, v, "queued"); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("a stale request was dispatched: %v", err)
			}
			tm.waitsFor(k, first.ID, other.ID) // back to what was proposed
			if got := tm.scheduleOf(k); !strings.Contains(got.Reason, "waiting for dependency") {
				t.Fatalf("restoring the dependencies did not restore the request: %q", got.Reason)
			}
		})
	}

	tm.waitsFor(k, first.ID)
	again, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, ScheduleInput{Version: v.Version, At: v.At, Timezone: v.Timezone, MissedPolicy: v.MissedPolicy, Priority: v.Priority})
	if err != nil {
		t.Fatalf("proposing again: %v", err)
	}
	if again.ExecutionID == v.ExecutionID || ScheduleFence(again) == fence || !sameIDs(again.Dependencies, []string{first.ID}) {
		t.Fatalf("the new request reuses the old authorization: %+v", again)
	}
	if got := tm.scheduleOf(k); got.Reason != "waiting for dependency "+first.Key {
		t.Fatalf("the new request is not waiting for the new list: %q", got.Reason)
	}
}

// A dependency that was archived without being finished may be set (the timeline warns about it) and makes the
// request wait, saying why and what to do, rather than waiting for ever with a vague reason.
func TestAnArchivedDependencyMakesTheRequestWaitAndSaysSo(t *testing.T) {
	tm := newTeam(t)
	dep, k := tm.claimed(tm.bo, "dep"), tm.claimed(tm.bo, "k")
	a := scheduleActor(t, tm, tm.bo, "runner")
	tm.waitsFor(k, dep.ID)
	v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.ArchiveTicket(bg, tm.bo, tm.pid(), dep.ID, dep.Version, true); err != nil {
		t.Fatal(err)
	}
	got := tm.scheduleOf(k)
	if got.State != "blocked" || !strings.Contains(got.Reason, dep.Key) || !strings.Contains(got.Reason, "archived") || !strings.Contains(got.Reason, "remove it from this ticket's dependencies") {
		t.Fatalf("reason = %q", got.Reason)
	}
	_, err = tm.dispatch(a, k, v, "queued")
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("dispatch = %v", err)
	}
	// Depending on archived work is still allowed to be set on a ticket.
	tm.waitsFor(tm.claimed(tm.bo, "later"), dep.ID)

	// The way out is on the ticket, and the request is then proposed again.
	tm.waitsFor(k)
	v2, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, ScheduleInput{Version: v.Version, At: v.At, Timezone: v.Timezone, MissedPolicy: v.MissedPolicy, Priority: v.Priority})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.dispatch(a, k, v2, "queued"); err != nil {
		t.Fatalf("still blocked after removing the archived dependency: %v", err)
	}
}

// A client of the old interface may still send dependencies with a request: only the ticket's own list is accepted.
func TestAScheduleRequestMayRepeatTheTicketsDependenciesAndNothingElse(t *testing.T) {
	tm := newTeam(t)
	first, other, k := tm.claimed(tm.bo, "first"), tm.claimed(tm.bo, "other"), tm.claimed(tm.bo, "k")
	tm.waitsFor(k, first.ID, other.ID)
	version := int64(0)
	try := func(deps []string) error {
		in := scheduleInput()
		in.Version, in.Dependencies = version, deps
		v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, in)
		if err == nil {
			version = v.Version
		}
		return err
	}
	for name, deps := range map[string][]string{"none stated": nil, "the same list": {first.ID, other.ID}, "the same set in another order": {other.ID, first.ID}} {
		if err := try(deps); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, deps := range map[string][]string{"a shorter list": {first.ID}, "a longer list": {first.ID, other.ID, k.ID}, "an empty list": {}, "another ticket": {first.ID, "tkt_other"}} {
		err := try(deps)
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "ticket's dependencies") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A ticket that waits for nothing accepts the empty list an old client sends.
	free := tm.claimed(tm.bo, "free")
	in := scheduleInput()
	in.Dependencies = []string{}
	if _, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), free.ID, in); err != nil {
		t.Errorf("an empty list for a ticket that waits for nothing: %v", err)
	}
}

// The ticket form refuses a circle, and a circle that arrived another way (a migration copies dependencies and cannot
// see one) is reported by the timeline, cannot be proposed, and makes no request loop.
func TestTicketDependenciesCannotMakeACircle(t *testing.T) {
	tm := newTeam(t)
	a, b := tm.claimed(tm.bo, "a"), tm.claimed(tm.bo, "b")
	tm.waitsFor(b, a.ID)
	ids := []string{b.ID}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), a.ID, TicketPatch{Dependencies: &ids}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a circle on the ticket form: %v", err)
	}
	self := []string{a.ID}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), a.ID, TicketPatch{Dependencies: &self}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a self-dependency on the ticket form: %v", err)
	}

	// A request exists for b (which waits for a). Then data arrives that makes a wait for b.
	vb, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), b.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.db.Update(bg, func(tx store.Tx) error {
		k, err := tx.Ticket(bg, tm.owner.Workspace.ID, tm.pid(), a.ID)
		if err != nil {
			return err
		}
		return tx.SetTicketDependencies(bg, tm.owner.Workspace.ID, k.ID, []string{b.ID})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), a.ID, scheduleInput()); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("proposed a request that could never start: %v", err)
	}
	tl, err := tm.svc.Timeline(bg, tm.owner, tm.pid())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range tl.Warnings {
		found = found || w.Code == planning.CodeDependencyCycle
	}
	if !found {
		t.Fatalf("the circle is not reported: %+v", tl.Warnings)
	}
	// b's request still answers (it waits), and reading it neither loops nor fails.
	if got := tm.scheduleOf(b); got.State != "blocked" || got.Reason != "waiting for dependency "+a.Key {
		t.Fatalf("b = %s %q", got.State, got.Reason)
	}
	if _, err := tm.dispatch(scheduleActor(t, tm, tm.bo, "runner"), b, vb, "queued"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dispatched inside a circle: %v", err)
	}
}

// Requests proposed before dependencies were part of the fence keep working when the ticket waits for exactly what the
// request recorded, and are stale when it does not. A ticket that waits for nothing has the fence it always had.
func TestRequestsProposedBeforeDependenciesWereFencedStayValid(t *testing.T) {
	tm := newTeam(t)
	ws := tm.owner.Workspace.ID
	old := func(k domain.Ticket) string { // the fence as it was computed before
		raw, _ := json.Marshal(struct {
			Workspace, Project, Ticket, Member, Repository, Title, Description, Requirements string
			Assignment                                                                       int64
		}{ws, tm.project.ID, k.ID, k.AssigneeID, tm.project.Repository, k.Title, k.Description, k.Requirements, k.Assignment})
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	free := tm.claimed(tm.bo, "free")
	if ScheduleContext(ws, tm.project, free) != old(free) {
		t.Fatal("the fence of a ticket that waits for nothing changed")
	}
	first, other, k := tm.claimed(tm.bo, "first"), tm.claimed(tm.bo, "other"), tm.claimed(tm.bo, "k")
	k = tm.waitsFor(k, first.ID, other.ID)
	if ScheduleContext(ws, tm.project, k) == old(k) || ScheduleContext(ws, tm.project, k) != ScheduleContext(ws, tm.project, func() domain.Ticket { c := k; c.Dependencies = []string{other.ID, first.ID}; return c }()) {
		t.Fatal("dependencies are not part of the fence, or their order is")
	}

	// Make the request look as it did before: the fence without dependencies, the recorded list in its document.
	v, err := tm.svc.SetSchedule(bg, tm.bo, tm.pid(), k.ID, scheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	rewrite := func(fence string, deps []string) {
		t.Helper()
		if err := tm.db.Update(bg, func(tx store.Tx) error {
			cur, err := tx.Schedule(bg, ws, tm.pid(), k.ID)
			if err != nil {
				return err
			}
			prev := cur.Version
			cur.Fence, cur.Dependencies = fence, deps
			return tx.SaveSchedule(bg, ws, cur, prev)
		}); err != nil {
			t.Fatal(err)
		}
	}
	rewrite(old(k), v.Dependencies)
	if got := tm.scheduleOf(k); got.Reason != "waiting for dependency "+first.Key {
		t.Fatalf("an unchanged request from before is stale: %q", got.Reason)
	}
	// It has not been given more to wait for than it recorded.
	rewrite(old(k), []string{first.ID})
	if got := tm.scheduleOf(k); !strings.Contains(got.Reason, "dependencies changed") {
		t.Fatalf("a request that now waits for more than it recorded is not stale: %q", got.Reason)
	}
	// And a change to anything else of the ticket still makes it stale.
	rewrite(old(k), v.Dependencies)
	title := "renamed"
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if got := tm.scheduleOf(k); got.Reason != "ticket context or assignment changed" {
		t.Fatalf("an edited ticket is not stale: %q", got.Reason)
	}
}
