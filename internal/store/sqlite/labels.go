package sqlite

import (
	"context"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/planning"
)

type labelRepo struct{ q queryer }

const labelCols = `id, name, color, description, version, created_at, updated_at`

func scanLabel(s interface{ Scan(...any) error }) (*domain.Label, error) {
	var l domain.Label
	var created, updated int64
	if err := s.Scan(&l.ID, &l.Name, &l.Color, &l.Description, &l.Version, &created, &updated); err != nil {
		return nil, err
	}
	l.CreatedAt, l.UpdatedAt = fromMS(created), fromMS(updated)
	return &l, nil
}

func (r labelRepo) Create(ctx context.Context, l *domain.Label) error {
	if l.Version == 0 {
		l.Version = 1
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO labels (id, name, name_key, color, description, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.Name, planning.LabelKey(l.Name), l.Color, l.Description, l.Version, ms(l.CreatedAt), ms(l.UpdatedAt))
	if isUniqueViolation(err) {
		return fmt.Errorf("a label named %q: %w", l.Name, domain.ErrDuplicate)
	}
	return err
}

func (r labelRepo) Get(ctx context.Context, id string) (*domain.Label, error) {
	l, err := scanLabel(r.q.QueryRowContext(ctx, `SELECT `+labelCols+` FROM labels WHERE id = ?`, id))
	return l, notFound(err, "label", id)
}

func (r labelRepo) List(ctx context.Context) ([]domain.Label, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+labelCols+` FROM labels ORDER BY name_key, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r labelRepo) Update(ctx context.Context, l *domain.Label) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE labels SET name = ?, name_key = ?, color = ?, description = ?, updated_at = ?, version = version + 1 WHERE id = ? AND version = ?`,
		l.Name, planning.LabelKey(l.Name), l.Color, l.Description, ms(l.UpdatedAt), l.ID, l.Version)
	if isUniqueViolation(err) {
		return fmt.Errorf("a label named %q: %w", l.Name, domain.ErrDuplicate)
	}
	if err != nil {
		return err
	}
	if err := checkCAS(ctx, res, r.q, "labels", l.ID, l.Version); err != nil {
		return err
	}
	l.Version++
	return nil
}

func (r labelRepo) Delete(ctx context.Context, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM labels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("label %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r labelRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM labels`).Scan(&n)
	return n, err
}

func (r labelRepo) Usage(ctx context.Context) (map[string]int, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT tl.label_id, COUNT(*) FROM task_labels tl JOIN tasks t ON t.id = tl.task_id WHERE t.archived_at IS NULL GROUP BY tl.label_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (r labelRepo) TaskIDs(ctx context.Context, labelID string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT task_id FROM task_labels WHERE label_id = ? ORDER BY task_id`, labelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
