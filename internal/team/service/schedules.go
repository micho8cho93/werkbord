package service

import (
	"context"
	"crypto/sha256"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"devboard/internal/planning"
)

type ScheduleInput struct {
	Version      int64     `json:"version"`
	At           time.Time `json:"at"`
	Timezone     string    `json:"timezone"`
	MissedPolicy string    `json:"missedPolicy"`
	GraceSeconds int       `json:"graceSeconds"`
	Order        int       `json:"order"`
	Priority     int       `json:"priority"`
	// Dependencies is accepted from older clients and nothing else: what a request waits for is the ticket's own
	// dependency list, edited on the ticket. A list that is not exactly the ticket's current one is refused.
	Dependencies []string `json:"dependencies"`
}

// ScheduleContext is what a shared request is bound to: the ticket as it was proposed, including the tickets it
// waits for. A ticket whose dependencies change after a request was made therefore no longer matches the request's
// fence, and the request is stale until it is proposed again, exactly as when the title or the assignee changes.
// The dependencies are sorted (their order means nothing) and left out when there are none, which keeps the fence
// of a ticket that waits for nothing what it was before dependencies were part of it.
func ScheduleContext(w string, p domain.Project, k domain.Ticket) string {
	return scheduleContext(w, p, k, sortedIDs(k.Dependencies))
}

func scheduleContext(w string, p domain.Project, k domain.Ticket, deps []string) string {
	raw, _ := json.Marshal(struct {
		Workspace, Project, Ticket, Member, Repository, Title, Description, Requirements string
		Assignment                                                                       int64
		Dependencies                                                                     []string `json:",omitempty"`
	}{w, p.ID, k.ID, k.AssigneeID, p.Repository, k.Title, k.Description, k.Requirements, k.Assignment, deps})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// contextHolds reports whether ticket k is still what request v was proposed for. A request proposed before
// dependencies were part of the context carries a fence without them; it still holds when the ticket waits for
// exactly the tickets the request recorded and nothing else changed, so an upgrade does not make every request that
// had dependencies stale (and does not stop a run that is already executing from reporting it finished).
func contextHolds(w string, p domain.Project, k domain.Ticket, v domain.Schedule) bool {
	if ScheduleContext(w, p, k) == v.Fence {
		return true
	}
	return sameIDs(v.Dependencies, k.Dependencies) && scheduleContext(w, p, k, nil) == v.Fence
}

func sortedIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func sameIDs(a, b []string) bool {
	a, b = sortedIDs(a), sortedIDs(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func ScheduleFence(s domain.Schedule) string {
	raw, _ := json.Marshal(struct {
		ID, Execution, Fence, Timezone, Missed string
		At                                     time.Time
		Grace, Order, Priority                 int
		Dependencies                           []string
	}{s.ID, s.ExecutionID, s.Fence, s.Timezone, s.MissedPolicy, s.At, s.GraceSeconds, s.Order, s.Priority, s.Dependencies})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) SetSchedule(ctx context.Context, a Actor, pid, tid string, in ScheduleInput) (domain.Schedule, error) {
	var out domain.Schedule
	err := s.mutate(ctx, a, pid, func(tx store.Tx, x access) error {
		k, err := s.loadTicket(ctx, tx, a, x, tid)
		if err != nil {
			return err
		}
		if in.At.IsZero() || in.At.Year() < 2020 || in.At.Year() > 9999 || in.Timezone == "" || in.GraceSeconds < 0 || in.GraceSeconds > 86400 || in.Priority < 0 || in.Priority > 2 || in.Order < 0 {
			return domain.ErrInvalid
		}
		if _, err := time.LoadLocation(in.Timezone); err != nil {
			return domain.ErrInvalid
		}
		if in.MissedPolicy != "run_late" && in.MissedPolicy != "skip" {
			return domain.ErrInvalid
		}

		if why := k.AgentRefusal(); why != "" {
			return fmt.Errorf("%w: %s", domain.ErrConflict, why)
		}
		if !x.can(domain.PPTicketsAssign) && k.AssigneeID != a.Member.ID {
			return forbidden("propose another member's schedule")
		}
		if !k.Status.Held() || k.AssigneeID == "" {
			return fmt.Errorf("%w: assign or claim the ticket before scheduling", domain.ErrConflict)
		}
		old, err := tx.Schedule(ctx, a.Workspace.ID, pid, tid)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if old.Version != in.Version {
			return domain.ErrConflict
		}
		if (old.DeviceID != "" || old.RunID != "") && !old.Terminal() {
			return fmt.Errorf("%w: cancel this shared request before rescheduling; stop an accepted run locally if needed", domain.ErrConflict)
		}
		// What the request waits for is the ticket's own dependency list. An older client may still send one; it is
		// only accepted when it says what the ticket already says.
		if in.Dependencies != nil && !sameIDs(in.Dependencies, k.Dependencies) {
			return fmt.Errorf("%w: set what this request waits for in the ticket's dependencies, not on its schedule", domain.ErrInvalid)
		}
		// Dependencies are checked for circles where they are written, so a circle can only come from data that
		// arrived another way (a migration copied them). Such a ticket cannot be proposed: it could never start.
		if len(k.Dependencies) > 0 {
			all, err := tx.Tickets(ctx, a.Workspace.ID, pid)
			if err != nil {
				return err
			}
			if planning.HasCycle(reachableDependencies(all, tid)) {
				return fmt.Errorf("%w: dependency cycle; fix the ticket's dependencies first", domain.ErrInvalid)
			}
		}
		out = domain.Schedule{ID: domain.NewID("sch"), ExecutionID: domain.NewID("exe"), ProjectID: pid, TicketID: tid, MemberID: k.AssigneeID, Assignment: k.Assignment, Fence: ScheduleContext(a.Workspace.ID, x.Project, k), Version: old.Version + 1, At: in.At.UTC(), Timezone: in.Timezone, MissedPolicy: in.MissedPolicy, GraceSeconds: in.GraceSeconds, Order: in.Order, Priority: in.Priority, Dependencies: append([]string{}, k.Dependencies...), State: "waiting_for_runner", UpdatedAt: s.stamp()}
		return tx.SaveSchedule(ctx, a.Workspace.ID, out, old.Version)
	})
	return out, err
}

// reachableDependencies is the dependency graph of the tickets that tid waits for, directly or not.
func reachableDependencies(all []domain.Ticket, tid string) map[string][]string {
	by := make(map[string][]string, len(all))
	for _, t := range all {
		by[t.ID] = t.Dependencies
	}
	graph := map[string][]string{}
	for todo := []string{tid}; len(todo) > 0; {
		id := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if _, seen := graph[id]; seen {
			continue
		}
		graph[id] = by[id]
		todo = append(todo, by[id]...)
	}
	return graph
}

func (s *Service) scheduleEligibility(ctx context.Context, tx store.Tx, a Actor, x access, v domain.Schedule) (string, error) {
	k, err := tx.Ticket(ctx, a.Workspace.ID, v.ProjectID, v.TicketID)
	if err != nil {
		return "", err
	}
	if x.Project.Archived || k.ArchivedAt != nil || !k.Status.Held() || k.AssigneeID != v.MemberID || k.Assignment != v.Assignment {
		return "ticket context or assignment changed", nil
	}
	if !contextHolds(a.Workspace.ID, x.Project, k, v) {
		if !sameIDs(v.Dependencies, k.Dependencies) {
			return "the ticket's dependencies changed after this request was proposed; propose it again", nil
		}
		return "ticket context or assignment changed", nil
	}
	on, err := tx.IsProjectMember(ctx, a.Workspace.ID, v.ProjectID, v.MemberID)
	if err != nil {
		return "", err
	}
	if !on {
		return "membership revoked", nil
	}
	if _, err := tx.Member(ctx, a.Workspace.ID, v.MemberID); err != nil {
		return "membership revoked", nil
	}
	if v.RunID != "" {
		return "", nil
	}
	if s.stamp().Before(v.At) {
		return "scheduled for later", nil
	}
	if v.MissedPolicy == "skip" && s.stamp().After(v.At.Add(time.Duration(v.GraceSeconds)*time.Second)) {
		return "missed allowed start window", nil
	}
	// What it waits for is the ticket's own list (the fence above makes it the list the request was proposed with).
	for _, id := range k.Dependencies {
		dep, err := tx.Ticket(ctx, a.Workspace.ID, v.ProjectID, id)
		if err != nil {
			return "dependency unavailable", nil
		}
		done := dep.Status == domain.TicketDone
		if schedule, err := tx.Schedule(ctx, a.Workspace.ID, v.ProjectID, id); err == nil {
			done = done || schedule.State == "completed"
		}
		if !done {
			if dep.ArchivedAt != nil {
				return "waiting for dependency " + dep.Key + ", which was archived without being finished; finish it or remove it from this ticket's dependencies", nil
			}
			return "waiting for dependency " + dep.Key, nil
		}
	}
	all, err := tx.Schedules(ctx, a.Workspace.ID, v.ProjectID)
	if err != nil {
		return "", err
	}
	for _, other := range all {
		if other.MemberID == v.MemberID && other.TicketID != v.TicketID && other.Order < v.Order && !other.Terminal() {
			return "waiting for earlier task", nil
		}
	}
	return "", nil
}
func (s *Service) Schedules(ctx context.Context, a Actor, pid string) ([]domain.Schedule, error) {
	out := []domain.Schedule{}
	err := s.view(ctx, a, pid, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "view schedules"); err != nil {
			return err
		}
		var err error
		out, err = tx.Schedules(ctx, a.Workspace.ID, pid)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Stale = out[i].DeviceID != "" && !out[i].Terminal() && s.stamp().Sub(out[i].UpdatedAt) > domain.DeviceOnlineWindow
			if out[i].Terminal() || out[i].RunID != "" {
				continue
			}
			reason, err := s.scheduleEligibility(ctx, tx, a, x, out[i])
			if err != nil {
				return err
			}
			out[i].Reason = reason
			if reason != "" {
				out[i].State = "blocked"
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Order != out[j].Order {
				return out[i].Order < out[j].Order
			}
			if out[i].Priority != out[j].Priority {
				return out[i].Priority > out[j].Priority
			}
			return out[i].At.Before(out[j].At)
		})
		return nil
	})
	return out, err
}

type ScheduleDispatch struct {
	ExecutionID string `json:"executionId"`
	Version     int64  `json:"version"`
	State       string `json:"state"`
	RunID       string `json:"runId"`
}

// DispatchSchedule binds one execution to one owner device transactionally.
// A claim is permanent across failover, never a lease that moves to another host.
func (s *Service) DispatchSchedule(ctx context.Context, a Actor, pid, tid string, in ScheduleDispatch) (domain.Schedule, error) {
	var out domain.Schedule
	err := s.mutate(ctx, a, pid, func(tx store.Tx, x access) error {
		if a.Device == nil || !x.Member {
			return forbidden("dispatch without your own enrolled device")
		}
		dev, err := tx.Device(ctx, a.Workspace.ID, a.Device.ID)
		if err != nil || dev.Revoked() || dev.MemberID != a.Member.ID || !dev.Has(domain.CapabilityRunner) {
			return domain.ErrUnauthenticated
		}
		v, err := tx.Schedule(ctx, a.Workspace.ID, pid, tid)
		if err != nil {
			return err
		}
		if v.MemberID != a.Member.ID {
			return forbidden("dispatch another member's work")
		}
		if v.ExecutionID != in.ExecutionID || v.Version != in.Version || v.Terminal() {
			return domain.ErrConflict
		}
		if v.DeviceID != "" && v.DeviceID != dev.ID {
			return fmt.Errorf("%w: execution already bound to another device", domain.ErrConflict)
		}
		eligibility := v
		if in.RunID != "" {
			eligibility.RunID = in.RunID
		}
		reason, err := s.scheduleEligibility(ctx, tx, a, x, eligibility)
		if err != nil {
			return err
		}
		if reason != "" {
			return fmt.Errorf("%w: %s", domain.ErrConflict, reason)
		}
		switch in.State {
		case "waiting_for_runner", "awaiting_approval", "queued", "executing", "blocked", "completed":
		default:
			return domain.ErrInvalid
		}
		if in.RunID != "" && !integration.Identifier(in.RunID) {
			return domain.ErrInvalid
		}
		if v.RunID != "" && in.RunID != v.RunID {
			return domain.ErrConflict
		}
		if (in.State == "executing" || in.State == "completed") && in.RunID == "" {
			return domain.ErrInvalid
		}
		if v.State == "executing" && in.State != "executing" && in.State != "completed" && in.State != "blocked" {
			return domain.ErrConflict
		}
		old := v.Version
		v.Version++
		v.State = in.State
		v.RunID = in.RunID
		if in.State == "queued" || in.State == "executing" || in.RunID != "" {
			v.DeviceID = dev.ID
		}
		v.UpdatedAt = s.stamp()
		out = v
		return tx.SaveSchedule(ctx, a.Workspace.ID, v, old)
	})
	return out, err
}
func (s *Service) CancelSchedule(ctx context.Context, a Actor, pid, tid string, version int64) error {
	return s.mutate(ctx, a, pid, func(tx store.Tx, x access) error {
		v, err := tx.Schedule(ctx, a.Workspace.ID, pid, tid)
		if err != nil {
			return err
		}
		if !x.can(domain.PPTicketsAssign) && v.MemberID != a.Member.ID {
			return forbidden("cancel another member's schedule")
		}
		if v.Version != version {
			return domain.ErrConflict
		}
		old := v.Version
		v.Version++
		v.State = "canceled"
		v.Reason = "request canceled; an accepted local run requires an explicit local stop"
		v.UpdatedAt = s.stamp()
		return tx.SaveSchedule(ctx, a.Workspace.ID, v, old)
	})
}
