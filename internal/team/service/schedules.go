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
)

type ScheduleInput struct {
	Version      int64     `json:"version"`
	At           time.Time `json:"at"`
	Timezone     string    `json:"timezone"`
	MissedPolicy string    `json:"missedPolicy"`
	GraceSeconds int       `json:"graceSeconds"`
	Order        int       `json:"order"`
	Priority     int       `json:"priority"`
	Dependencies []string  `json:"dependencies"`
}

func ScheduleContext(w string, p domain.Project, k domain.Ticket) string {
	raw, _ := json.Marshal(struct {
		Workspace, Project, Ticket, Member, Repository, Title, Description, Requirements string
		Assignment                                                                       int64
	}{w, p.ID, k.ID, k.AssigneeID, p.Repository, k.Title, k.Description, k.Requirements, k.Assignment})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
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
		if in.At.IsZero() || in.At.Year() < 2020 || in.At.Year() > 9999 || in.Timezone == "" || len(in.Dependencies) > 100 || in.GraceSeconds < 0 || in.GraceSeconds > 86400 || in.Priority < 0 || in.Priority > 2 || in.Order < 0 {
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
		all, err := tx.Schedules(ctx, a.Workspace.ID, pid)
		if err != nil {
			return err
		}
		graph := map[string][]string{}
		for _, v := range all {
			if !v.Terminal() {
				graph[v.TicketID] = v.Dependencies
			}
		}
		graph[tid] = in.Dependencies
		seen := map[string]bool{}
		for _, id := range in.Dependencies {
			if !integration.Identifier(id) || id == tid || seen[id] {
				return domain.ErrInvalid
			}
			seen[id] = true
			dep, err := tx.Ticket(ctx, a.Workspace.ID, pid, id)
			if err != nil {
				return err
			}
			if dep.ArchivedAt != nil {
				return domain.ErrConflict
			}
		}
		var visit func(string, map[string]bool) bool
		visit = func(id string, path map[string]bool) bool {
			if path[id] {
				return true
			}
			path[id] = true
			for _, d := range graph[id] {
				if visit(d, path) {
					return true
				}
			}
			delete(path, id)
			return false
		}
		if visit(tid, map[string]bool{}) {
			return fmt.Errorf("%w: dependency cycle", domain.ErrInvalid)
		}
		out = domain.Schedule{ID: domain.NewID("sch"), ExecutionID: domain.NewID("exe"), ProjectID: pid, TicketID: tid, MemberID: k.AssigneeID, Assignment: k.Assignment, Fence: ScheduleContext(a.Workspace.ID, x.Project, k), Version: old.Version + 1, At: in.At.UTC(), Timezone: in.Timezone, MissedPolicy: in.MissedPolicy, GraceSeconds: in.GraceSeconds, Order: in.Order, Priority: in.Priority, Dependencies: in.Dependencies, State: "waiting_for_runner", UpdatedAt: s.stamp()}
		return tx.SaveSchedule(ctx, a.Workspace.ID, out, old.Version)
	})
	return out, err
}
func (s *Service) scheduleEligibility(ctx context.Context, tx store.Tx, a Actor, x access, v domain.Schedule) (string, error) {
	k, err := tx.Ticket(ctx, a.Workspace.ID, v.ProjectID, v.TicketID)
	if err != nil {
		return "", err
	}
	if x.Project.Archived || k.ArchivedAt != nil || !k.Status.Held() || k.AssigneeID != v.MemberID || k.Assignment != v.Assignment || ScheduleContext(a.Workspace.ID, x.Project, k) != v.Fence {
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
	for _, id := range v.Dependencies {
		dep, err := tx.Ticket(ctx, a.Workspace.ID, v.ProjectID, id)
		if err != nil {
			return "dependency unavailable", nil
		}
		done := dep.Status == domain.TicketDone
		if schedule, err := tx.Schedule(ctx, a.Workspace.ID, v.ProjectID, id); err == nil {
			done = done || schedule.State == "completed"
		}
		if !done {
			return "waiting for dependency", nil
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
