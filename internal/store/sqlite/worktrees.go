package sqlite

import (
	"context"
	"fmt"

	"devboard/internal/domain"
)

type worktreeRepo struct{ q queryer }

const worktreeCols = `id, project_id, path, branch, base_ref, state, created_at, updated_at`

func scanWorktree(s interface{ Scan(...any) error }) (*domain.Worktree, error) {
	var w domain.Worktree
	var created, updated int64
	if err := s.Scan(&w.ID, &w.ProjectID, &w.Path, &w.Branch, &w.BaseRef, &w.State, &created, &updated); err != nil {
		return nil, err
	}
	w.CreatedAt, w.UpdatedAt = fromMS(created), fromMS(updated)
	return &w, nil
}

func (r worktreeRepo) Create(ctx context.Context, w *domain.Worktree) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO worktrees (`+worktreeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.Path, w.Branch, w.BaseRef, w.State, ms(w.CreatedAt), ms(w.UpdatedAt))
	switch {
	case isUniqueViolation(err):
		return fmt.Errorf("worktree at %s: %w", w.Path, domain.ErrDuplicate)
	case isFKViolation(err):
		return fmt.Errorf("project %s: %w", w.ProjectID, domain.ErrNotFound)
	}
	return err
}

func (r worktreeRepo) Get(ctx context.Context, id string) (*domain.Worktree, error) {
	w, err := scanWorktree(r.q.QueryRowContext(ctx, `SELECT `+worktreeCols+` FROM worktrees WHERE id = ?`, id))
	return w, notFound(err, "worktree", id)
}

func (r worktreeRepo) Update(ctx context.Context, w *domain.Worktree) error {
	res, err := r.q.ExecContext(ctx, `UPDATE worktrees SET branch = ?, base_ref = ?, state = ?, updated_at = ? WHERE id = ?`,
		w.Branch, w.BaseRef, w.State, ms(w.UpdatedAt), w.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("worktree %s: %w", w.ID, domain.ErrNotFound)
	}
	return nil
}

func (r worktreeRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Worktree, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+worktreeCols+` FROM worktrees WHERE project_id = ? ORDER BY created_at`, projectID)
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
