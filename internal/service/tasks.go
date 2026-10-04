package service

import (
	"context"
	"fmt"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Tasks manages tasks on project boards.
type Tasks struct {
	Deps
}

// TaskPatch is a partial update. Nil fields are left unchanged. Version must
// be the version the caller last read; a mismatch returns domain.ErrConflict.
type TaskPatch struct {
	Title       *string
	Description *string
	State       *domain.TaskState
	Position    *float64
	Version     int64
}

// Create adds a task to the bottom of the project's Backlog.
func (s *Tasks) Create(ctx context.Context, projectID, title, description string) (*domain.Task, error) {
	title, err := domain.ValidateTaskTitle(title)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateTaskDescription(description); err != nil {
		return nil, err
	}
	now := s.now()
	t := &domain.Task{
		ID: domain.NewID(domain.PrefixTask), ProjectID: projectID, Title: title, Description: description,
		State: domain.TaskBacklog, CreatedAt: now, UpdatedAt: now,
	}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		max, err := tx.Tasks().MaxPosition(ctx, projectID, t.State)
		if err != nil {
			return err
		}
		t.Position = max + 1
		if err := tx.Tasks().Create(ctx, t); err != nil {
			return err
		}
		ev := newEvent(domain.EventTaskCreated, t)
		ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
		return em.emit(ev)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Update applies a patch with compare-and-swap semantics. Changing State
// without a Position moves the task to the bottom of the target column.
//
// Moving a task never starts, stops or otherwise affects a run.
func (s *Tasks) Update(ctx context.Context, id string, patch TaskPatch) (*domain.Task, error) {
	var t *domain.Task
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if t, err = tx.Tasks().Get(ctx, id); err != nil {
			return err
		}
		if t.Version != patch.Version {
			return fmt.Errorf("task %s is at version %d, not %d: %w", id, t.Version, patch.Version, domain.ErrConflict)
		}
		if patch.Title != nil {
			if t.Title, err = domain.ValidateTaskTitle(*patch.Title); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if err := domain.ValidateTaskDescription(*patch.Description); err != nil {
				return err
			}
			t.Description = *patch.Description
		}
		if patch.State != nil {
			if !patch.State.Valid() {
				return fmt.Errorf("%w: unknown task state %q", domain.ErrInvalid, *patch.State)
			}
			if *patch.State != t.State && patch.Position == nil {
				max, err := tx.Tasks().MaxPosition(ctx, t.ProjectID, *patch.State)
				if err != nil {
					return err
				}
				t.Position = max + 1
			}
			t.State = *patch.State
		}
		if patch.Position != nil {
			t.Position = *patch.Position
		}
		t.UpdatedAt = s.now()
		if err := tx.Tasks().Update(ctx, t); err != nil {
			return err
		}
		ev := newEvent(domain.EventTaskUpdated, t)
		ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
		return em.emit(ev)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns a task.
func (s *Tasks) Get(ctx context.Context, id string) (*domain.Task, error) {
	var t *domain.Task
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		t, err = tx.Tasks().Get(ctx, id)
		return err
	})
	return t, err
}

// List returns a project's tasks ordered by column and position.
func (s *Tasks) List(ctx context.Context, projectID string) ([]domain.Task, error) {
	var out []domain.Task
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		var err error
		out, err = tx.Tasks().ListByProject(ctx, projectID)
		return err
	})
	return out, err
}

// moveTaskToDoing is the policy that connects a run to its card: a task whose
// agent starts or resumes working belongs in Doing. A task already in Doing, or
// finished in Done, is left where it is. The move is part of the caller's
// transaction, so card and run never disagree.
func moveTaskToDoing(ctx context.Context, tx store.Tx, em *emitter, taskID string, now time.Time) error {
	t, err := tx.Tasks().Get(ctx, taskID)
	if err != nil {
		return err
	}
	if t.State == domain.TaskDoing || t.State == domain.TaskDone {
		return nil
	}
	max, err := tx.Tasks().MaxPosition(ctx, t.ProjectID, domain.TaskDoing)
	if err != nil {
		return err
	}
	t.State, t.Position, t.UpdatedAt = domain.TaskDoing, max+1, now
	if err := tx.Tasks().Update(ctx, t); err != nil {
		return err
	}
	ev := newEvent(domain.EventTaskUpdated, t)
	ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
	return em.emit(ev)
}
