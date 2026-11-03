package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// The trash. A Done task is archived (by hand, or by the project's own setting after some days), an archived one can be deleted, and a
// deleted one waits TrashRetention to be restored before it is erased. A deleted task is always an archived one, so everything that
// already leaves archived tasks out (the board, plans, the scheduler, Calendar) leaves it out without being told.

// Trash deletes an archived task: it leaves the Archive for Recently deleted, where it can be restored for TrashRetention.
func (s *Tasks) Trash(ctx context.Context, projectID, id string, version int64) (*domain.Task, error) {
	return s.moveInTrash(ctx, projectID, id, version, true)
}

// Untrash brings a deleted task back to the Archive.
func (s *Tasks) Untrash(ctx context.Context, projectID, id string, version int64) (*domain.Task, error) {
	return s.moveInTrash(ctx, projectID, id, version, false)
}

func (s *Tasks) moveInTrash(ctx context.Context, projectID, id string, version int64, deleting bool) (*domain.Task, error) {
	var t *domain.Task
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if t, err = tx.Tasks().Get(ctx, id); err != nil {
			return err
		}
		if t.ProjectID != projectID {
			return fmt.Errorf("task %s: %w", id, domain.ErrNotFound)
		}
		if t.Version != version {
			return fmt.Errorf("task %s is at version %d, not %d: %w", id, t.Version, version, domain.ErrConflict)
		}
		switch {
		case deleting && t.ArchivedAt == nil:
			return fmt.Errorf("%w: archive this task before deleting it", domain.ErrConflict)
		case deleting && t.DeletedAt != nil:
			return fmt.Errorf("%w: this task is already deleted", domain.ErrConflict)
		case !deleting && t.DeletedAt == nil:
			return fmt.Errorf("%w: this task is not deleted", domain.ErrConflict)
		}
		runs, err := tx.Runs().ListByTask(ctx, t.ID)
		if err != nil {
			return err
		}
		for _, r := range runs {
			if r.State.Active() {
				return fmt.Errorf("%w: stop the active run before deleting this task", domain.ErrConflict)
			}
		}
		now := s.now()
		if deleting {
			t.DeletedAt = &now
		} else {
			t.DeletedAt = nil
		}
		t.UpdatedAt = now
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

// Erase removes a deleted task for good, without waiting out the trash: the person emptied it. Only a task that is already in the
// trash can be erased.
func (s *Tasks) Erase(ctx context.Context, projectID, id string, version int64) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		t, err := tx.Tasks().Get(ctx, id)
		if err != nil {
			return err
		}
		if t.ProjectID != projectID {
			return fmt.Errorf("task %s: %w", id, domain.ErrNotFound)
		}
		if t.Version != version {
			return fmt.Errorf("task %s is at version %d, not %d: %w", id, t.Version, version, domain.ErrConflict)
		}
		if t.DeletedAt == nil {
			return fmt.Errorf("%w: delete this task first; only a deleted task can be erased", domain.ErrConflict)
		}
		return eraseTask(ctx, tx, em, t)
	})
}

// eraseTask removes the task and takes it out of what other tasks wait for, so nothing is left waiting on something that is gone.
func eraseTask(ctx context.Context, tx store.Tx, em *emitter, t *domain.Task) error {
	siblings, err := tx.Tasks().ListByProject(ctx, t.ProjectID)
	if err != nil {
		return err
	}
	for _, o := range siblings {
		if o.ID == t.ID {
			continue
		}
		kept := make([]string, 0, len(o.Orchestration.Dependencies))
		for _, d := range o.Orchestration.Dependencies {
			if d != t.ID {
				kept = append(kept, d)
			}
		}
		if len(kept) == len(o.Orchestration.Dependencies) {
			continue
		}
		o.Orchestration.Dependencies = kept
		if err := tx.Tasks().Update(ctx, &o); err != nil {
			return err
		}
		ev := newEvent(domain.EventTaskUpdated, o)
		ev.ProjectID, ev.TaskID = o.ProjectID, o.ID
		if err := em.emit(ev); err != nil {
			return err
		}
	}
	if err := tx.Tasks().Delete(ctx, t.ID); err != nil {
		return err
	}
	ev := newEvent(domain.EventTaskDeleted, map[string]any{"id": t.ID})
	ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
	return em.emit(ev)
}

// ArchiveSettings reads a project's choice about finished work.
func (s *Tasks) ArchiveSettings(ctx context.Context, projectID string) (domain.ArchiveSettings, error) {
	var out domain.ArchiveSettings
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, e := tx.Projects().Get(ctx, projectID); e != nil {
			return e
		}
		e := tx.Settings().Get(ctx, domain.SettingArchivePrefix+projectID, &out)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		return e
	})
	return out, err
}

// SetArchiveSettings records it.
func (s *Tasks) SetArchiveSettings(ctx context.Context, projectID string, in domain.ArchiveSettings) (domain.ArchiveSettings, error) {
	if err := in.Validate(); err != nil {
		return in, err
	}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		p, e := tx.Projects().Get(ctx, projectID)
		if e != nil {
			return e
		}
		if p.Inactive() {
			return fmt.Errorf("%w: restore this project before changing it", domain.ErrConflict)
		}
		return tx.Settings().Set(ctx, domain.SettingArchivePrefix+projectID, in, s.now())
	})
	return in, err
}

// Housekeep does what the trash and the archive settings ask, and is safe to run as often as wanted: it archives Done tasks that
// have waited as long as their project says, and erases what has been in the trash for TrashRetention. A task with an active run is
// left alone. It returns how many tasks it archived and erased.
func (s *Tasks) Housekeep(ctx context.Context) (archived, erased int, err error) {
	now := s.now()
	var projects []domain.Project
	if err = s.Store.View(ctx, func(tx store.Tx) error {
		var e error
		projects, e = tx.Projects().List(ctx)
		return e
	}); err != nil {
		return 0, 0, err
	}
	for _, p := range projects {
		var settings domain.ArchiveSettings
		if err = s.Store.View(ctx, func(tx store.Tx) error {
			e := tx.Settings().Get(ctx, domain.SettingArchivePrefix+p.ID, &settings)
			if errors.Is(e, domain.ErrNotFound) {
				return nil
			}
			return e
		}); err != nil {
			return archived, erased, err
		}
		if settings.AfterDays <= 0 {
			continue
		}
		cutoff := now.Add(-time.Duration(settings.AfterDays) * 24 * time.Hour)
		n := 0
		err = s.update(ctx, func(tx store.Tx, em *emitter) error {
			n = 0
			tasks, e := tx.Tasks().ListByProject(ctx, p.ID)
			if e != nil {
				return e
			}
			for _, t := range tasks {
				if t.State != domain.TaskDone || t.ArchivedAt != nil || t.UpdatedAt.After(cutoff) {
					continue
				}
				if archiveTask(ctx, tx, &t, now) != nil {
					continue // it still has an active run: not this time
				}
				t.UpdatedAt = now
				if e := tx.Tasks().Update(ctx, &t); e != nil {
					return e
				}
				ev := newEvent(domain.EventTaskUpdated, t)
				ev.ProjectID, ev.TaskID = t.ProjectID, t.ID
				if e := em.emit(ev); e != nil {
					return e
				}
				n++
			}
			return nil
		})
		if err != nil {
			return archived, erased, err
		}
		archived += n
	}
	err = s.update(ctx, func(tx store.Tx, em *emitter) error {
		erased = 0
		due, e := tx.Tasks().ListTrashedBefore(ctx, now.Add(-domain.TrashRetention))
		if e != nil {
			return e
		}
		for i := range due {
			if e := eraseTask(ctx, tx, em, &due[i]); e != nil {
				return e
			}
			erased++
		}
		return nil
	})
	return archived, erased, err
}

// Erase removes a project that is in the trash, without waiting out the retention: the person emptied it. Only a deleted project can
// be erased; the repository on disk, its branches and its worktrees are not touched.
func (s *Projects) Erase(ctx context.Context, id string) error {
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		p, err := tx.Projects().Get(ctx, id)
		if err != nil {
			return err
		}
		if p.DeletedAt == nil {
			return fmt.Errorf("%w: delete this project first; only a deleted project can be erased", domain.ErrConflict)
		}
		return erasedProject(ctx, tx, em, p)
	})
}

func erasedProject(ctx context.Context, tx store.Tx, em *emitter, p *domain.Project) error {
	if err := tx.Projects().Purge(ctx, p.ID); err != nil {
		return err
	}
	ev := newEvent(domain.EventProjectUpdated, map[string]any{"id": p.ID, "lifecycle": "erased"})
	ev.ProjectID = p.ID
	return em.emit(ev)
}

// PurgeExpired erases the projects whose time in the trash is up.
func (s *Projects) PurgeExpired(ctx context.Context) (int, error) {
	n := 0
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		n = 0
		all, e := tx.Projects().ListHistory(ctx)
		if e != nil {
			return e
		}
		cutoff := s.now().Add(-domain.TrashRetention)
		for i := range all {
			if all[i].DeletedAt != nil && all[i].DeletedAt.Before(cutoff) {
				if e := erasedProject(ctx, tx, em, &all[i]); e != nil {
					return e
				}
				n++
			}
		}
		return nil
	})
	return n, err
}
