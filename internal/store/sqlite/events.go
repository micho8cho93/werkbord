package sqlite

import (
	"context"
	"database/sql"
	"math"
	"time"

	"devboard/internal/domain"
)

type eventRepo struct{ q queryer }

func (r eventRepo) Append(ctx context.Context, e *domain.Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
	}
	var payload sql.NullString
	if len(e.Payload) > 0 {
		payload = sql.NullString{String: string(e.Payload), Valid: true}
	}
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO events (type, project_id, task_id, run_id, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.Type, nullString(e.ProjectID), nullString(e.TaskID), nullString(e.RunID), payload, ms(e.CreatedAt))
	if err != nil {
		return err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return err
	}
	e.Seq = seq
	return nil
}

func (r eventRepo) ListAfter(ctx context.Context, after int64, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 500
	}
	return r.list(ctx, `
		SELECT seq, type, project_id, task_id, run_id, payload, created_at
		FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
}

func (r eventRepo) ListByRun(ctx context.Context, runID string, before int64, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 500
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	out, err := r.list(ctx, `
		SELECT seq, type, project_id, task_id, run_id, payload, created_at FROM (
			SELECT * FROM events WHERE run_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?
		) ORDER BY seq`, runID, before, limit)
	return out, err
}

func (r eventRepo) list(ctx context.Context, query string, args ...any) ([]domain.Event, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		var project, task, run, payload sql.NullString
		var created int64
		if err := rows.Scan(&e.Seq, &e.Type, &project, &task, &run, &payload, &created); err != nil {
			return nil, err
		}
		e.ProjectID, e.TaskID, e.RunID = project.String, task.String, run.String
		if payload.Valid {
			e.Payload = []byte(payload.String)
		}
		e.CreatedAt = fromMS(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r eventRepo) LatestSeq(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	err := r.q.QueryRowContext(ctx, `SELECT MAX(seq) FROM events`).Scan(&seq)
	return seq.Int64, err
}
