package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"devboard/internal/domain"
)

func (r memoryRepo) KnowledgeRuns(ctx context.Context, owner, after string, limit int) ([]domain.Run, error) {
	if limit < 1 || limit > 8 {
		return nil, domain.ErrInvalid
	}
	// Only IDs are loaded here; run prompts/transcripts never enter the extractor.
	rows, err := r.q.QueryContext(ctx, `SELECT r.id FROM runs r LEFT JOIN memory_extractions e ON e.run_id=r.id AND e.owner_id=? WHERE r.id>? AND r.state IN ('completed','failed','stopped') AND (e.version IS NULL OR e.version<>r.version) ORDER BY r.id LIMIT ?`, owner, after, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []domain.Run{}
	for _, id := range ids {
		v, e := (runRepo{r.q}).Get(ctx, id)
		if e != nil {
			return nil, e
		}
		v.Prompt = ""
		out = append(out, *v)
	}
	return out, nil
}
func (r memoryRepo) MarkExtracted(ctx context.Context, owner, id string, version int64) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO memory_extractions(owner_id,run_id,version) VALUES(?,?,?) ON CONFLICT(owner_id,run_id) DO UPDATE SET version=excluded.version`, owner, id, version)
	return err
}
func (r memoryRepo) EvidenceSeen(ctx context.Context, owner, key string) (bool, error) {
	var n int
	err := r.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_evidence WHERE owner_id=? AND source_key=?`, owner, key).Scan(&n)
	return n != 0, err
}
func (r memoryRepo) AddEvidence(ctx context.Context, e domain.MemoryEvidence) error {
	raw, err := json.Marshal(e.Source)
	if err != nil {
		return err
	}
	if len(raw) > 4096 || len(e.Excerpt) > 1024 || len(e.Key) != 64 {
		return domain.ErrInvalid
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO memory_evidence(owner_id,source_key,memory_id,provenance,excerpt,observed_at) VALUES(?,?,?,?,?,?)`, e.OwnerID, e.Key, e.MemoryID, string(raw), e.Excerpt, ms(e.ObservedAt))
	return memoryDBError(err)
}
func (r memoryRepo) Evidence(ctx context.Context, s domain.MemoryScope, id string, limit int) ([]domain.MemoryEvidence, error) {
	if limit < 1 || limit > 64 {
		return nil, domain.ErrInvalid
	}
	if _, err := r.Get(ctx, s, id); err != nil {
		return nil, err
	}
	rows, err := r.q.QueryContext(ctx, `SELECT source_key,provenance,excerpt,observed_at FROM memory_evidence WHERE owner_id=? AND memory_id=? ORDER BY observed_at DESC,source_key LIMIT ?`, s.OwnerID, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MemoryEvidence{}
	for rows.Next() {
		e := domain.MemoryEvidence{OwnerID: s.OwnerID, MemoryID: id}
		var raw string
		var at int64
		if err = rows.Scan(&e.Key, &raw, &e.Excerpt, &at); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &e.Source); err != nil {
			return nil, err
		}
		e.ObservedAt = fromMS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (r memoryRepo) EvidenceUsage(ctx context.Context, owner string) (int64, error) {
	var n int64
	err := r.q.QueryRowContext(ctx, `SELECT coalesce(sum(length(CAST(provenance AS BLOB))+length(CAST(excerpt AS BLOB))+128),0) FROM memory_evidence WHERE owner_id=?`, owner).Scan(&n)
	return n, err
}

// KnowledgeList is project-scoped, never owner-wide. SQL ranks the complete
// matching set before the bounded result, so old pinned/verified records survive.
func (r memoryRepo) KnowledgeList(ctx context.Context, s domain.MemoryScope, q domain.MemoryQuery, retrieval bool) ([]domain.Memory, error) {
	if err := domain.ValidateMemoryQuery(q); err != nil {
		return nil, err
	}
	where := `owner_id=? AND project_id=?`
	args := []any{s.OwnerID, s.ProjectID}
	if s.Repository != "" {
		where += ` AND repository=?`
		args = append(args, s.Repository)
	}
	if q.Type != "" {
		where += ` AND type=?`
		args = append(args, q.Type)
	}
	if q.Authority != "" {
		where += ` AND authority=?`
		args = append(args, q.Authority)
	}
	if q.State != "" {
		where += ` AND state=?`
		args = append(args, q.State)
	}
	if retrieval {
		if q.IncludeCandidates {
			where += ` AND state IN ('current','needs_verification')`
		} else {
			where += ` AND state='current'`
		}
		where += ` AND duplicate_of IS NULL AND sensitivity='private'`
	}
	order := `pinned DESC,(verified_at IS NOT NULL) DESC,(source_type='user') DESC,updated_at DESC,id DESC`
	if q.Text != "" {
		terms := []string{}
		for _, term := range strings.Fields(q.Text) {
			terms = append(terms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
		}
		match := strings.Join(terms, " AND ")
		where += ` AND rowid IN (SELECT rowid FROM memories_fts WHERE memories_fts MATCH ?)`
		args = append(args, match)
		// bm25 uses a bound literal FTS query, never caller SQL.
		order = `pinned DESC,(verified_at IS NOT NULL) DESC,(source_type='user') DESC,(SELECT bm25(memories_fts) FROM memories_fts WHERE memories_fts.rowid=memories.rowid AND memories_fts MATCH ?) ASC,updated_at DESC,id DESC`
		args = append(args, match)
	}
	if q.PrioritizeAuthority {
		order = `(verified_at IS NOT NULL) DESC,CASE authority WHEN 'approved_policy' THEN 4 WHEN 'explicit_decision' THEN 3 WHEN 'factual_observation' THEN 2 ELSE 1 END DESC,` + order
	}
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := r.q.QueryContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE `+where+` ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()
	out := []domain.Memory{}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r memoryRepo) KnowledgeMatch(ctx context.Context, s domain.MemoryScope, c domain.MemoryContent) (*domain.Memory, error) {
	args := append(memoryScopeArgs(s), c.Type, c.Authority, c.Source.Type, c.Summary)
	m, err := scanMemory(r.q.QueryRowContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE `+memoryScopeWhere+` AND type=? AND authority=? AND source_type=? AND summary=? AND duplicate_of IS NULL AND state<>'superseded' AND (SELECT count(*) FROM memory_evidence e WHERE e.memory_id=memories.id)<64 ORDER BY created_at,id LIMIT 1`, args...))
	return m, notFound(err, "knowledge match", c.Summary)
}

func (r memoryRepo) KnowledgeRepositories(ctx context.Context, after string, limit int) ([]domain.GitRepository, error) {
	if limit < 1 || limit > 4 {
		return nil, domain.ErrInvalid
	}
	rows, err := r.q.QueryContext(ctx, `SELECT `+gitRepoCols+` FROM git_repositories WHERE project_id>? ORDER BY project_id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.GitRepository{}
	for rows.Next() {
		g, e := scanGitRepo(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (r memoryRepo) DeactivateRunEvidence(ctx context.Context, owner, run string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE memory_evidence SET active=0 WHERE owner_id=? AND json_extract(provenance,'$.runId')=? AND json_extract(provenance,'$.type')='handoff' AND active=1`, owner, run)
	return err
}
func (r memoryRepo) ReactivateEvidence(ctx context.Context, owner, key string) error {
	_, err := r.q.ExecContext(ctx, `UPDATE memory_evidence SET active=1 WHERE owner_id=? AND source_key=? AND memory_id IS NOT NULL AND active=0`, owner, key)
	return err
}
func (r memoryRepo) RetireObservations(ctx context.Context, owner string, now time.Time) error {
	// Obsolete reports remain evidence. Pinning never confers currency or proof.
	_, err := r.q.ExecContext(ctx, `UPDATE memories SET state='historical',updated_at=?,version=version+1 WHERE id IN (
 SELECT m.id FROM memories m WHERE m.owner_id=? AND m.source_type='handoff' AND m.state='needs_verification'
 AND EXISTS(SELECT 1 FROM memory_evidence e WHERE e.owner_id=m.owner_id AND e.memory_id=m.id)
 AND NOT EXISTS(SELECT 1 FROM memory_evidence e WHERE e.owner_id=m.owner_id AND e.memory_id=m.id AND e.active=1)
 ORDER BY m.updated_at,m.id LIMIT 100)`, ms(now), owner)
	return err
}

func (r memoryRepo) RetireGitHeads(ctx context.Context, s domain.MemoryScope, sha string, now time.Time) error {
	args := []any{ms(now)}
	args = append(args, memoryScopeArgs(s)...)
	args = append(args, sha)
	_, err := r.q.ExecContext(ctx, `UPDATE memories SET state='historical',updated_at=?,version=version+1 WHERE id IN (SELECT id FROM memories WHERE `+memoryScopeWhere+` AND source_type='git' AND type='git_head' AND state IN ('current','needs_verification') AND json_extract(provenance,'$.gitSha')<>? ORDER BY updated_at,id LIMIT 100)`, args...)
	return err
}

// Only an identical independently observed HEAD fact can regain currency.
// Deletion tombstones and user edits/archives cannot match this fingerprint.
func (r memoryRepo) RefreshGitObservation(ctx context.Context, s domain.MemoryScope, key, fingerprint string, now time.Time) error {
	args := []any{ms(now), ms(now)}
	args = append(args, memoryScopeArgs(s)...)
	args = append(args, fingerprint, s.OwnerID, key)
	_, err := r.q.ExecContext(ctx, `UPDATE memories SET state='current',verified_at=?,updated_at=?,version=version+1 WHERE `+memoryScopeWhere+` AND fingerprint=? AND type='git_head' AND source_type='git' AND json_extract(details,'$.archivedBy') IS NULL AND state IN ('needs_verification','historical') AND id IN (SELECT memory_id FROM memory_evidence WHERE owner_id=? AND source_key=?)`, args...)
	return err
}
