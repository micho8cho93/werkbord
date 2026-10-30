package service

import (
	"context"
	"fmt"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func (s *Tasks) WorkPlans(ctx context.Context, project string) ([]domain.WorkPlan, error) {
	var out []domain.WorkPlan
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, project); err != nil {
			return err
		}
		var err error
		out, err = tx.WorkPlans().List(ctx, project)
		return err
	})
	return out, err
}

// SaveWorkPlan preserves task identity, order, dependencies and execution history.
func (s *Tasks) SaveWorkPlan(ctx context.Context, project, id string, in domain.WorkPlan) (domain.WorkPlan, error) {
	out := in
	title, err := domain.ValidateTaskTitle(in.Title)
	if err != nil {
		return out, err
	}
	out.Title = title
	for _, text := range []string{in.Description, in.Decisions, in.Questions} {
		if len(text) > 20000 {
			return out, fmt.Errorf("%w: plan text is too long", domain.ErrInvalid)
		}
	}
	if len(in.TaskIDs) > 200 {
		return out, domain.ErrInvalid
	}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		p, err := tx.Projects().Get(ctx, project)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return domain.ErrConflict
		}
		plans, err := tx.WorkPlans().List(ctx, project)
		if err != nil {
			return err
		}
		out.ProjectID = project
		out.ID = id
		if id == "" {
			if in.Version != 0 || len(plans) >= 200 {
				return domain.ErrConflict
			}
			out.ID = domain.NewID("wpl")
			out.CreatedAt = s.now()
		} else {
			found := false
			for _, old := range plans {
				if old.ID == id {
					found = true
					if old.Version != in.Version {
						return domain.ErrConflict
					}
					out.CreatedAt = old.CreatedAt
				}
			}
			if !found {
				return domain.ErrNotFound
			}
		}
		seen := map[string]bool{}
		for _, tid := range in.TaskIDs {
			if seen[tid] {
				return domain.ErrInvalid
			}
			seen[tid] = true
			t, err := tx.Tasks().Get(ctx, tid)
			if err != nil {
				return err
			}
			if t.ProjectID != project {
				return domain.ErrNotFound
			}
		}
		out.Version = in.Version + 1
		out.UpdatedAt = s.now()
		if out.TaskIDs == nil {
			out.TaskIDs = []string{}
		}
		if err := tx.WorkPlans().Save(ctx, out, in.Version); err != nil {
			return err
		}
		ev := newEvent(domain.EventWorkPlanUpdated, map[string]any{"id": out.ID, "version": out.Version})
		ev.ProjectID = project
		return em.emit(ev)
	})
	return out, err
}
