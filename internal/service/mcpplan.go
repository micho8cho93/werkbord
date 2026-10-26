package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"werkbord/internal/domain"
	"werkbord/internal/integration"
	"werkbord/internal/mcpplan"
	"werkbord/internal/planning"
	"werkbord/internal/store"
)

type appliedPlanRecord struct {
	Digest string          `json:"digest"`
	Result mcpplan.Applied `json:"result"`
}

// PreviewMCPTaskPlan validates a proposed single-project batch and reports
// timeline conflicts involving its changed tasks.
func (s *Tasks) PreviewMCPTaskPlan(ctx context.Context, raw mcpplan.Plan) (mcpplan.Preview, error) {
	plan, err := mcpplan.Clean(raw)
	if err != nil {
		return mcpplan.Preview{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var preview mcpplan.Preview
	err = s.Store.View(ctx, func(tx store.Tx) error {
		_, _, _, warnings, err := prepareMCPTaskPlan(ctx, tx, s, plan, false)
		preview.Warnings = warnings
		return err
	})
	return preview, err
}

// ApplyMCPTaskPlan applies a plan in one write transaction. The idempotency key
// is scoped to the authenticated MCP principal and its project.
func (s *Tasks) ApplyMCPTaskPlan(ctx context.Context, principalID string, raw mcpplan.Plan) (mcpplan.Applied, error) {
	plan, err := mcpplan.Clean(raw)
	if err != nil {
		return mcpplan.Applied{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if principalID == "" || len(principalID) > 160 {
		return mcpplan.Applied{}, fmt.Errorf("%w: invalid plan principal", domain.ErrInvalid)
	}
	key := "mcp-task-plan:" + strings.TrimSpace(principalID) + ":" + plan.ProjectID + ":" + plan.IdempotencyKey
	digest := mcpplan.Digest(plan)
	var result mcpplan.Applied
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		var prior appliedPlanRecord
		if err := tx.Settings().Get(ctx, key, &prior); err == nil {
			if prior.Digest != digest {
				return fmt.Errorf("%w: this idempotency key was used for a different task plan", domain.ErrConflict)
			}
			result = prior.Result
			return nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		tasks, ids, _, _, err := prepareMCPTaskPlan(ctx, tx, s, plan, true)
		if err != nil {
			return err
		}
		result = mcpplan.Applied{Created: []string{}, Updated: []string{}, TempIDs: ids}
		for _, item := range plan.Tasks {
			task := tasks[item.TempID]
			if item.TaskID != "" {
				task = tasks[item.TaskID]
			}
			if item.TempID != "" {
				if err := tx.Tasks().Create(ctx, task); err != nil {
					return err
				}
				result.Created = append(result.Created, task.ID)
				ev := newEvent(domain.EventTaskCreated, task)
				ev.ProjectID, ev.TaskID = task.ProjectID, task.ID
				if err := em.emit(ev); err != nil {
					return err
				}
			} else {
				if err := tx.Tasks().Update(ctx, task); err != nil {
					return err
				}
				result.Updated = append(result.Updated, task.ID)
				ev := newEvent(domain.EventTaskUpdated, task)
				ev.ProjectID, ev.TaskID = task.ProjectID, task.ID
				if err := em.emit(ev); err != nil {
					return err
				}
			}
			if task.SourceRef != "" {
				var bound integration.Imported
				err := tx.Settings().Get(ctx, integrationSourceKey(task.SourceRef), &bound)
				if err == nil && (bound.ProjectID != plan.ProjectID || bound.TaskID != task.ID) {
					return fmt.Errorf("%w: source reference %s already belongs to another task", domain.ErrConflict, task.SourceRef)
				}
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					return err
				}
				if errors.Is(err, domain.ErrNotFound) {
					if err := tx.Settings().Set(ctx, integrationSourceKey(task.SourceRef), integration.Imported{Schema: integration.Schema, ProjectID: plan.ProjectID, TaskID: task.ID}, s.now()); err != nil {
						return err
					}
				}
			}
		}
		return tx.Settings().Set(ctx, key, appliedPlanRecord{Digest: digest, Result: result}, s.now())
	})
	return result, err
}

// prepareMCPTaskPlan builds and validates the final task set. New IDs are
// generated before dependencies are resolved so a batch may contain a chain.
func prepareMCPTaskPlan(ctx context.Context, tx store.Tx, s *Tasks, plan mcpplan.Plan, applying bool) (map[string]*domain.Task, map[string]string, map[string]bool, []planning.Warning, error) {
	project, err := tx.Projects().Get(ctx, plan.ProjectID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	all, err := tx.Tasks().ListByProject(ctx, plan.ProjectID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	byID := make(map[string]domain.Task, len(all))
	for _, task := range all {
		byID[task.ID] = task
	}
	ids := make(map[string]string)
	for _, change := range plan.Tasks {
		if change.TempID != "" {
			id := change.TempID
			if applying {
				id = domain.NewID(domain.PrefixTask)
			}
			ids[change.TempID] = id
		}
	}
	changes := make(map[string]*domain.Task, len(plan.Tasks))
	touched := make(map[string]bool, len(plan.Tasks))
	maxPosition, err := tx.Tasks().MaxPosition(ctx, plan.ProjectID, domain.TaskBacklog)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	now := s.now()
	for _, change := range plan.Tasks {
		var task *domain.Task
		if change.TempID != "" {
			title, _ := domain.ValidateTaskTitle(*change.Title)
			mode := planning.ModeAgent
			if !project.HasRepository() {
				mode = planning.ModeHuman
			}
			task = &domain.Task{ID: ids[change.TempID], ProjectID: plan.ProjectID, Title: title, State: domain.TaskBacklog,
				Position: maxPosition + 1, WorkMode: mode, LabelIDs: []string{}, CreatedAt: now, UpdatedAt: now, Version: 1}
			maxPosition++
		} else {
			existing, ok := byID[change.TaskID]
			if !ok {
				return nil, nil, nil, nil, fmt.Errorf("%w: task %s was not found in this project", domain.ErrNotFound, change.TaskID)
			}
			if existing.Version != change.Version {
				return nil, nil, nil, nil, fmt.Errorf("%w: task %s changed; expected version %d and found %d", domain.ErrConflict, existing.ID, change.Version, existing.Version)
			}
			if existing.ArchivedAt != nil {
				return nil, nil, nil, nil, fmt.Errorf("%w: restore task %s before editing it", domain.ErrConflict, existing.ID)
			}
			copy := existing
			task = &copy
		}
		if change.Title != nil {
			if task.Title, err = domain.ValidateTaskTitle(*change.Title); err != nil {
				return nil, nil, nil, nil, err
			}
		}
		if change.Description != nil {
			if err := domain.ValidateTaskDescription(*change.Description); err != nil {
				return nil, nil, nil, nil, err
			}
			task.Description = *change.Description
		}
		if change.Plan != nil {
			if task.Plan, err = planning.CleanRange(*change.Plan); err != nil {
				return nil, nil, nil, nil, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
			}
		}
		if change.SourceRef != nil {
			if *change.SourceRef != "" && !project.HasRepository() {
				return nil, nil, nil, nil, fmt.Errorf("%w: source references require a repository project", domain.ErrInvalid)
			}
			task.SourceRef = *change.SourceRef
		}
		if task.SourceRef != "" {
			var bound integration.Imported
			if err := tx.Settings().Get(ctx, integrationSourceKey(task.SourceRef), &bound); err == nil &&
				(bound.ProjectID != plan.ProjectID || bound.TaskID != task.ID) {
				return nil, nil, nil, nil, fmt.Errorf("%w: source reference %s already belongs to another task", domain.ErrConflict, task.SourceRef)
			} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return nil, nil, nil, nil, err
			}
		}
		if change.Dependencies != nil {
			deps := make([]string, 0, len(*change.Dependencies))
			for _, dependency := range *change.Dependencies {
				if id, ok := ids[dependency]; ok {
					deps = append(deps, id)
				} else if _, ok := byID[dependency]; ok {
					deps = append(deps, dependency)
				} else {
					return nil, nil, nil, nil, fmt.Errorf("%w: dependency %s does not exist in this project", domain.ErrInvalid, dependency)
				}
			}
			task.Orchestration.Dependencies = deps
		}
		if change.TempID == "" || applying {
			task.UpdatedAt = now
		}
		key := change.TaskID
		if change.TempID != "" {
			key = change.TempID
		}
		changes[key] = task
		touched[task.ID] = true
	}
	items := make([]planning.Item, 0, len(all)+len(changes))
	sourceRefs := map[string]string{}
	for _, existing := range all {
		candidate := existing
		if updated := changes[existing.ID]; updated != nil {
			candidate = *updated
		}
		if candidate.SourceRef != "" {
			if prior := sourceRefs[candidate.SourceRef]; prior != "" && prior != candidate.ID {
				return nil, nil, nil, nil, fmt.Errorf("%w: source reference %s belongs to more than one task", domain.ErrConflict, candidate.SourceRef)
			}
			sourceRefs[candidate.SourceRef] = candidate.ID
		}
		items = append(items, planning.Item{ID: candidate.ID, Title: candidate.Title, Range: candidate.Plan,
			Done: candidate.State == domain.TaskDone, Archived: candidate.ArchivedAt != nil, Dependencies: candidate.Orchestration.Dependencies})
	}
	for key, candidate := range changes {
		if strings.HasPrefix(key, "tmp_") {
			if candidate.SourceRef != "" {
				if prior := sourceRefs[candidate.SourceRef]; prior != "" && prior != candidate.ID {
					return nil, nil, nil, nil, fmt.Errorf("%w: source reference %s belongs to more than one task", domain.ErrConflict, candidate.SourceRef)
				}
				sourceRefs[candidate.SourceRef] = candidate.ID
			}
			items = append(items, planning.Item{ID: candidate.ID, Title: candidate.Title, Range: candidate.Plan,
				Dependencies: candidate.Orchestration.Dependencies})
		}
	}
	warnings := planning.AnalyzeOpen(items)
	for _, warning := range warnings {
		if warning.Severity != planning.SeverityError {
			continue
		}
		relevant := touched[warning.ItemID]
		for _, cycleID := range warning.Cycle {
			relevant = relevant || touched[cycleID]
		}
		if relevant {
			return nil, nil, nil, nil, fmt.Errorf("%w: %s", domain.ErrInvalid, warning.Message)
		}
	}
	visible := make([]planning.Warning, 0, len(warnings))
	for _, warning := range warnings {
		if touched[warning.ItemID] || touched[warning.OtherID] {
			visible = append(visible, warning)
		}
	}
	return changes, ids, touched, visible, nil
}
