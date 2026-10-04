package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"devboard/internal/domain"
)

type worktreeRepo struct{ q queryer }

const worktreeCols = `id, project_id, path, branch, base_ref, state, removing_since, version, created_at, updated_at`

func scanWorktree(s interface{ Scan(...any) error }) (*domain.Worktree, error) {
	var w domain.Worktree
	var removing sql.NullInt64
	var created, updated int64
	if err := s.Scan(&w.ID, &w.ProjectID, &w.Path, &w.Branch, &w.BaseRef, &w.State, &removing, &w.Version, &created, &updated); err != nil {
		return nil, err
	}
	w.RemovingSince = fromNullMS(removing)
	w.CreatedAt, w.UpdatedAt = fromMS(created), fromMS(updated)
	return &w, nil
}

func (r worktreeRepo) Create(ctx context.Context, w *domain.Worktree) error {
	if w.Version == 0 {
		w.Version = 1
	}
	_, err := r.q.ExecContext(ctx, `INSERT INTO worktrees (`+worktreeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.Path, w.Branch, w.BaseRef, w.State, nullMS(w.RemovingSince), w.Version, ms(w.CreatedAt), ms(w.UpdatedAt))
	switch {
	case isAbort(err, "malformed worktree"):
		return fmt.Errorf("worktree at %q on %q from %q: the path must be absolute and clean, and the branch and base ref must be non-empty and not start with '-': %w",
			w.Path, w.Branch, w.BaseRef, domain.ErrInvalid)
	case isUniqueViolation(err) && strings.Contains(err.Error(), "worktrees.path"):
		return fmt.Errorf("worktree at %s: %w", w.Path, domain.ErrDuplicate)
	case isUniqueViolation(err):
		return fmt.Errorf("branch %q already has an active worktree in project %s: %w", w.Branch, w.ProjectID, domain.ErrDuplicate)
	case isFKViolation(err):
		return fmt.Errorf("project %s: %w", w.ProjectID, domain.ErrNotFound)
	}
	return err
}

func (r worktreeRepo) Get(ctx context.Context, id string) (*domain.Worktree, error) {
	w, err := scanWorktree(r.q.QueryRowContext(ctx, `SELECT `+worktreeCols+` FROM worktrees WHERE id = ?`, id))
	return w, notFound(err, "worktree", id)
}

// Update is compare-and-swap on Version and changes only the removal state
// (State, RemovingSince) and UpdatedAt: a worktree's path, branch and base ref
// are fixed at creation.
func (r worktreeRepo) Update(ctx context.Context, w *domain.Worktree) error {
	res, err := r.q.ExecContext(ctx, `
		UPDATE worktrees SET state = ?, removing_since = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`,
		w.State, nullMS(w.RemovingSince), ms(w.UpdatedAt), w.ID, w.Version)
	switch {
	case isAbort(err, "in use by an active run"):
		return fmt.Errorf("worktree %s is in use by an active run: %w", w.ID, domain.ErrConflict)
	case isAbort(err, "cannot become active again"):
		return fmt.Errorf("worktree %s was removed and cannot become active again: %w", w.ID, domain.ErrConflict)
	case isAbort(err, "removal has not begun"):
		return fmt.Errorf("worktree %s cannot be marked removed before its removal has begun: %w", w.ID, domain.ErrConflict)
	case isAbort(err, "removal cannot be cancelled"):
		return fmt.Errorf("worktree %s is being removed and that cannot be undone: %w", w.ID, domain.ErrConflict)
	case err != nil:
		return err
	}
	if err := checkCAS(ctx, res, r.q, "worktrees", w.ID, w.Version); err != nil {
		return err
	}
	w.Version++
	return nil
}

func (r worktreeRepo) ListActive(ctx context.Context) ([]domain.Worktree, error) {
	return r.list(ctx, `SELECT `+worktreeCols+` FROM worktrees WHERE state = 'active' ORDER BY created_at`)
}

func (r worktreeRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Worktree, error) {
	return r.list(ctx, `SELECT `+worktreeCols+` FROM worktrees WHERE project_id = ? ORDER BY created_at`, projectID)
}

func (r worktreeRepo) list(ctx context.Context, query string, args ...any) ([]domain.Worktree, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Worktree{}
	for rows.Next() {
		w, err := scanWorktree(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}
