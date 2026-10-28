package store

import (
	"context"
	"encoding/json"
	"werkbord/internal/team/domain"
)

func (t *sqlTx) ProductMaps(ctx context.Context, w, p string) ([]domain.ProductMapRecord, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM product_maps WHERE workspace_id=? AND project_id=? AND (state='proposed' OR id IN (SELECT id FROM product_maps WHERE workspace_id=? AND project_id=? AND state='published' ORDER BY version DESC LIMIT 5)) ORDER BY rowid DESC LIMIT 25`, w, p, w, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductMapRecord{}
	for rows.Next() {
		var raw string
		var r domain.ProductMapRecord
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (t *sqlTx) ProductMap(ctx context.Context, w, p, id string) (domain.ProductMapRecord, error) {
	var raw string
	var r domain.ProductMapRecord
	err := t.q.QueryRowContext(ctx, `SELECT document FROM product_maps WHERE workspace_id=? AND project_id=? AND id=?`, w, p, id).Scan(&raw)
	if err != nil {
		return r, notFound(err, "product map")
	}
	err = json.Unmarshal([]byte(raw), &r)
	return r, err
}
func (t *sqlTx) SaveProductMap(ctx context.Context, w, p string, r domain.ProductMapRecord, create bool) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if create {
		_, err = t.q.ExecContext(ctx, `INSERT INTO product_maps(workspace_id,project_id,id,state,version,document) VALUES(?,?,?,?,?,?)`, w, p, r.ID, r.State, r.Version, string(raw))
		if isUnique(err) {
			return domain.ErrConflict
		}
		return err
	}
	result, err := t.q.ExecContext(ctx, `UPDATE product_maps SET state=?,version=?,document=? WHERE workspace_id=? AND project_id=? AND id=? AND state='proposed'`, r.State, r.Version, string(raw), w, p, r.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
