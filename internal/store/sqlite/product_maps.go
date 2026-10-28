package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
)

type productMapRepo struct{ q queryer }

func (r productMapRepo) Current(ctx context.Context, p string) (*integration.ProductMapSnapshot, error) {
	var raw string
	err := r.q.QueryRowContext(ctx, `SELECT snapshot FROM product_map_snapshots WHERE project_id=? ORDER BY version DESC LIMIT 1`, p).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out integration.ProductMapSnapshot
	err = json.Unmarshal([]byte(raw), &out)
	return &out, err
}
func (r productMapRepo) History(ctx context.Context, p string) ([]integration.ProductMapSnapshot, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT snapshot FROM product_map_snapshots WHERE project_id=? ORDER BY version DESC LIMIT 10`, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []integration.ProductMapSnapshot{}
	for rows.Next() {
		var raw string
		var s integration.ProductMapSnapshot
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r productMapRepo) Publish(ctx context.Context, p string, base int64, s *integration.ProductMapSnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var v int64
	if err := r.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM product_map_snapshots WHERE project_id=?`, p).Scan(&v); err != nil {
		return err
	}
	if v != base || s.Version != base+1 {
		return domain.ErrConflict
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO product_map_snapshots(project_id,version,snapshot) VALUES(?,?,?)`, p, s.Version, string(raw))
	return err
}
func (r productMapRepo) SaveJob(ctx context.Context, p, id string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO product_map_jobs(id,project_id,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body WHERE project_id=excluded.project_id`, id, p, string(raw))
	return err
}
func (r productMapRepo) Jobs(ctx context.Context, p string) ([]json.RawMessage, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT body FROM product_map_jobs WHERE project_id=? ORDER BY rowid DESC LIMIT 20`, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (r productMapRepo) InterruptJobs(ctx context.Context) error {
	_, err := r.q.ExecContext(ctx, `UPDATE product_map_jobs SET body=json_set(body,'$.state','interrupted','$.stage','interrupted','$.error','The controller stopped. Start a new analysis to retry.') WHERE json_extract(body,'$.state')='running'`)
	return err
}
