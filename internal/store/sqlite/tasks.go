package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"devboard/internal/domain"
)

type taskRepo struct{ q queryer }

const taskCols = `id, project_id, title, description, state, position, version, created_at, updated_at, policy`

func scanTask(s interface{ Scan(...any) error }) (*domain.Task, error) {
	var t domain.Task
	var created, updated int64
	var policy string
	if err := s.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.State, &t.Position, &t.Version, &created, &updated, &policy); err != nil {
		return nil, err
	}
	t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
	var err error
	if t.Policy, err = decodePolicy(policy); err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	return &t, nil
}

func (r taskRepo) Create(ctx context.Context, t *domain.Task) error {
	if t.Version == 0 {
		t.Version = 1
	}
	policy, err := encodePolicy(t.Policy)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx,
		`INSERT INTO tasks (`+taskCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.Title, t.Description, t.State, t.Position, t.Version, ms(t.CreatedAt), ms(t.UpdatedAt), policy)
	if isFKViolation(err) {
		return fmt.Errorf("project %s: %w", t.ProjectID, domain.ErrNotFound)
	}
	return err
}

func (r taskRepo) Get(ctx context.Context, id string) (*domain.Task, error) {
	t, err := scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	return t, notFound(err, "task", id)
}

func (r taskRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Task, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE project_id = ? ORDER BY state, position, created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r taskRepo) Update(ctx context.Context, t *domain.Task) error {
	policy, err := encodePolicy(t.Policy)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE tasks SET title = ?, description = ?, state = ?, position = ?, policy = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`,
		t.Title, t.Description, t.State, t.Position, policy, ms(t.UpdatedAt), t.ID, t.Version)
	if err != nil {
		return err
	}
	if err := checkCAS(ctx, res, r.q, "tasks", t.ID, t.Version); err != nil {
		return err
	}
	t.Version++
	return nil
}

func (r taskRepo) MaxPosition(ctx context.Context, projectID string, state domain.TaskState) (float64, error) {
	var max sql.NullFloat64
	err := r.q.QueryRowContext(ctx,
		`SELECT MAX(position) FROM tasks WHERE project_id = ? AND state = ?`, projectID, state).Scan(&max)
	return max.Float64, err
}
