package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"devboard/internal/domain"
)

type runRepo struct{ q queryer }

const runCols = `id, task_id, project_id, agent_id, state, worktree_id, session_ref, reason, version, created_at, updated_at, ended_at`

func scanRun(s interface{ Scan(...any) error }) (*domain.Run, error) {
	var r domain.Run
	var worktree sql.NullString
	var created, updated int64
	var ended sql.NullInt64
	if err := s.Scan(&r.ID, &r.TaskID, &r.ProjectID, &r.AgentID, &r.State, &worktree, &r.SessionRef, &r.Reason,
		&r.Version, &created, &updated, &ended); err != nil {
		return nil, err
	}
	r.WorktreeID = worktree.String
	r.CreatedAt, r.UpdatedAt, r.EndedAt = fromMS(created), fromMS(updated), fromNullMS(ended)
	return &r, nil
}

func (q runRepo) list(ctx context.Context, query string, args ...any) ([]domain.Run, error) {
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (q runRepo) Create(ctx context.Context, r *domain.Run) error {
	if r.Version == 0 {
		r.Version = 1
	}
	_, err := q.q.ExecContext(ctx,
		`INSERT INTO runs (`+runCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.ProjectID, r.AgentID, r.State, nullString(r.WorktreeID), r.SessionRef, r.Reason,
		r.Version, ms(r.CreatedAt), ms(r.UpdatedAt), nullMS(r.EndedAt))
	if isFKViolation(err) {
		return fmt.Errorf("run %s references a missing task, project or worktree: %w", r.ID, domain.ErrNotFound)
	}
	return err
}

func (q runRepo) Get(ctx context.Context, id string) (*domain.Run, error) {
	r, err := scanRun(q.q.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = ?`, id))
	return r, notFound(err, "run", id)
}

func (q runRepo) Update(ctx context.Context, r *domain.Run) error {
	res, err := q.q.ExecContext(ctx, `
		UPDATE runs SET state = ?, worktree_id = ?, session_ref = ?, reason = ?, updated_at = ?, ended_at = ?, version = version + 1
		WHERE id = ? AND version = ?`,
		r.State, nullString(r.WorktreeID), r.SessionRef, r.Reason, ms(r.UpdatedAt), nullMS(r.EndedAt), r.ID, r.Version)
	if err != nil {
		return err
	}
	if err := checkCAS(ctx, res, q.q, "runs", r.ID, r.Version); err != nil {
		return err
	}
	r.Version++
	return nil
}

func (q runRepo) ListByTask(ctx context.Context, taskID string) ([]domain.Run, error) {
	return q.list(ctx, `SELECT `+runCols+` FROM runs WHERE task_id = ? ORDER BY created_at`, taskID)
}

func (q runRepo) ListActive(ctx context.Context) ([]domain.Run, error) {
	return q.list(ctx, `SELECT `+runCols+` FROM runs
		WHERE state IN ('starting', 'running', 'waiting_for_user') ORDER BY created_at`)
}

type questionRepo struct{ q queryer }

const questionCols = `id, run_id, prompt, options, status, answer, created_at, answered_at`

func scanQuestion(s interface{ Scan(...any) error }) (*domain.Question, error) {
	var qn domain.Question
	var options string
	var created int64
	var answered sql.NullInt64
	if err := s.Scan(&qn.ID, &qn.RunID, &qn.Prompt, &options, &qn.Status, &qn.Answer, &created, &answered); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(options), &qn.Options); err != nil {
		return nil, fmt.Errorf("decode options for question %s: %w", qn.ID, err)
	}
	qn.CreatedAt, qn.AnsweredAt = fromMS(created), fromNullMS(answered)
	return &qn, nil
}

func (q questionRepo) list(ctx context.Context, query string, args ...any) ([]domain.Question, error) {
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Question{}
	for rows.Next() {
		qn, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *qn)
	}
	return out, rows.Err()
}

func (q questionRepo) Create(ctx context.Context, qn *domain.Question) error {
	opts := qn.Options
	if opts == nil {
		opts = []string{}
	}
	oj, err := toJSON(opts)
	if err != nil {
		return err
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO questions (`+questionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		qn.ID, qn.RunID, qn.Prompt, oj, qn.Status, qn.Answer, ms(qn.CreatedAt), nullMS(qn.AnsweredAt))
	if isFKViolation(err) {
		return fmt.Errorf("run %s: %w", qn.RunID, domain.ErrNotFound)
	}
	return err
}

func (q questionRepo) Get(ctx context.Context, id string) (*domain.Question, error) {
	qn, err := scanQuestion(q.q.QueryRowContext(ctx, `SELECT `+questionCols+` FROM questions WHERE id = ?`, id))
	return qn, notFound(err, "question", id)
}

func (q questionRepo) Update(ctx context.Context, qn *domain.Question) error {
	res, err := q.q.ExecContext(ctx, `UPDATE questions SET status = ?, answer = ?, answered_at = ? WHERE id = ?`,
		qn.Status, qn.Answer, nullMS(qn.AnsweredAt), qn.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("question %s: %w", qn.ID, domain.ErrNotFound)
	}
	return nil
}

func (q questionRepo) ListPending(ctx context.Context) ([]domain.Question, error) {
	return q.list(ctx, `SELECT `+questionCols+` FROM questions WHERE status = 'pending' ORDER BY created_at`)
}

func (q questionRepo) ListByRun(ctx context.Context, runID string) ([]domain.Question, error) {
	return q.list(ctx, `SELECT `+questionCols+` FROM questions WHERE run_id = ? ORDER BY created_at`, runID)
}
