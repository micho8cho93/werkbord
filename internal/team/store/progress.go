package store

import (
	"context"
	"devboard/internal/team/domain"
	"encoding/json"
)

func (t *sqlTx) Progress(ctx context.Context, w, p, k, d string) (domain.ProgressRecord, error) {
	var raw string
	err := t.q.QueryRowContext(ctx, `SELECT document FROM ticket_progress WHERE workspace_id=? AND project_id=? AND ticket_id=? AND device_id=?`, w, p, k, d).Scan(&raw)
	var out domain.ProgressRecord
	if err != nil {
		return out, notFound(err, "progress")
	}
	err = json.Unmarshal([]byte(raw), &out)
	// Digest is deliberately private; persist it separately from public JSON.
	if err == nil {
		err = t.q.QueryRowContext(ctx, `SELECT digest FROM ticket_progress WHERE workspace_id=? AND project_id=? AND ticket_id=? AND device_id=?`, w, p, k, d).Scan(&out.Digest)
	}
	return out, err
}
func (t *sqlTx) SaveProgress(ctx context.Context, w, p, k string, r domain.ProgressRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO ticket_progress (workspace_id,project_id,ticket_id,device_id,member_id,sequence,claim_at,digest,document,reported_at) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,project_id,ticket_id,device_id) DO UPDATE SET member_id=excluded.member_id,sequence=excluded.sequence,claim_at=excluded.claim_at,digest=excluded.digest,document=excluded.document,reported_at=excluded.reported_at`, w, p, k, r.DeviceID, r.MemberID, r.Sequence, ms(r.ClaimAt), r.Digest, string(b), ms(r.ReportedAt))
	return err
}
func (t *sqlTx) TicketProgress(ctx context.Context, w, p, k string) ([]domain.ProgressRecord, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM ticket_progress WHERE workspace_id=? AND project_id=? AND ticket_id=? ORDER BY device_id`, w, p, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProgressRecord{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r domain.ProgressRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
