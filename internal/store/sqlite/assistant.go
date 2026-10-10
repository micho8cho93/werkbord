package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

type assistantRepo struct{ q queryer }

var _ store.AssistantRepo = assistantRepo{}

const sessionCols = `id, provider, model, reasoning, provider_ref, state, turns, last_error, reported_at, version, created_at, updated_at`

func scanSession(s interface{ Scan(...any) error }) (*domain.AssistantSession, error) {
	var a domain.AssistantSession
	var created, updated, reported int64
	if err := s.Scan(&a.ID, &a.Provider, &a.Model, &a.Reasoning, &a.ProviderRef, &a.State, &a.Turns, &a.LastError,
		&reported, &a.Version, &created, &updated); err != nil {
		return nil, err
	}
	a.CreatedAt, a.UpdatedAt, a.ReportedAt = fromMS(created), fromMS(updated), fromMS(reported)
	return &a, nil
}

func (r assistantRepo) CreateSession(ctx context.Context, s *domain.AssistantSession) error {
	if s.Version == 0 {
		s.Version = 1
	}
	_, err := r.q.ExecContext(ctx, `INSERT INTO assistant_sessions (`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Provider, s.Model, s.Reasoning, s.ProviderRef, string(s.State), s.Turns, s.LastError, ms(s.ReportedAt), s.Version,
		ms(s.CreatedAt), ms(s.UpdatedAt))
	return err
}

func (r assistantRepo) GetSession(ctx context.Context, id string) (*domain.AssistantSession, error) {
	s, err := scanSession(r.q.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM assistant_sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("assistant session %s: %w", id, domain.ErrNotFound)
	}
	return s, err
}

func (r assistantRepo) UpdateSession(ctx context.Context, s *domain.AssistantSession) error {
	res, err := r.q.ExecContext(ctx, `UPDATE assistant_sessions SET provider = ?, model = ?, reasoning = ?, provider_ref = ?,
		state = ?, turns = ?, last_error = ?, reported_at = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		s.Provider, s.Model, s.Reasoning, s.ProviderRef, string(s.State), s.Turns, s.LastError, ms(s.ReportedAt), ms(s.UpdatedAt), s.ID, s.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := r.GetSession(ctx, s.ID); err != nil {
			return err
		}
		return fmt.Errorf("assistant session %s changed since it was read: %w", s.ID, domain.ErrConflict)
	}
	s.Version++
	return nil
}

func (r assistantRepo) ListSessions(ctx context.Context) ([]domain.AssistantSession, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+sessionCols+` FROM assistant_sessions ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AssistantSession{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r assistantRepo) DeleteSession(ctx context.Context, id string) error {
	// The actions go with the session (foreign keys cascade); the audit, which names the session only by id, stays.
	if _, err := r.q.ExecContext(ctx, `DELETE FROM assistant_actions WHERE session_id = ?`, id); err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `DELETE FROM assistant_sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("assistant session %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

const actionCols = `id, session_id, principal, operation, args, args_hash, summary, state, outcome, created_at, expires_at, resolved_at`

func scanAction(s interface{ Scan(...any) error }) (*domain.AssistantAction, error) {
	var a domain.AssistantAction
	var args string
	var created, expires int64
	var resolved sql.NullInt64
	if err := s.Scan(&a.ID, &a.SessionID, &a.Principal, &a.Operation, &args, &a.ArgsHash, &a.Summary, &a.State, &a.Outcome,
		&created, &expires, &resolved); err != nil {
		return nil, err
	}
	a.Args = json.RawMessage(args)
	a.CreatedAt, a.ExpiresAt, a.ResolvedAt = fromMS(created), fromMS(expires), fromNullMS(resolved)
	return &a, nil
}

func (r assistantRepo) CreateAction(ctx context.Context, a *domain.AssistantAction) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO assistant_actions (`+actionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.SessionID, a.Principal, a.Operation, string(a.Args), a.ArgsHash, a.Summary, string(a.State), a.Outcome,
		ms(a.CreatedAt), ms(a.ExpiresAt), nullMS(a.ResolvedAt))
	return err
}

func (r assistantRepo) GetAction(ctx context.Context, id string) (*domain.AssistantAction, error) {
	a, err := scanAction(r.q.QueryRowContext(ctx, `SELECT `+actionCols+` FROM assistant_actions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("assistant action %s: %w", id, domain.ErrNotFound)
	}
	return a, err
}

func (r assistantRepo) TransitionAction(ctx context.Context, id string, from, to domain.AssistantActionState, outcome string, at time.Time) error {
	if !from.Open() || !to.Valid() || to == domain.ActionPending || from == to {
		return fmt.Errorf("%w: an action cannot move from %q to %q", domain.ErrInvalid, from, to)
	}
	res, err := r.q.ExecContext(ctx, `UPDATE assistant_actions SET state = ?, outcome = ?, resolved_at = ?
		WHERE id = ? AND state = ?`, string(to), outcome, nullMS(resolvedAt(to, at)), id, string(from))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		a, err := r.GetAction(ctx, id)
		if err != nil {
			return err
		}
		return fmt.Errorf("assistant action %s is %s, not %s: %w", id, a.State, from, domain.ErrConflict)
	}
	return nil
}

// resolvedAt is when an action reached its end; moving to executing is not an end.
func resolvedAt(to domain.AssistantActionState, at time.Time) *time.Time {
	if !to.Settled() {
		return nil
	}
	return &at
}

func (r assistantRepo) listActions(ctx context.Context, where string, args ...any) ([]domain.AssistantAction, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+actionCols+` FROM assistant_actions `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AssistantAction{}
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r assistantRepo) ListActions(ctx context.Context, sessionID string) ([]domain.AssistantAction, error) {
	return r.listActions(ctx, `WHERE session_id = ? ORDER BY created_at, id`, sessionID)
}

func (r assistantRepo) ListOpenActions(ctx context.Context) ([]domain.AssistantAction, error) {
	return r.listActions(ctx, `WHERE state IN ('pending', 'executing') ORDER BY created_at, id`)
}

const auditCols = `seq, at, session_id, action_id, actor, operation, kind, outcome, args_hash, detail, prev_hash, hash`

func scanAudit(s interface{ Scan(...any) error }) (*domain.AssistantAuditEntry, error) {
	var e domain.AssistantAuditEntry
	var at int64
	if err := s.Scan(&e.Seq, &at, &e.SessionID, &e.ActionID, &e.Actor, &e.Operation, &e.Kind, &e.Outcome, &e.ArgsHash,
		&e.Detail, &e.PrevHash, &e.Hash); err != nil {
		return nil, err
	}
	e.At = fromMS(at)
	return &e, nil
}

func (r assistantRepo) AppendAudit(ctx context.Context, e *domain.AssistantAuditEntry) error {
	// The write transaction holds the only writer, so the last hash read here is still the last when this row lands.
	var prev string
	err := r.q.QueryRowContext(ctx, `SELECT hash FROM assistant_audit ORDER BY seq DESC LIMIT 1`).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	e.At = time.UnixMilli(ms(e.At)).UTC() // the precision the database keeps, so the hash can be recomputed from it
	e.PrevHash = prev
	e.Hash = domain.AuditHash(*e)
	res, err := r.q.ExecContext(ctx, `INSERT INTO assistant_audit (at, session_id, action_id, actor, operation, kind, outcome,
		args_hash, detail, prev_hash, hash) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		ms(e.At), e.SessionID, e.ActionID, e.Actor, e.Operation, string(e.Kind), e.Outcome, e.ArgsHash, e.Detail, e.PrevHash, e.Hash)
	if err != nil {
		return err
	}
	e.Seq, err = res.LastInsertId()
	return err
}

func (r assistantRepo) ListAudit(ctx context.Context, sessionID string, before int64, limit int) ([]domain.AssistantAuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + auditCols + ` FROM assistant_audit WHERE 1 = 1`
	var args []any
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if before > 0 {
		q += ` AND seq < ?`
		args = append(args, before)
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)
	return r.scanAuditRows(ctx, q, args...)
}

func (r assistantRepo) AllAudit(ctx context.Context) ([]domain.AssistantAuditEntry, error) {
	return r.scanAuditRows(ctx, `SELECT `+auditCols+` FROM assistant_audit ORDER BY seq`)
}

func (r assistantRepo) scanAuditRows(ctx context.Context, q string, args ...any) ([]domain.AssistantAuditEntry, error) {
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AssistantAuditEntry{}
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
