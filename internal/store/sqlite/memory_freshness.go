package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"devboard/internal/domain"
)

func (r memoryRepo) FreshnessScopes(ctx context.Context, after *domain.MemoryScope, limit int) ([]domain.MemoryScope, error) {
	return r.freshnessScopes(ctx, "", nil, after, limit)
}
func (r memoryRepo) FreshnessProjectScopes(ctx context.Context, project string, repositories []string, after *domain.MemoryScope, limit int) ([]domain.MemoryScope, error) {
	if project == "" || len(repositories) > 32 {
		return nil, domain.ErrInvalid
	}
	return r.freshnessScopes(ctx, project, repositories, after, limit)
}
func (r memoryRepo) freshnessScopes(ctx context.Context, project string, repositories []string, after *domain.MemoryScope, limit int) ([]domain.MemoryScope, error) {
	if limit < 1 || limit > 32 {
		return nil, domain.ErrInvalid
	}
	where := `(project_id<>'' OR repository<>'') AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.id=memories.project_id AND p.kind='work')`
	args := []any{}
	if project != "" {
		where += ` AND (project_id=?`
		args = append(args, project)
		if len(repositories) > 0 {
			where += ` OR (project_id='' AND repository IN (` + strings.TrimSuffix(strings.Repeat("?,", len(repositories)), ",") + `))`
			for _, repository := range repositories {
				args = append(args, repository)
			}
		}
		where += `)`
	}
	if after != nil {
		where += ` AND (owner_id,project_id,repository)>(?,?,?)`
		args = append(args, memoryScopeArgs(*after)...)
	}
	args = append(args, limit)
	rows, err := r.q.QueryContext(ctx, `SELECT DISTINCT owner_id,project_id,repository FROM memories WHERE `+where+` ORDER BY owner_id,project_id,repository LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MemoryScope{}
	for rows.Next() {
		var s domain.MemoryScope
		if err := rows.Scan(&s.OwnerID, &s.ProjectID, &s.Repository); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r memoryRepo) Freshness(ctx context.Context, scope domain.MemoryScope) (*domain.MemoryFreshness, error) {
	var raw string
	var version int64
	err := r.q.QueryRowContext(ctx, `SELECT checkpoint,version FROM memory_freshness WHERE `+memoryScopeWhere, memoryScopeArgs(scope)...).Scan(&raw, &version)
	if err != nil {
		return nil, notFound(err, "memory freshness", scope.ProjectID)
	}
	var out domain.MemoryFreshness
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	out.Scope, out.Version = scope, version
	return &out, nil
}
func (r memoryRepo) SaveFreshness(ctx context.Context, state *domain.MemoryFreshness) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return domain.ErrInvalid
	}
	if state.Version == 0 {
		_, err = r.q.ExecContext(ctx, `INSERT INTO memory_freshness(owner_id,project_id,repository,checkpoint,version) VALUES(?,?,?,?,1)`, state.Scope.OwnerID, state.Scope.ProjectID, state.Scope.Repository, string(raw))
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
	} else {
		args := []any{string(raw)}
		args = append(args, memoryScopeArgs(state.Scope)...)
		args = append(args, state.Version)
		res, e := r.q.ExecContext(ctx, `UPDATE memory_freshness SET checkpoint=?,version=version+1 WHERE `+memoryScopeWhere+` AND version=?`, args...)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return domain.ErrConflict
		}
	}
	if err == nil {
		state.Version++
	}
	return err
}

// Invalidation never changes evidence/content and must remain possible at the
// content quota. Keep its existing logical payload charge: the small fixed
// lifecycle/version overhead is bounded independently of content capacity.
func (r memoryRepo) Invalidate(ctx context.Context, scope domain.MemoryScope, id string, version int64, at time.Time) error {
	args := []any{ms(at)}
	args = append(args, memoryScopeArgs(scope)...)
	args = append(args, id, version)
	res, err := r.q.ExecContext(ctx, `UPDATE memories SET state='needs_verification',updated_at=max(updated_at,?),version=version+1 WHERE `+memoryScopeWhere+` AND id=? AND version=? AND state='current' AND authority NOT IN ('explicit_decision','approved_policy')`, args...)
	if err != nil {
		return memoryDBError(err)
	}
	return r.cas(ctx, res, scope, id)
}
