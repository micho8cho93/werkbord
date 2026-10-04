package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"devboard/internal/domain"
)

type runRepo struct{ q queryer }

const runCols = `id, task_id, project_id, agent_id, state, worktree_id, session_ref, reason, prompt, waiting,
	activity, activity_at, exit_code, pid, process_id, version, created_at, updated_at, ended_at, policy, blocker, model, reasoning, parent_run_id, purpose, schedule_key, handoff, attempt, distribution`

// activeStates are the run states that still hold a session and a worktree.
// The database's indexes and triggers use the same list.
const activeStates = `('starting', 'running', 'waiting_for_user', 'blocked')`

func scanRun(s interface{ Scan(...any) error }) (*domain.Run, error) {
	var r domain.Run
	var worktree sql.NullString
	var created, updated int64
	var ended, activityAt, exitCode sql.NullInt64
	var policy, blocker, handoff, distribution string
	if err := s.Scan(&r.ID, &r.TaskID, &r.ProjectID, &r.AgentID, &r.State, &worktree, &r.SessionRef, &r.Reason,
		&r.Prompt, &r.Waiting, &r.Activity, &activityAt, &exitCode, &r.PID, &r.ProcessID,
		&r.Version, &created, &updated, &ended, &policy, &blocker, &r.Model, &r.Reasoning, &r.ParentRunID, &r.Purpose, &r.ScheduleKey, &handoff, &r.Attempt, &distribution); err != nil {
		return nil, err
	}
	var err error
	if r.Policy, err = decodePolicy(policy); err != nil {
		return nil, fmt.Errorf("run %s: %w", r.ID, err)
	}
	if blocker != "" {
		r.Blocker = &domain.Blocker{}
		if err := json.Unmarshal([]byte(blocker), r.Blocker); err != nil {
			return nil, fmt.Errorf("decode blocker of run %s: %w", r.ID, err)
		}
	}
	if err := json.Unmarshal([]byte(handoff), &r.Handoff); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(distribution), &r); err != nil {
		return nil, err
	}
	r.WorktreeID = worktree.String
	r.ActivityAt = fromNullMS(activityAt)
	if exitCode.Valid {
		c := int(exitCode.Int64)
		r.ExitCode = &c
	}
	r.CreatedAt, r.UpdatedAt, r.EndedAt = fromMS(created), fromMS(updated), fromNullMS(ended)
	return &r, nil
}

// encodeBlocker stores a blocker as JSON; no blocker is the empty string.
func encodeBlocker(b *domain.Blocker) (string, error) {
	if b == nil {
		return "", nil
	}
	return toJSON(b)
}

func nullInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
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
	policy, err := encodePolicy(r.Policy)
	if err != nil {
		return err
	}
	blocker, err := encodeBlocker(r.Blocker)
	if err != nil {
		return err
	}
	_, err = q.q.ExecContext(ctx,
		`INSERT INTO runs (`+runCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.ProjectID, r.AgentID, r.State, nullString(r.WorktreeID), r.SessionRef, r.Reason,
		r.Prompt, r.Waiting, r.Activity, nullMS(r.ActivityAt), nullInt(r.ExitCode), r.PID, r.ProcessID,
		r.Version, ms(r.CreatedAt), ms(r.UpdatedAt), nullMS(r.EndedAt), policy, blocker, r.Model, r.Reasoning, r.ParentRunID, r.Purpose, r.ScheduleKey, mustJSON(r.Handoff), r.Attempt, encodeDistribution(r))
	if isFKViolation(err) {
		return fmt.Errorf("run %s references a missing task, project or worktree: %w", r.ID, domain.ErrNotFound)
	}
	return runRule(err, r)
}

// runRule maps the database's rules about runs to domain errors.
func runRule(err error, r *domain.Run) error {
	switch {
	case isAbort(err, "waiting is set exactly"):
		return fmt.Errorf("run %s: state %s with waiting %q: %w", r.ID, r.State, r.Waiting, domain.ErrInvalid)
	case isAbort(err, "CHECK constraint failed") && r.State == domain.RunBlocked:
		return fmt.Errorf("run %s is blocked but records no blocker: %w", r.ID, domain.ErrInvalid)
	case isAbort(err, "an ended run has no process"):
		return fmt.Errorf("run %s has ended but still records process %d: %w", r.ID, r.PID, domain.ErrInvalid)
	case isAbort(err, "belong to different projects"):
		return fmt.Errorf("run %s (project %s) cannot use worktree %s: it belongs to another project: %w", r.ID, r.ProjectID, r.WorktreeID, domain.ErrInvalid)
	case isAbort(err, "removed or being removed"):
		return fmt.Errorf("run %s cannot be active on worktree %s: it is being removed or has been: %w", r.ID, r.WorktreeID, domain.ErrConflict)
	case isUniqueViolation(err) && strings.Contains(err.Error(), "runs.worktree_id"):
		return fmt.Errorf("worktree %s is already used by an active run: %w", r.WorktreeID, domain.ErrDuplicate)
	}
	return err
}

func (q runRepo) Get(ctx context.Context, id string) (*domain.Run, error) {
	r, err := scanRun(q.q.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = ?`, id))
	return r, notFound(err, "run", id)
}

func (q runRepo) Update(ctx context.Context, r *domain.Run) error {
	blocker, err := encodeBlocker(r.Blocker)
	if err != nil {
		return err
	}
	// The policy a run started with is fixed, so it is not written here.
	res, err := q.q.ExecContext(ctx, `
		UPDATE runs SET state = ?, worktree_id = ?, session_ref = ?, reason = ?, waiting = ?, activity = ?, activity_at = ?,
			exit_code = ?, pid = ?, process_id = ?, blocker = ?, handoff = ?, distribution = ?, updated_at = ?, ended_at = ?, version = version + 1
		WHERE id = ? AND version = ?`,
		r.State, nullString(r.WorktreeID), r.SessionRef, r.Reason, r.Waiting, r.Activity, nullMS(r.ActivityAt),
		nullInt(r.ExitCode), r.PID, r.ProcessID, blocker, mustJSON(r.Handoff), encodeDistribution(r), ms(r.UpdatedAt), nullMS(r.EndedAt), r.ID, r.Version)
	if err != nil {
		return runRule(err, r)
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

func (q runRepo) TouchActivity(ctx context.Context, id, activity string, at time.Time) error {
	_, err := q.q.ExecContext(ctx, `UPDATE runs SET activity = ?, activity_at = ?
		WHERE id = ? AND state IN `+activeStates, activity, ms(at), id)
	return err
}

func (q runRepo) ListLatestByProject(ctx context.Context, projectID string) ([]domain.Run, error) {
	return q.list(ctx, `SELECT `+runCols+` FROM runs r
		WHERE project_id = ? AND r.rowid = (
			SELECT r2.rowid FROM runs r2 WHERE r2.task_id = r.task_id ORDER BY r2.created_at DESC, r2.rowid DESC LIMIT 1)
		ORDER BY created_at`, projectID)
}

func (q runRepo) ListActive(ctx context.Context) ([]domain.Run, error) {
	return q.list(ctx, `SELECT `+runCols+` FROM runs WHERE state IN `+activeStates+` ORDER BY created_at, rowid`)
}

func (q runRepo) ListByProject(ctx context.Context, projectID string, limit int) ([]domain.Run, error) {
	return q.list(ctx, `SELECT `+runCols+` FROM runs WHERE project_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`, projectID, limit)
}

type questionRepo struct{ q queryer }

const questionCols = `id, run_id, task_id, project_id, kind, prompt, context, options, allow_free_text,
	state, answer, cancel_reason, asked_at, answered_at, delivered_at, closed_at, answered_by`

func scanQuestion(s interface{ Scan(...any) error }) (*domain.Question, error) {
	var qn domain.Question
	var options string
	var free int
	var asked int64
	var answered, delivered, closed sql.NullInt64
	if err := s.Scan(&qn.ID, &qn.RunID, &qn.TaskID, &qn.ProjectID, &qn.Kind, &qn.Prompt, &qn.Context, &options, &free,
		&qn.State, &qn.Answer, &qn.CancelReason, &asked, &answered, &delivered, &closed, &qn.AnsweredBy); err != nil {
		return nil, err
	}
	if qn.Answer == "" {
		qn.AnsweredBy = "" // the column defaults to 'user'; it only means something once there is an answer
	}
	if err := json.Unmarshal([]byte(options), &qn.Options); err != nil {
		return nil, fmt.Errorf("decode options for question %s: %w", qn.ID, err)
	}
	qn.AllowFreeText = free != 0
	qn.AskedAt, qn.AnsweredAt, qn.DeliveredAt, qn.ClosedAt = fromMS(asked), fromNullMS(answered), fromNullMS(delivered), fromNullMS(closed)
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
	free := 0
	if qn.AllowFreeText {
		free = 1
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO questions (`+questionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		qn.ID, qn.RunID, qn.TaskID, qn.ProjectID, qn.Kind, qn.Prompt, qn.Context, oj, free,
		qn.State, qn.Answer, qn.CancelReason, ms(qn.AskedAt), nullMS(qn.AnsweredAt), nullMS(qn.DeliveredAt), nullMS(qn.ClosedAt), answeredBy(qn))
	if isFKViolation(err) {
		return fmt.Errorf("run %s: %w", qn.RunID, domain.ErrNotFound)
	}
	return err
}

func (q questionRepo) Get(ctx context.Context, id string) (*domain.Question, error) {
	qn, err := scanQuestion(q.q.QueryRowContext(ctx, `SELECT `+questionCols+` FROM questions WHERE id = ?`, id))
	return qn, notFound(err, "question", id)
}

// Update writes the fields of a question that change as it is answered or
// cancelled. Who asked what is fixed once it is asked.
func (q questionRepo) Update(ctx context.Context, qn *domain.Question) error {
	res, err := q.q.ExecContext(ctx, `UPDATE questions SET state = ?, answer = ?, cancel_reason = ?,
		answered_at = ?, delivered_at = ?, closed_at = ?, answered_by = ? WHERE id = ?`,
		qn.State, qn.Answer, qn.CancelReason, nullMS(qn.AnsweredAt), nullMS(qn.DeliveredAt), nullMS(qn.ClosedAt), answeredBy(qn), qn.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("question %s: %w", qn.ID, domain.ErrNotFound)
	}
	return nil
}

// answeredBy is what to store for who answered: the user unless a policy did.
func answeredBy(qn *domain.Question) domain.AnsweredBy {
	if qn.AnsweredBy == domain.AnsweredByPolicy {
		return domain.AnsweredByPolicy
	}
	return domain.AnsweredByUser
}

func (q questionRepo) ListPendingByProject(ctx context.Context, projectID string) ([]domain.Question, error) {
	return q.list(ctx, `SELECT `+questionCols+` FROM questions WHERE project_id = ? AND state = 'pending' ORDER BY asked_at, rowid`, projectID)
}

func (q questionRepo) ListPending(ctx context.Context) ([]domain.Question, error) {
	return q.list(ctx, `SELECT `+questionCols+` FROM questions WHERE state = 'pending' ORDER BY asked_at, rowid`)
}

func (q questionRepo) ListByRun(ctx context.Context, runID string) ([]domain.Question, error) {
	return q.list(ctx, `SELECT `+questionCols+` FROM questions WHERE run_id = ? ORDER BY asked_at, rowid`, runID)
}

func encodeDistribution(r *domain.Run) string {
	return mustJSON(struct {
		RunnerID    string       `json:"runnerId"`
		Remote      bool         `json:"remote"`
		Branch      string       `json:"branch"`
		BaseCommit  string       `json:"baseCommit"`
		HeadCommit  string       `json:"headCommit"`
		Uncommitted *bool        `json:"uncommitted,omitempty"`
		Usage       domain.Usage `json:"usage"`
	}{r.RunnerID, r.Remote, r.Branch, r.BaseCommit, r.HeadCommit, r.Uncommitted, r.Usage})
}
