package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Worktrees records the worktrees the controller owns. It is the safety layer
// the worktree engine must go through, not the engine: it runs no Git command
// and creates or deletes nothing on disk. Its records are the only evidence
// that the controller owns a directory, and a directory is deleted on the
// strength of one, so a record is only written after checking that the path
// could never name something that was not the controller's to delete.
//
// The order of the engine's steps is what makes a crash safe:
//
//	creating:  Create, then make the directory.
//	removing:  BeginRemoval, then delete the directory, then FinishRemoval.
//
// A crash leaves either a record with no (or a partial) directory, which is
// harmless and can be retried, or never a directory nobody knows about.
// BeginRemoval is refused while a run uses the worktree and stops one
// starting, so "nothing is using it" cannot go stale before the deletion.
type Worktrees struct {
	Deps
	// Root is the only directory worktrees may be created in; the controller's
	// data directory is the expected choice. It must be absolute and clean. An
	// empty Root places nothing.
	Root string
}

// NewWorktree describes a worktree to record.
type NewWorktree struct {
	ProjectID string
	Path      string // absolute, clean, canonical (no symlinks), inside Root
	Branch    string
	BaseRef   string
}

// Create validates and records a worktree that is about to be created. The
// directory itself must not exist yet or must be empty; making it is the
// caller's job, after Create has returned.
func (s *Worktrees) Create(ctx context.Context, in NewWorktree) (*domain.Worktree, error) {
	now := s.now()
	w := &domain.Worktree{
		ID: domain.NewID(domain.PrefixWorktree), ProjectID: in.ProjectID, Path: in.Path,
		Branch: in.Branch, BaseRef: in.BaseRef, State: domain.WorktreeActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	if err := requireCanonical(w.Path); err != nil {
		return nil, err
	}
	root := ""
	if s.Root != "" {
		if err := domain.ValidateWorktreePath(s.Root); err != nil {
			return nil, fmt.Errorf("worktree directory is misconfigured: %w", err)
		}
		if err := requireCanonical(s.Root); err != nil {
			return nil, fmt.Errorf("worktree directory is misconfigured: %w", err)
		}
		root = s.Root
	}

	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if _, err := tx.Projects().Get(ctx, w.ProjectID); err != nil {
			return err
		}
		pl, err := s.placement(ctx, tx, root)
		if err != nil {
			return err
		}
		if err := pl.Check(w.Path); err != nil {
			return err
		}
		if err := tx.Worktrees().Create(ctx, w); err != nil {
			return err
		}
		return emitWorktree(em, domain.EventWorktreeCreated, w)
	})
	if err != nil {
		return nil, err
	}
	s.log().Info("worktree recorded", "worktree", w.ID, "project", w.ProjectID, "path", w.Path, "branch", w.Branch)
	return w, nil
}

// BeginRemoval records that the directory is about to be deleted. It returns
// domain.ErrConflict while a run is using the worktree, and from then on no
// run can start on it. Delete the directory only after this succeeds.
func (s *Worktrees) BeginRemoval(ctx context.Context, id string, version int64) (*domain.Worktree, error) {
	return s.step(ctx, id, version, domain.EventWorktreeRemoving, (*domain.Worktree).BeginRemoval)
}

// FinishRemoval records that the directory is gone. It is only possible after
// BeginRemoval.
func (s *Worktrees) FinishRemoval(ctx context.Context, id string, version int64) (*domain.Worktree, error) {
	return s.step(ctx, id, version, domain.EventWorktreeRemoved, (*domain.Worktree).FinishRemoval)
}

func (s *Worktrees) step(ctx context.Context, id string, version int64, ev domain.EventType, advance func(*domain.Worktree, time.Time) error) (*domain.Worktree, error) {
	var w *domain.Worktree
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		if w, err = tx.Worktrees().Get(ctx, id); err != nil {
			return err
		}
		if w.Version != version {
			return fmt.Errorf("worktree %s is at version %d, not %d: %w", id, w.Version, version, domain.ErrConflict)
		}
		if err := advance(w, s.now()); err != nil {
			return err
		}
		if err := tx.Worktrees().Update(ctx, w); err != nil {
			return err
		}
		return emitWorktree(em, ev, w)
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Get returns a worktree.
func (s *Worktrees) Get(ctx context.Context, id string) (*domain.Worktree, error) {
	var w *domain.Worktree
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		w, err = tx.Worktrees().Get(ctx, id)
		return err
	})
	return w, err
}

// GetIn returns a worktree of the given project; one of another project is
// reported as not found, as if it did not exist.
func (s *Worktrees) GetIn(ctx context.Context, projectID, id string) (*domain.Worktree, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if w.ProjectID != projectID {
		return nil, fmt.Errorf("worktree %s: %w", id, domain.ErrNotFound)
	}
	return w, nil
}

// ListByProject returns a project's worktrees, oldest first.
func (s *Worktrees) ListByProject(ctx context.Context, projectID string) ([]domain.Worktree, error) {
	var out []domain.Worktree
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Projects().Get(ctx, projectID); err != nil {
			return err
		}
		var err error
		out, err = tx.Worktrees().ListByProject(ctx, projectID)
		return err
	})
	return out, err
}

// placement gathers everything on disk a new worktree must not collide with:
// every registered repository (not only the new worktree's own project), its
// Git directory, and every worktree that is still on disk, including one whose
// removal has begun.
func (s *Worktrees) placement(ctx context.Context, tx store.Tx, root string) (domain.WorktreePlacement, error) {
	pl := domain.WorktreePlacement{Root: root}
	projects, err := tx.Projects().List(ctx)
	if err != nil {
		return pl, err
	}
	for _, p := range projects {
		pl.RepoRoots = append(pl.RepoRoots, p.RepoPath)
		repo, err := tx.Repositories().Get(ctx, p.ID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			return pl, err
		}
		if repo.RootPath != p.RepoPath {
			pl.RepoRoots = append(pl.RepoRoots, repo.RootPath)
		}
		if repo.CommonDir != "" {
			pl.GitDirs = append(pl.GitDirs, repo.CommonDir)
		}
	}
	active, err := tx.Worktrees().ListActive(ctx)
	if err != nil {
		return pl, err
	}
	for _, w := range active {
		pl.Worktrees = append(pl.Worktrees, w.Path)
	}
	return pl, nil
}

func emitWorktree(em *emitter, t domain.EventType, w *domain.Worktree) error {
	ev := newEvent(t, w)
	ev.ProjectID = w.ProjectID
	return em.emit(ev)
}

// requireCanonical rejects a path that passes through a symlink. Paths are
// compared as text everywhere else, and a symlink in the middle of one would
// let two different-looking paths name the same directory, or let a later
// deletion follow the link out of the worktree directory. The part of the path
// that does not exist yet cannot contain a link, so only the existing prefix is
// resolved.
func requireCanonical(path string) error {
	existing, rest := path, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return nil // nothing on the path exists, so nothing on it can be a link
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("%w: %s cannot be resolved: %v", domain.ErrInvalid, path, err)
	}
	if got := filepath.Join(real, rest); got != path {
		return fmt.Errorf("%w: %s is not canonical (it passes through a symlink); use %s", domain.ErrInvalid, path, got)
	}
	return nil
}
