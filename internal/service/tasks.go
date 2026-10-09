package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

// Tasks manages tasks on project boards.
type Tasks struct {
	Deps
	Catalog AgentCatalog // checks the agent in an execution config; nil checks only its shape
}

// TaskPatch is a partial update. Nil fields are left unchanged. Version must
// be the version the caller last read; a mismatch returns domain.ErrConflict.
type TaskPatch struct {
	Title       *string
	Description *string
	State       *domain.TaskState
	Position    *float64
	// Execution replaces the task's overrides of the execution defaults: whatever
	// it leaves unset is inherited from the project and the global defaults. It
	// applies to runs started afterwards: a run that is already working keeps what
	// it started with.
	Execution     *domain.ExecutionConfig
	Orchestration *domain.Orchestration
	Version       int64
	Archived      *bool
}

// NewTask describes a task to add.
type NewTask struct {
	SourceRef   string
	WorkBranch  string
	BaseBranch  string
	ProjectID   string
	Title       string
	Description string
	// Execution is what the task overrides about how its runs are carried out;
	// anything unset is inherited. A new task starts with no overrides.
	Execution     domain.ExecutionConfig
	Orchestration domain.Orchestration
}

// Create adds a task with no overrides to the bottom of the project's Backlog.
func (s *Tasks) Create(ctx context.Context, projectID, title, description string) (*domain.Task, error) {
	return s.CreateTask(ctx, NewTask{ProjectID: projectID, Title: title, Description: description})
}

// CreateTask adds a task to the bottom of the project's Backlog.
func (s *Tasks) CreateTask(ctx context.Context, in NewTask) (*domain.Task, error) {
	projectID := in.ProjectID
	if len(in.SourceRef) > 2000 {
		return nil, domain.ErrInvalid
	}
	for _, branch := range []string{in.WorkBranch, in.BaseBranch} {
		if branch != "" {
			if err := domain.ValidateRefName(branch); err != nil {
				return nil, err
			}
		}
	}
	title, err := domain.ValidateTaskTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateTaskDescription(in.Description); err != nil {
		return nil, err
	}
	exec := in.Execution.Normalized()
	if err := validateExecution(ctx, s.Catalog, exec); err != nil {
		return nil, err
	}
	now := s.now()
	t := &domain.Task{
		SourceRef: in.SourceRef, WorkBranch: in.WorkBranch, BaseBranch: in.BaseBranch,
		ID: domain.NewID(domain.PrefixTask), ProjectID: projectID, Title: title, Description: in.Description,
		State: domain.TaskBacklog, Execution: exec, Orchestration: in.Orchestration, CreatedAt: now, UpdatedAt: now,
	}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		if in.SourceRef != "" {
			var bound integration.Imported
			err := tx.Settings().Get(ctx, integrationSourceKey(in.SourceRef), &bound)
			if err == nil {
				if bound.ProjectID != projectID {
					return fmt.Errorf("%w: source already belongs to another project", domain.ErrConflict)
				}
				t, err = tx.Tasks().Get(ctx, bound.TaskID)
				return err
			}
			if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		if in.SourceRef != "" || in.WorkBranch != "" {
			all, err := tx.Tasks().ListByProject(ctx, projectID)
			if err != nil {
				return err
			}
			for _, existing := range all {
				if in.SourceRef != "" && existing.SourceRef == in.SourceRef {
					t = &existing
					return nil
				}
				if in.WorkBranch != "" && existing.WorkBranch == in.WorkBranch {
					return fmt.Errorf("%w: branch %s is associated with another task (%s)", domain.ErrConflict, in.WorkBranch, existing.ID)
				}
			}
		}
		max, err := tx.Tasks().MaxPosition(ctx, projectID, t.State)
		if err != nil {
			return err
		}
		t.Position = max + 1
		if err := configureOrchestration(ctx, tx, t, in.Orchestration); err != nil {
			return err
		}
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
		if t.ArchivedAt != nil && (patch.Archived == nil || *patch.Archived) {
			return fmt.Errorf("%w: restore this task before editing it", domain.ErrConflict)
		}
		if patch.Archived != nil {
			if *patch.Archived {
				if err := archiveTask(ctx, tx, t, s.now()); err != nil {
					return err
				}
			} else {
				t.ArchivedAt = nil
			}
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
		if patch.Execution != nil {
			exec := patch.Execution.Normalized()
			if err := validateExecution(ctx, s.Catalog, exec); err != nil {
				return err
			}
			t.Execution = exec
		}
		if patch.Orchestration != nil {
			if err := configureOrchestration(ctx, tx, t, *patch.Orchestration); err != nil {
				return err
			}
		}
		if t.ArchivedAt != nil {
			t.Orchestration.Enabled = false
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

// archiveTask preserves the task and every run. Active sessions must be stopped
// and acknowledged by their runner before the task can leave the board.
func archiveTask(ctx context.Context, tx store.Tx, t *domain.Task, now time.Time) error {
	runs, err := tx.Runs().ListByTask(ctx, t.ID)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.State.Active() {
			return fmt.Errorf("%w: stop the active run before closing this task", domain.ErrConflict)
		}
	}
	t.ArchivedAt = &now
	t.Orchestration.Enabled = false
	return nil
}

// ArchiveDone clears Done atomically: any active run refuses the entire batch.
func (s *Tasks) ArchiveDone(ctx context.Context, projectID string) ([]domain.Task, error) {
	out := []domain.Task{}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		tasks, err := tx.Tasks().ListByProject(ctx, projectID)
		if err != nil {
			return err
		}
		now := s.now()
		for _, t := range tasks {
			if t.State != domain.TaskDone || t.ArchivedAt != nil {
				continue
			}
			if err := archiveTask(ctx, tx, &t, now); err != nil {
				return err
			}
			t.UpdatedAt = now
			if err := tx.Tasks().Update(ctx, &t); err != nil {
				return err
			}
			ev := newEvent(domain.EventTaskUpdated, t)
			ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
			if err := em.emit(ev); err != nil {
				return err
			}
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetIn returns a task of the given project. A task that belongs to another
// project is reported as not found, exactly like one that does not exist, so
// that a request scoped to one project can never reveal another's.
func (s *Tasks) GetIn(ctx context.Context, projectID, id string) (*domain.Task, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.ProjectID != projectID {
		return nil, fmt.Errorf("task %s: %w", id, domain.ErrNotFound)
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
