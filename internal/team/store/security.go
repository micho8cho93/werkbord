package store

import (
	"context"
	"time"
)

type LicenseRecord struct {
	WorkspaceID string
	Document    []byte
}

type SecurityQueries interface {
	LicenseRecords(context.Context) ([]LicenseRecord, error)
	UnlicensedWorkspaces(context.Context) ([]string, error)
	SetLicense(context.Context, string, []byte) error
	UseAPINonce(context.Context, string, string, string, time.Time, time.Time) (bool, error)
}

func (t *sqlTx) UnlicensedWorkspaces(ctx context.Context) ([]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT id FROM workspaces WHERE id NOT IN (SELECT workspace_id FROM workspace_licenses) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (t *sqlTx) LicenseRecords(ctx context.Context) ([]LicenseRecord, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT workspace_id, document FROM workspace_licenses ORDER BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LicenseRecord
	for rows.Next() {
		var r LicenseRecord
		var raw string
		if err := rows.Scan(&r.WorkspaceID, &raw); err != nil {
			return nil, err
		}
		r.Document = []byte(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *sqlTx) SetLicense(ctx context.Context, ws string, doc []byte) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO workspace_licenses(workspace_id, document) VALUES (?, ?) ON CONFLICT(workspace_id) DO UPDATE SET document = excluded.document`, ws, string(doc))
	return err
}

func (t *sqlTx) UseAPINonce(ctx context.Context, ws, dev, nonce string, expires, now time.Time) (bool, error) {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM api_replays WHERE expires_at <= ?`, ms(now)); err != nil {
		return false, err
	}
	// Capacity is per workspace; live entries are never evicted to make room.
	res, err := t.q.ExecContext(ctx, `INSERT OR IGNORE INTO api_replays(workspace_id, device_id, nonce, expires_at)
	 SELECT ?, ?, ?, ? WHERE (SELECT count(*) FROM api_replays WHERE workspace_id = ?) < 100000`, ws, dev, nonce, ms(expires), ws)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
