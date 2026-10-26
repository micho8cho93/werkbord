package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

type memoryRepo struct{ q queryer }

var _ store.MemoryRepo = memoryRepo{}

const memoryCols = `id,owner_id,project_id,repository,type,authority,source_type,provenance,summary,details,state,sensitivity,sharing_eligible,superseded_by,duplicate_of,fingerprint,schema_version,version,created_at,updated_at,verified_at,record_bytes,pinned`
const memoryScopeWhere = `owner_id=? AND project_id=? AND repository=?`

func memoryScopeArgs(s domain.MemoryScope) []any { return []any{s.OwnerID, s.ProjectID, s.Repository} }

func scanMemory(s interface{ Scan(...any) error }) (*domain.Memory, error) {
	var m domain.Memory
	var sourceType, provenance, details string
	var superseded, duplicate sql.NullString
	var created, updated, size int64
	var verified sql.NullInt64
	if err := s.Scan(&m.ID, &m.Scope.OwnerID, &m.Scope.ProjectID, &m.Scope.Repository, &m.Type, &m.Authority, &sourceType, &provenance, &m.Summary, &details, &m.State, &m.Sensitivity, &m.SharingEligible, &superseded, &duplicate, &m.Fingerprint, &m.SchemaVersion, &m.Version, &created, &updated, &verified, &size, &m.Pinned); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(provenance), &m.Source); err != nil {
		return nil, err
	}
	if string(m.Source.Type) != sourceType {
		return nil, fmt.Errorf("memory source metadata mismatch")
	}
	m.Details = json.RawMessage(details)
	m.SupersededBy = superseded.String
	m.DuplicateOf = duplicate.String
	m.CreatedAt = fromMS(created)
	m.UpdatedAt = fromMS(updated)
	m.VerifiedAt = fromNullMS(verified)
	return &m, nil
}
func memorySize(m *domain.Memory) (int, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	if len(raw) > domain.MaxMemoryRecordBytes {
		return 0, fmt.Errorf("%w: memory record exceeds size limit", domain.ErrInvalid)
	}
	return len(raw), nil
}
func memoryDBError(err error) error {
	if isAbort(err, "memory evidence owner mismatch") {
		return fmt.Errorf("%w: evidence owner mismatch", domain.ErrForbidden)
	}
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: memory identity or evidence already exists", domain.ErrDuplicate)
	}
	if isFKViolation(err) {
		return fmt.Errorf("%w: memory reference missing or still in use", domain.ErrConflict)
	}
	if isAbort(err, "memory capacity exceeded") {
		return fmt.Errorf("%w: memory capacity exceeded", domain.ErrConflict)
	}
	return err
}
func (r memoryRepo) Create(ctx context.Context, m *domain.Memory) error {
	size, err := memorySize(m)
	if err != nil {
		return err
	}
	details := string(m.Details)
	if details == "" {
		details = "{}"
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO memories (`+memoryCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.Scope.OwnerID, m.Scope.ProjectID, m.Scope.Repository, m.Type, m.Authority, m.Source.Type, mustJSON(m.Source), m.Summary, details, m.State, m.Sensitivity, m.SharingEligible, nullString(m.SupersededBy), nullString(m.DuplicateOf), m.Fingerprint, m.SchemaVersion, m.Version, ms(m.CreatedAt), ms(m.UpdatedAt), nullMS(m.VerifiedAt), size, m.Pinned)
	return memoryDBError(err)
}
func (r memoryRepo) Get(ctx context.Context, s domain.MemoryScope, id string) (*domain.Memory, error) {
	args := append(memoryScopeArgs(s), id)
	m, err := scanMemory(r.q.QueryRowContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE `+memoryScopeWhere+` AND id=?`, args...))
	return m, notFound(err, "memory", id)
}
func (r memoryRepo) FindFingerprint(ctx context.Context, s domain.MemoryScope, f string) (*domain.Memory, error) {
	args := append(memoryScopeArgs(s), f)
	m, err := scanMemory(r.q.QueryRowContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE `+memoryScopeWhere+` AND fingerprint=? AND duplicate_of IS NULL`, args...))
	return m, notFound(err, "memory fingerprint", f)
}
func (r memoryRepo) List(ctx context.Context, s domain.MemoryScope, in domain.MemoryQuery) (domain.MemoryPage, error) {
	out := domain.MemoryPage{Records: []domain.Memory{}}
	if err := domain.ValidateMemoryQuery(in); err != nil {
		return out, err
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	where := memoryScopeWhere
	args := memoryScopeArgs(s)
	if in.Type != "" {
		where += ` AND type=?`
		args = append(args, in.Type)
	}
	if in.Authority != "" {
		where += ` AND authority=?`
		args = append(args, in.Authority)
	}
	if in.State != "" {
		where += ` AND state=?`
		args = append(args, in.State)
	}
	if in.After != nil {
		where += ` AND (created_at<? OR (created_at=? AND id<?))`
		args = append(args, ms(in.After.CreatedAt), ms(in.After.CreatedAt), in.After.ID)
	}
	if in.Text != "" {
		var terms []string
		for _, s := range strings.Fields(in.Text) {
			terms = append(terms, `"`+strings.ReplaceAll(s, `"`, `""`)+`"`)
		}
		where += ` AND rowid IN (SELECT rowid FROM memories_fts WHERE memories_fts MATCH ?)`
		args = append(args, strings.Join(terms, " AND "))
	}
	args = append(args, in.Limit+1)
	rows, err := r.q.QueryContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return out, err
		}
		out.Records = append(out.Records, *m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Records) > in.Limit {
		out.Records = out.Records[:in.Limit]
		last := out.Records[in.Limit-1]
		out.Next = &domain.MemoryCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out, nil
}
func (r memoryRepo) Update(ctx context.Context, m *domain.Memory) error {
	next := *m
	next.Version++
	size, err := memorySize(&next)
	if err != nil {
		return err
	}
	details := string(m.Details)
	if details == "" {
		details = "{}"
	}
	args := []any{mustJSON(m.Source), m.Summary, details, m.State, m.Sensitivity, m.SharingEligible, nullString(m.SupersededBy), nullString(m.DuplicateOf), m.Fingerprint, ms(m.UpdatedAt), nullMS(m.VerifiedAt), size, m.Pinned}
	args = append(args, memoryScopeArgs(m.Scope)...)
	args = append(args, m.ID, m.Version, m.Type, m.Authority, m.Source.Type, m.SchemaVersion, ms(m.CreatedAt))
	res, err := r.q.ExecContext(ctx, `UPDATE memories SET provenance=?,summary=?,details=?,state=?,sensitivity=?,sharing_eligible=?,superseded_by=?,duplicate_of=?,fingerprint=?,updated_at=?,verified_at=?,record_bytes=?,pinned=?,version=version+1 WHERE `+memoryScopeWhere+` AND id=? AND version=? AND type=? AND authority=? AND source_type=? AND schema_version=? AND created_at=?`, args...)
	if err != nil {
		return memoryDBError(err)
	}
	if err := r.cas(ctx, res, m.Scope, m.ID); err != nil {
		return err
	}
	m.Version++
	return nil
}
func (r memoryRepo) cas(ctx context.Context, res sql.Result, s domain.MemoryScope, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	if _, err := r.Get(ctx, s, id); err != nil {
		return err
	}
	return fmt.Errorf("%w: memory changed since it was read", domain.ErrConflict)
}
func (r memoryRepo) Delete(ctx context.Context, s domain.MemoryScope, id string, version int64) error {
	args := append(memoryScopeArgs(s), id, version)
	res, err := r.q.ExecContext(ctx, `DELETE FROM memories WHERE `+memoryScopeWhere+` AND id=? AND version=?`, args...)
	if err != nil {
		return memoryDBError(err)
	}
	if err := r.cas(ctx, res, s, id); err != nil {
		return err
	}
	return r.purgeIndex(ctx)
}

// purgeIndex merges the full-text index after a deletion. FTS5 only marks a deleted
// row in its segments; the words stay readable there until the segments are merged.
// Together with secure_delete this leaves no readable copy of the deleted summary.
func (r memoryRepo) purgeIndex(ctx context.Context) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts) VALUES('optimize')`)
	return err
}
func (r memoryRepo) Usage(ctx context.Context, owner string) (domain.MemoryUsage, error) {
	var u domain.MemoryUsage
	var err error
	if owner == "" {
		err = r.q.QueryRowContext(ctx, `SELECT records,bytes FROM memory_usage WHERE singleton=1`).Scan(&u.Records, &u.Bytes)
	} else {
		err = r.q.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(record_bytes),0) FROM memories WHERE owner_id=?`, owner).Scan(&u.Records, &u.Bytes)
	}
	return u, err
}
func (r memoryRepo) Prune(ctx context.Context, owner string, before time.Time, limit int) (int, error) {
	if owner == "" || limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("%w: invalid memory prune bounds", domain.ErrInvalid)
	}
	res, err := r.q.ExecContext(ctx, `DELETE FROM memories WHERE id IN (
 SELECT m.id FROM memories m WHERE m.owner_id=? AND m.state IN ('historical','superseded') AND m.pinned=0 AND m.updated_at<?
 AND NOT EXISTS (SELECT 1 FROM memories ref WHERE ref.owner_id=m.owner_id AND ref.project_id=m.project_id AND ref.repository=m.repository AND (ref.superseded_by=m.id OR ref.duplicate_of=m.id OR json_extract(ref.details,'$.corrects')=m.id))
 ORDER BY m.updated_at,m.id LIMIT ?)`, owner, ms(before), limit)
	if err != nil {
		return 0, memoryDBError(err)
	}
	n, err := res.RowsAffected()
	if err == nil && n > 0 {
		err = r.purgeIndex(ctx)
	}
	return int(n), err
}
