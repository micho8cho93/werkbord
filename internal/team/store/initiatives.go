package store

import (
	"context"
	"encoding/json"
	"werkbord/internal/team/domain"
)

func (t *sqlTx) Initiatives(ctx context.Context, workspace string) ([]domain.Initiative, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM initiatives WHERE workspace_id=? ORDER BY id LIMIT 1001`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Initiative{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var i domain.Initiative
		if err := json.Unmarshal([]byte(raw), &i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (t *sqlTx) Initiative(ctx context.Context, workspace, id string) (domain.Initiative, error) {
	var raw string
	var i domain.Initiative
	err := t.q.QueryRowContext(ctx, `SELECT document FROM initiatives WHERE workspace_id=? AND id=?`, workspace, id).Scan(&raw)
	if err != nil {
		return i, notFound(err, "initiative")
	}
	err = json.Unmarshal([]byte(raw), &i)
	return i, err
}
func (t *sqlTx) SaveInitiative(ctx context.Context, workspace string, i domain.Initiative, expected int64) error {
	raw, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = t.q.ExecContext(ctx, `INSERT INTO initiatives(workspace_id,id,version,document) VALUES(?,?,?,?)`, workspace, i.ID, i.Version, string(raw))
		if isUnique(err) {
			return domain.ErrConflict
		}
		return err
	}
	res, err := t.q.ExecContext(ctx, `UPDATE initiatives SET version=?,document=? WHERE workspace_id=? AND id=? AND version=?`, i.Version, string(raw), workspace, i.ID, expected)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
