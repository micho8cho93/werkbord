package store

import (
	"context"
	"devboard/internal/team/domain"
	"encoding/json"
)

func (t *sqlTx) Schedule(ctx context.Context, w, p, k string) (domain.Schedule, error) {
	var raw string
	var out domain.Schedule
	err := t.q.QueryRowContext(ctx, `SELECT document FROM ticket_schedules WHERE workspace_id=? AND project_id=? AND ticket_id=?`, w, p, k).Scan(&raw)
	if err != nil {
		return out, notFound(err, "schedule")
	}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}
func (t *sqlTx) Schedules(ctx context.Context, w, p string) ([]domain.Schedule, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM ticket_schedules WHERE workspace_id=? AND project_id=? ORDER BY ticket_id`, w, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Schedule{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var s domain.Schedule
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (t *sqlTx) SaveSchedule(ctx context.Context, w string, s domain.Schedule, previous int64) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if previous == 0 {
		_, err = t.q.ExecContext(ctx, `INSERT INTO ticket_schedules(workspace_id,project_id,ticket_id,version,document) VALUES(?,?,?,?,?)`, w, s.ProjectID, s.TicketID, s.Version, string(raw))
		if isUnique(err) {
			return domain.ErrConflict
		}
		return err
	}
	res, err := t.q.ExecContext(ctx, `UPDATE ticket_schedules SET version=?,document=? WHERE workspace_id=? AND project_id=? AND ticket_id=? AND version=?`, s.Version, string(raw), w, s.ProjectID, s.TicketID, previous)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
