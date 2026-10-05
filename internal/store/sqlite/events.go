package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
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

func (r eventRepo) ListAfterProject(ctx context.Context, projectID string, after int64, limit int) ([]domain.Event, error) {
	if projectID == "" {
		return r.ListAfter(ctx, after, limit)
	}
	if limit <= 0 {
		limit = 500
	}
	return r.list(ctx, `SELECT seq,type,project_id,task_id,run_id,payload,created_at FROM events WHERE project_id=? AND seq>? ORDER BY seq LIMIT ?`, projectID, after, limit)
}
func (r eventRepo) ReplayFloor(ctx context.Context) (int64, error) {
	var floor int64
	err := (settingsRepo{q: r.q}).Get(ctx, "events:replay-floor", &floor)
	if errors.Is(err, domain.ErrNotFound) {
		return 0, nil
	}
	return floor, err
}
func (r eventRepo) Prune(ctx context.Context, now time.Time) (int, error) {
	// Keep lifecycle/audit evidence for a year; detailed terminal transcripts for
	// thirty days. Stored runs and handoffs remain available independently.
	rows, err := r.q.QueryContext(ctx, `SELECT e.seq FROM events e JOIN runs r ON r.id=e.run_id WHERE r.state IN ('completed','failed','stopped') AND ((e.type=? AND e.created_at<?) OR e.created_at<?) ORDER BY e.created_at LIMIT 10000`, domain.EventAgentOutput, ms(now.Add(-30*24*time.Hour)), ms(now.Add(-365*24*time.Hour)))
	if err != nil {
		return 0, err
	}
	ids := []any{}
	floor, err := r.ReplayFloor(ctx)
	if err != nil {
		rows.Close()
		return 0, err
	}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, seq)
		if seq > floor {
			floor = seq
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	if _, err := r.q.ExecContext(ctx, "DELETE FROM events WHERE seq IN ("+marks+")", ids...); err != nil {
		return 0, err
	}
	return len(ids), (settingsRepo{q: r.q}).Set(ctx, "events:replay-floor", floor, now)
}
