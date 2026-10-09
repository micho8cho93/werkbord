package service

import (
	"context"
	"devboard/internal/team/domain"
	"errors"
	"sync"
	"testing"
	"time"
)

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
				in.Dependencies = []string{k.ID}
			case "dependency":
				dep := tm.claimed(tm.bo, "dep")
				in.Dependencies = []string{dep.ID}
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
	in := scheduleInput()
	in.Order, in.Dependencies = 1, []string{first.ID}
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
