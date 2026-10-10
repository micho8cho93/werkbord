package service

import (
	"context"
	"errors"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/planning"
	"devboard/internal/store"
)

// Labels manages the reusable labels. A label is defined once and put on tasks in any project; this
// service defines, renames, recolours and deletes them. Putting one on a task is the Tasks service's
// (the task's own version guards it).
//
// The controller has one owner, who is authenticated before any of this is reached, so there is no
// per-label permission here: labels are shared between that person's projects, not between people.
// (A Team workspace shares labels between people and asks its roles who may manage them; that is
// Team's own, in internal/team.)
type Labels struct {
	Deps
}

// NewLabel describes a label to add.
type NewLabel struct {
	Name        string
	Color       string
	Description string
}

// LabelPatch is a partial update. Nil fields are left unchanged; Version must be the version the
// caller last read.
type LabelPatch struct {
	Name        *string
	Color       *string
	Description *string
	Version     int64
}

func invalidPlanning(err error) error { return fmt.Errorf("%w: %s", domain.ErrInvalid, err) }

// List returns every label with how many open tasks carry it.
func (s *Labels) List(ctx context.Context) ([]domain.LabelUse, error) {
	var out []domain.LabelUse
	err := s.Store.View(ctx, func(tx store.Tx) error {
		ls, err := tx.Labels().List(ctx)
		if err != nil {
			return err
		}
		use, err := tx.Labels().Usage(ctx)
		if err != nil {
			return err
		}
		out = make([]domain.LabelUse, 0, len(ls))
		for _, l := range ls {
			out = append(out, domain.LabelUse{Label: l, Tasks: use[l.ID]})
		}
		return nil
	})
	return out, err
}

// Create defines a label. Two labels cannot have the same name, ignoring case and spacing.
func (s *Labels) Create(ctx context.Context, in NewLabel) (*domain.Label, error) {
	name, err := planning.CleanLabelName(in.Name)
	if err != nil {
		return nil, invalidPlanning(err)
	}
	color, err := planning.CleanLabelColor(in.Color)
	if err != nil {
		return nil, invalidPlanning(err)
	}
	desc, err := planning.CleanLabelDescription(in.Description)
	if err != nil {
		return nil, invalidPlanning(err)
	}
	now := s.now()
	l := &domain.Label{ID: domain.NewID(domain.PrefixLabel), Name: name, Color: color, Description: desc, CreatedAt: now, UpdatedAt: now}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		n, err := tx.Labels().Count(ctx)
		if err != nil {
			return err
		}
		if n >= planning.MaxLabels {
			return fmt.Errorf("%w: there are already %d labels; delete some before adding more", domain.ErrConflict, n)
		}
		if err := tx.Labels().Create(ctx, l); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventLabelCreated, l))
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// Update renames, recolours or re-describes a label, wherever it is used. A stale version is a conflict.
func (s *Labels) Update(ctx context.Context, id string, patch LabelPatch) (*domain.Label, error) {
	var l *domain.Label
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if l, err = tx.Labels().Get(ctx, id); err != nil {
			return err
		}
		if l.Version != patch.Version {
			return fmt.Errorf("label %s is at version %d, not %d: %w", id, l.Version, patch.Version, domain.ErrConflict)
		}
		if patch.Name != nil {
			if l.Name, err = planning.CleanLabelName(*patch.Name); err != nil {
				return invalidPlanning(err)
			}
		}
		if patch.Color != nil {
			if l.Color, err = planning.CleanLabelColor(*patch.Color); err != nil {
				return invalidPlanning(err)
			}
		}
		if patch.Description != nil {
			if l.Description, err = planning.CleanLabelDescription(*patch.Description); err != nil {
				return invalidPlanning(err)
			}
		}
		l.UpdatedAt = s.now()
		if err := tx.Labels().Update(ctx, l); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventLabelUpdated, l))
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}

// Delete removes a label from every task that carries it, and then the label. The tasks are otherwise
// untouched, so their versions move on (a client holding one sees it changed) and nothing is started,
// stopped or rescheduled. Closed tasks lose it too.
func (s *Labels) Delete(ctx context.Context, id string) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Labels().Get(ctx, id); err != nil {
			return err
		}
		taskIDs, err := tx.Labels().TaskIDs(ctx, id)
		if err != nil {
			return err
		}
		now := s.now()
		for _, taskID := range taskIDs {
			t, err := tx.Tasks().Get(ctx, taskID)
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			kept := make([]string, 0, len(t.LabelIDs))
			for _, l := range t.LabelIDs {
				if l != id {
					kept = append(kept, l)
				}
			}
			t.LabelIDs, t.UpdatedAt = kept, now
			if err := tx.Tasks().Update(ctx, t); err != nil {
				return err
			}
			ev := newEvent(domain.EventTaskUpdated, t)
			ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
			if err := em.emit(ev); err != nil {
				return err
			}
		}
		if err := tx.Labels().Delete(ctx, id); err != nil {
			return err
		}
		return em.emit(newEvent(domain.EventLabelDeleted, map[string]string{"id": id}))
	})
}
