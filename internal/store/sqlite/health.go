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

type healthRepo struct{ q queryer }

// healthBody is what a finding keeps in its JSON column: everything a screen
// shows. What the database filters and orders on is in columns of its own.
type healthBody struct {
	Title       string                  `json:"title"`
	Explanation string                  `json:"explanation"`
	Subject     domain.HealthSubject    `json:"subject"`
	Evidence    []domain.HealthEvidence `json:"evidence"`
	Action      domain.HealthAction     `json:"action"`
}

const healthCols = `project_id, id, type, category, severity, basis, state, body, detected_at, updated_at, resolved_at, dismissed_at, dismissed_severity`

func scanFinding(s interface{ Scan(...any) error }) (*domain.HealthFinding, error) {
	var f domain.HealthFinding
	var body string
	var detected, updated int64
	var resolved, dismissed sql.NullInt64
	if err := s.Scan(&f.ProjectID, &f.ID, &f.Type, &f.Category, &f.Severity, &f.Basis, &f.State, &body,
		&detected, &updated, &resolved, &dismissed, &f.DismissedSeverity); err != nil {
		return nil, err
	}
	var b healthBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return nil, fmt.Errorf("health finding %s: %w", f.ID, err)
	}
	f.Title, f.Explanation, f.Subject, f.Evidence, f.Action = b.Title, b.Explanation, b.Subject, b.Evidence, b.Action
	if f.Evidence == nil {
		f.Evidence = []domain.HealthEvidence{}
	}
	f.DetectedAt, f.UpdatedAt = fromMS(detected), fromMS(updated)
	f.ResolvedAt, f.DismissedAt = fromNullMS(resolved), fromNullMS(dismissed)
	return &f, nil
}

func (r healthRepo) Upsert(ctx context.Context, f *domain.HealthFinding) error {
	if f.ID == "" || f.ProjectID == "" {
		return fmt.Errorf("%w: a finding needs an id and a project", domain.ErrInvalid)
	}
	if !f.Severity.Valid() || !f.State.Valid() {
		return fmt.Errorf("%w: finding %s has severity %q and state %q", domain.ErrInvalid, f.ID, f.Severity, f.State)
	}
	body, err := toJSON(healthBody{Title: f.Title, Explanation: f.Explanation, Subject: f.Subject, Evidence: f.Evidence, Action: f.Action})
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO health_findings (`+healthCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, id) DO UPDATE SET
			type = excluded.type, category = excluded.category, severity = excluded.severity, basis = excluded.basis,
			state = excluded.state, body = excluded.body, detected_at = excluded.detected_at, updated_at = excluded.updated_at,
			resolved_at = excluded.resolved_at, dismissed_at = excluded.dismissed_at, dismissed_severity = excluded.dismissed_severity`,
		f.ProjectID, f.ID, f.Type, f.Category, f.Severity, f.Basis, f.State, body,
		ms(f.DetectedAt), ms(f.UpdatedAt), nullMS(f.ResolvedAt), nullMS(f.DismissedAt), string(f.DismissedSeverity))
	switch {
	case isFKViolation(err):
		return fmt.Errorf("project %s: %w", f.ProjectID, domain.ErrNotFound)
	case err != nil && strings.Contains(err.Error(), "CHECK constraint failed"):
		return fmt.Errorf("%w: finding %s is inconsistent (state %s): %v", domain.ErrInvalid, f.ID, f.State, err)
	}
	return err
}

func (r healthRepo) Get(ctx context.Context, projectID, id string) (*domain.HealthFinding, error) {
	f, err := scanFinding(r.q.QueryRowContext(ctx, `SELECT `+healthCols+` FROM health_findings WHERE project_id = ? AND id = ?`, projectID, id))
	return f, notFound(err, "finding", id)
}

// severityOrder sorts worst first in SQL.
const severityOrder = `CASE severity WHEN 'critical' THEN 4 WHEN 'risk' THEN 3 WHEN 'attention' THEN 2 ELSE 1 END DESC, detected_at, id`

func (r healthRepo) ListByProject(ctx context.Context, projectID string, states ...domain.HealthState) ([]domain.HealthFinding, error) {
	q := `SELECT ` + healthCols + ` FROM health_findings WHERE project_id = ?`
	args := []any{projectID}
	if len(states) > 0 {
		marks := make([]string, len(states))
		for i, s := range states {
			marks[i] = "?"
			args = append(args, string(s))
		}
		q += ` AND state IN (` + strings.Join(marks, ",") + `)`
	}
	return r.list(ctx, q+` ORDER BY `+severityOrder, args...)
}

func (r healthRepo) ListOpen(ctx context.Context, min domain.HealthSeverity) ([]domain.HealthFinding, error) {
	var keep []string
	for _, s := range []domain.HealthSeverity{domain.HealthInfo, domain.HealthAttention, domain.HealthRisk, domain.HealthCritical} {
		if s.Rank() >= min.Rank() {
			keep = append(keep, "'"+string(s)+"'")
		}
	}
	return r.list(ctx, `SELECT `+healthCols+` FROM health_findings WHERE state = 'open' AND severity IN (`+strings.Join(keep, ",")+`) ORDER BY `+severityOrder)
}

func (r healthRepo) list(ctx context.Context, query string, args ...any) ([]domain.HealthFinding, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.HealthFinding{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (r healthRepo) DeleteResolvedBefore(ctx context.Context, t time.Time) (int, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM health_findings WHERE state = 'resolved' AND resolved_at < ?`, ms(t))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (r healthRepo) GetCheck(ctx context.Context, projectID string) (*domain.HealthCheck, error) {
	var c domain.HealthCheck
	var at int64
	err := r.q.QueryRowContext(ctx, `SELECT project_id, checked_at, duration_ms, error FROM health_checks WHERE project_id = ?`, projectID).
		Scan(&c.ProjectID, &at, &c.DurationMS, &c.Error)
	if err != nil {
		return nil, notFound(err, "health check of project", projectID)
	}
	c.CheckedAt = fromMS(at)
	return &c, nil
}

func (r healthRepo) SetCheck(ctx context.Context, c domain.HealthCheck) error {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO health_checks (project_id, checked_at, duration_ms, error) VALUES (?, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET checked_at = excluded.checked_at, duration_ms = excluded.duration_ms, error = excluded.error`,
		c.ProjectID, ms(c.CheckedAt), c.DurationMS, c.Error)
	if isFKViolation(err) {
		return fmt.Errorf("project %s: %w", c.ProjectID, domain.ErrNotFound)
	}
	return err
}
