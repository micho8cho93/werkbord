package appops

import (
	"context"
	"encoding/json"
	"fmt"
	"werkbord/internal/domain"
)

// WorkPlanBackend offers reference-only organisation through the task service.
type WorkPlanBackend interface {
	WorkPlans(context.Context, string) ([]domain.WorkPlan, error)
	SaveWorkPlan(context.Context, string, string, domain.WorkPlan) (domain.WorkPlan, error)
}

func (b servicesBackend) WorkPlans(ctx context.Context, pid string) ([]domain.WorkPlan, error) {
	return b.s.Tasks.WorkPlans(ctx, pid)
}
func (b servicesBackend) SaveWorkPlan(ctx context.Context, pid, id string, p domain.WorkPlan) (domain.WorkPlan, error) {
	return b.s.Tasks.SaveWorkPlan(ctx, pid, id, p)
}
func workPlanOps() []*operation {
	fields := map[string]*Schema{"projectId": projectID, "planId": Str("Existing plan id; empty creates a plan.", idMax), "title": Str("Plan title.", titleMax), "description": Str("Plan description.", descMax), "taskIds": List("Existing task ids in progression order. Dependencies belong to the tasks.", ticketID, 200), "decisions": Str("Relevant decisions.", descMax), "questions": Str("Open questions.", descMax), "version": Int("Version last read, or zero for a new plan.", 0, 1<<30)}
	return []*operation{
		{Spec: Spec{Name: "list_work_plans", Title: "Read Work plans", Description: "Read structured plans and their ordered task references. Use get_ticket to inspect dependencies and associated sessions.", Kind: KindRead, Permission: PermTicketsRead, Input: Object(map[string]*Schema{"projectId": projectID}, "projectId")}, run: func(ctx context.Context, e *env, a map[string]any) (any, error) {
			b, ok := e.b.(WorkPlanBackend)
			if !ok {
				return nil, errorf(CodeUnavailable, "Work plans unavailable")
			}
			return b.WorkPlans(ctx, str(a, "projectId"))
		}},
		{Spec: Spec{Name: "save_work_plan", Title: "Save a Work plan", Description: "Create or replace a structured plan containing existing tasks in order, description, decisions and open questions. Read an existing plan before editing. Creates no execution and starts no agent. The person reviews all fields before saving.", Kind: KindMutation, Permission: PermTicketsUpdate, RequiresConfirmation: true, Input: Object(fields, "projectId", "title", "description", "taskIds", "decisions", "questions", "version")}, plan: prepareWorkPlan, run: runWorkPlan},
	}
}
func workPlanFrom(a map[string]any) domain.WorkPlan {
	raw, _ := json.Marshal(a)
	var p domain.WorkPlan
	_ = json.Unmarshal(raw, &p)
	p.ID = str(a, "planId")
	return p
}
func prepareWorkPlan(ctx context.Context, e *env, a map[string]any) (*plan, error) {
	pid := str(a, "projectId")
	if _, err := e.b.Project(ctx, pid); err != nil {
		return nil, err
	}
	b, ok := e.b.(WorkPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	p := workPlanFrom(a)
	if _, err := domain.ValidateTaskTitle(p.Title); err != nil {
		return nil, err
	}
	all, err := b.WorkPlans(ctx, pid)
	if err != nil {
		return nil, err
	}
	if p.ID != "" {
		found := false
		for _, old := range all {
			if old.ID == p.ID {
				found = true
				if old.Version != p.Version {
					return nil, domain.ErrConflict
				}
			}
		}
		if !found {
			return nil, domain.ErrNotFound
		}
	} else if p.Version != 0 {
		return nil, domain.ErrConflict
	}
	seen := map[string]bool{}
	for _, id := range p.TaskIDs {
		if seen[id] {
			return nil, domain.ErrInvalid
		}
		seen[id] = true
		if _, err := e.b.Ticket(ctx, pid, id); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(p)
	return &plan{Args: a, Summary: fmt.Sprintf("Save Work plan in %s: %s", pid, string(raw))}, nil
}
func runWorkPlan(ctx context.Context, e *env, a map[string]any) (any, error) {
	b, ok := e.b.(WorkPlanBackend)
	if !ok {
		return nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	p := workPlanFrom(a)
	return b.SaveWorkPlan(ctx, str(a, "projectId"), p.ID, p)
}
func (b *workspaceBackend) WorkPlans(ctx context.Context, pid string) ([]domain.WorkPlan, error) {
	w, id, connected := b.locate(pid)
	if !connected {
		if local, ok := b.Backend.(WorkPlanBackend); ok {
			return local.WorkPlans(ctx, pid)
		}
		return nil, errorf(CodeUnavailable, "Work plans unavailable")
	}
	var out []domain.WorkPlan
	err := b.read(ctx, w, "list_work_plans", map[string]string{"projectId": id}, &out)
	for i := range out {
		out[i].ProjectID = pid
	}
	return out, err
}
func (b *workspaceBackend) SaveWorkPlan(ctx context.Context, pid, id string, p domain.WorkPlan) (domain.WorkPlan, error) {
	w, project, connected := b.locate(pid)
	if !connected {
		if local, ok := b.Backend.(WorkPlanBackend); ok {
			return local.SaveWorkPlan(ctx, pid, id, p)
		}
		return p, errorf(CodeUnavailable, "Work plans unavailable")
	}
	p.ProjectID = project
	raw, _ := json.Marshal(p)
	var out domain.WorkPlan
	err := b.read(ctx, w, "save_work_plan", map[string]string{"projectId": project, "planJSON": string(raw)}, &out)
	out.ProjectID = pid
	return out, err
}
