package store

import (
	"context"
	"database/sql"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const MemoryEvidenceTTL = 24 * time.Hour
const MemoryAuditRetention = 90 * 24 * time.Hour
const MemoryRevisionLimit = 20
const MemoryFreshnessLimit = 64

func (t *sqlTx) InvalidateProjectMemory(ctx context.Context, w, p string, now time.Time) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO memory_project_changes VALUES(?,?,?) ON CONFLICT(workspace_id,project_id) DO UPDATE SET changed_at=MAX(changed_at,excluded.changed_at)`, w, p, ms(now))
	return err
}

func (t *sqlTx) memoryRelationships(ctx context.Context, r integration.SharedMemoryRecord) error {
	for _, p := range r.Scope.Projects() {
		if _, err := t.q.ExecContext(ctx, `INSERT INTO memory_relationships VALUES(?,?,?,(SELECT revision FROM projects WHERE workspace_id=? AND id=?),(SELECT repository FROM projects WHERE workspace_id=? AND id=?)) ON CONFLICT(workspace_id,memory_id,project_id) DO UPDATE SET revision=excluded.revision,repository=excluded.repository`, r.Scope.WorkspaceID, r.ID, p.ProjectID, r.Scope.WorkspaceID, p.ProjectID, r.Scope.WorkspaceID, p.ProjectID); err != nil {
			return err
		}
	}
	return nil
}

func (t *sqlTx) ReportMemoryEvidence(ctx context.Context, a MemoryPrincipal, e domain.MemoryEvidence, now time.Time) error {
	if e.Validate(now) != nil {
		return domain.ErrInvalid
	}
	if err := t.memoryAccess(ctx, a, e.Scope, domain.PPGitReport); err != nil {
		return err
	}
	p, err := t.Project(ctx, a.WorkspaceID, e.Scope.ProjectID)
	if err != nil {
		return err
	}
	if domain.RepositoryKey(p.Repository) != e.Repository {
		return domain.ErrConflict
	}
	raw, _ := json.Marshal(e)
	var sequence int64
	var previous string
	err = t.q.QueryRowContext(ctx, `SELECT sequence,document FROM memory_evidence WHERE workspace_id=? AND project_id=? AND member_id=? AND device_id=? AND branch=? AND pr=?`, a.WorkspaceID, p.ID, a.MemberID, a.DeviceID, e.Branch, e.PullRequest).Scan(&sequence, &previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if sequence == e.Sequence && previous == string(raw) {
			return nil
		}
		if sequence >= e.Sequence {
			return domain.ErrConflict
		}
		var old domain.MemoryEvidence
		if json.Unmarshal([]byte(previous), &old) != nil {
			return domain.ErrInvalid
		}
		if e.ObservedAt.Before(old.ObservedAt) {
			return domain.ErrConflict
		}
	} else {
		var count int
		if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_evidence WHERE workspace_id=?`, a.WorkspaceID).Scan(&count); err != nil {
			return err
		}
		var perBranch int
		if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_evidence WHERE workspace_id=? AND project_id=? AND branch=? AND pr=?`, a.WorkspaceID, p.ID, e.Branch, e.PullRequest).Scan(&perBranch); err != nil {
			return err
		}
		if count >= 10000 || perBranch >= 32 {
			return domain.ErrBusy
		}
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO memory_evidence VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,project_id,member_id,device_id,branch,pr) DO UPDATE SET sequence=excluded.sequence,document=excluded.document`, a.WorkspaceID, p.ID, a.MemberID, a.DeviceID, e.Branch, e.PullRequest, e.Sequence, string(raw))
	if err != nil {
		return err
	}
	if _, err = t.q.ExecContext(ctx, `INSERT INTO memory_evidence_audit(workspace_id,project_id,member_id,device_id,branch,pr,sequence,created_at,document) VALUES(?,?,?,?,?,?,?,?,?)`, a.WorkspaceID, p.ID, a.MemberID, a.DeviceID, e.Branch, e.PullRequest, e.Sequence, ms(now), string(raw)); err != nil {
		return err
	}
	if _, err = t.q.ExecContext(ctx, `DELETE FROM memory_evidence_audit WHERE workspace_id=? AND project_id=? AND member_id=? AND device_id=? AND branch=? AND pr=? AND (sequence NOT IN (SELECT sequence FROM memory_evidence_audit WHERE workspace_id=? AND project_id=? AND member_id=? AND device_id=? AND branch=? AND pr=? ORDER BY sequence DESC LIMIT 64) OR created_at<?)`, a.WorkspaceID, p.ID, a.MemberID, a.DeviceID, e.Branch, e.PullRequest, a.WorkspaceID, p.ID, a.MemberID, a.DeviceID, e.Branch, e.PullRequest, ms(now.Add(-MemoryAuditRetention))); err != nil {
		return err
	}
	// The project generation invalidates all related knowledge in this transaction.
	_, err = t.BumpRevision(ctx, a.WorkspaceID, p.ID)
	if err != nil {
		return err
	}
	return t.InvalidateProjectMemory(ctx, a.WorkspaceID, p.ID, e.ObservedAt)
}

// memoryFreshness is deliberately evaluated on every retrieval. Worker lag,
// offline authors and revoked reporters must never turn into a stale cache hit.
func (t *sqlTx) memoryFreshness(ctx context.Context, r integration.SharedMemoryRecord, now time.Time) (integration.MemoryFreshness, error) {
	f := integration.MemoryFreshness{Status: "uncertain", IntegrationState: "unknown"}
	if r.State == "deleted" || r.State == "superseded" {
		f.Status = r.State
		return f, nil
	}
	changed := false
	var invalidatedAt time.Time
	for _, target := range r.Scope.Projects() {
		var baseline int64
		var repository string
		err := t.q.QueryRowContext(ctx, `SELECT revision,repository FROM memory_relationships WHERE workspace_id=? AND memory_id=? AND project_id=?`, r.Scope.WorkspaceID, r.ID, target.ProjectID).Scan(&baseline, &repository)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return f, err
		}
		p, err := t.Project(ctx, r.Scope.WorkspaceID, target.ProjectID)
		if err != nil {
			return f, err
		}
		if repository != p.Repository {
			f.Status = "stale"
			return f, nil
		}
		if baseline != p.Revision {
			changed = true
			var stamp int64
			err := t.q.QueryRowContext(ctx, `SELECT changed_at FROM memory_project_changes WHERE workspace_id=? AND project_id=?`, r.Scope.WorkspaceID, p.ID).Scan(&stamp)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return f, err
			}
			if fromMS(stamp).After(invalidatedAt) {
				invalidatedAt = fromMS(stamp)
			}
		}
		if target.Repository != "" && domain.RepositoryKey(p.Repository) != target.Repository {
			f.Status = "stale"
			return f, nil
		}
	}
	if r.Content.Verification == "stale" || r.Content.Verification == "rejected" {
		f.Status = "stale"
		return f, nil
	}
	if changed {
		f.Status = "stale"
	}
	var taskUpdated time.Time
	taskPRState := ""
	x := r.Content.Context
	if x != nil && x.Branch != "" {
		f.IntegrationState = "branch_only"
	}
	if x == nil {
		return f, nil
	}
	// Compare claims only within the same exact scope, classification, kind and
	// branch. Different implementations on different branches remain independent.
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM shared_memories WHERE workspace_id=? AND scope_key=? AND id<>? AND json_extract(document,'$.content.context.claim')=? AND COALESCE(json_extract(document,'$.content.context.branch'),'')=? AND json_extract(document,'$.content.classification')=? AND json_extract(document,'$.content.kind')=? AND json_extract(document,'$.state') NOT IN ('deleted','superseded') LIMIT 10000`, r.Scope.WorkspaceID, memoryScopeKey(r.Scope), r.ID, x.Claim, x.Branch, r.Content.Classification, r.Content.Kind)
	if err != nil {
		return f, err
	}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var other integration.SharedMemoryRecord
		if err = json.Unmarshal([]byte(raw), &other); err != nil {
			break
		}
		if other.Content.Summary != r.Content.Summary {
			f.Conflict = true
		} else if other.Content.Source.Revision == r.Content.Source.Revision && other.ID < r.ID && (f.DuplicateOf == "" || other.ID < f.DuplicateOf) {
			f.DuplicateOf = other.ID
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return f, err
	}
	if f.Conflict {
		f.Status = "conflict"
	}
	if x.TaskID != "" && len(r.Scope.Projects()) == 1 {
		k, err := t.Ticket(ctx, r.Scope.WorkspaceID, r.Scope.Projects()[0].ProjectID, x.TaskID)
		if errors.Is(err, domain.ErrNotFound) {
			f.Status = "uncertain"
			return f, nil
		}
		if err != nil {
			return f, err
		}
		taskUpdated = k.UpdatedAt
		f.TaskState = string(k.Status)
		if k.Status == domain.TicketReview {
			f.ReviewState = "pending"
		} else if k.Status == domain.TicketDone {
			f.ReviewState = "not_in_review"
		} else {
			f.ReviewState = "not_in_review"
		}
		if k.PullRequest != nil {
			f.PRState = string(k.PullRequest.State)
			taskPRState = f.PRState
			if taskPRState == "merged" {
				f.IntegrationState = "entered_target"
			}
			if x.PullRequest != 0 && x.PullRequest != k.PullRequest.Number {
				f.PRState = "superseded"
				f.IntegrationState = "branch_only"
				taskPRState = f.PRState
			}
			f.TargetBranch = k.PullRequest.BaseBranch
		}
	}
	if x.Branch == "" || r.Content.Source.Revision == "" || len(r.Scope.Projects()) != 1 {
		if f.Conflict {
			f.IntegrationState = "unknown"
		}
		return f, nil
	}
	target := r.Scope.Projects()[0]
	p, err := t.Project(ctx, r.Scope.WorkspaceID, target.ProjectID)
	if err != nil {
		return f, err
	}
	// Expired, unavailable and revoked reports cannot corroborate a revision.
	rows, err = t.q.QueryContext(ctx, `SELECT member_id,device_id,document FROM memory_evidence WHERE workspace_id=? AND project_id=? AND branch=? AND pr=? LIMIT 10000`, r.Scope.WorkspaceID, p.ID, x.Branch, x.PullRequest)
	if err != nil {
		return f, err
	}
	type report struct{ m, d, raw string }
	reports := []report{}
	for rows.Next() {
		var e report
		if err = rows.Scan(&e.m, &e.d, &e.raw); err != nil {
			break
		}
		reports = append(reports, e)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return f, err
	}
	matched, unavailable, mismatch := false, false, false
	var revision, prState, base, authoritative string
	for _, rep := range reports {
		if err = t.memoryAccess(ctx, MemoryPrincipal{WorkspaceID: r.Scope.WorkspaceID, MemberID: rep.m, DeviceID: rep.d}, integration.SharedMemoryScope{WorkspaceID: r.Scope.WorkspaceID, Kind: "project", ProjectID: p.ID}, domain.PPGitReport); err != nil {
			unavailable = true
			continue
		}
		var e domain.MemoryEvidence
		if err = json.Unmarshal([]byte(rep.raw), &e); err != nil {
			return f, err
		}
		if e.Repository != domain.RepositoryKey(p.Repository) || !e.Available || now.Sub(e.ObservedAt) > MemoryEvidenceTTL {
			unavailable = true
			continue
		}
		if revision != "" && (revision != e.Revision || prState != e.PRState || base != e.TargetBranch) {
			f.Conflict = true
		}
		if authoritative != "" && e.AuthoritativeRevision != "" && authoritative != e.AuthoritativeRevision {
			f.Conflict = true
		}
		if e.AuthoritativeRevision != "" {
			authoritative = e.AuthoritativeRevision
		}
		revision, prState, base = e.Revision, e.PRState, e.TargetBranch
		if e.ObservedAt.After(f.EvidenceAt) {
			f.EvidenceAt = e.ObservedAt
		}
		if e.Revision == r.Content.Source.Revision {
			matched = true
		} else {
			mismatch = true
		}
	}
	// Existing synchronized branch and ticket snapshots recover missed events even
	// when the dedicated evidence endpoint was never used. They remain reports.
	{
		branches, err := t.Branches(ctx, r.Scope.WorkspaceID, p.ID)
		if err != nil {
			return f, err
		}
		for _, b := range branches {
			if b.Name != x.Branch {
				continue
			}
			if len(reports) > 0 && !b.ReportedAt.After(f.EvidenceAt) {
				continue
			}
			if t.memoryAccess(ctx, MemoryPrincipal{WorkspaceID: r.Scope.WorkspaceID, MemberID: b.ReportedBy}, integration.SharedMemoryScope{WorkspaceID: r.Scope.WorkspaceID, Kind: "project", ProjectID: p.ID}, domain.PPGitReport) != nil || now.Sub(b.ReportedAt) > MemoryEvidenceTTL || (len(b.HeadSHA) != 40 && len(b.HeadSHA) != 64) {
				unavailable = true
				continue
			}
			// A newly synchronized branch snapshot supersedes earlier source evidence.
			revision = b.HeadSHA
			authoritative = ""
			matched, mismatch = false, false
			f.EvidenceAt = b.ReportedAt
			if revision == r.Content.Source.Revision {
				matched = true
			} else {
				mismatch = true
			}
		}
	}
	f.SourceRevision = revision
	f.AuthoritativeRevision = authoritative
	// Independent task and source snapshots cannot silently overrule each other.
	// A replacement PR is already handled as superseded above.
	if taskPRState != "" && taskPRState != "superseded" && prState != "" && prState != taskPRState {
		f.Conflict = true
	}
	if prState != "" && taskPRState == "" {
		f.PRState = prState
		f.TargetBranch = base
	}
	switch {
	case f.Conflict:
		f.Status = "conflict"
	case unavailable:
		f.Status = "uncertain"
	case mismatch:
		f.Status = "stale"
	case f.PRState == "closed" || f.PRState == "superseded":
		f.Status = "stale"
	case matched && (taskUpdated.After(f.EvidenceAt) || invalidatedAt.After(f.EvidenceAt)):
		f.Status = "stale"
	case matched:
		f.Status = "source_matches"
	default:
		f.Status = "uncertain"
	}
	if f.PRState == "merged" && !f.Conflict {
		f.IntegrationState = "entered_target"
	}
	if f.Conflict {
		f.SourceRevision = ""
		f.IntegrationState = "unknown"
	}
	if f.Conflict || unavailable {
		f.AuthoritativeRevision = ""
	}
	if f.AuthoritativeRevision != "" && f.AuthoritativeRevision != r.Content.Source.Revision && !f.Conflict && !unavailable {
		f.Status = "stale"
	}
	return f, nil
}

// SearchMemories searches all relationships of ONE project. Each returned record
// rechecks its FULL scope, including cross-repository and restricted authority.
func (t *sqlTx) SearchMemories(ctx context.Context, a MemoryPrincipal, project, query, after, id string, now time.Time) (integration.MemoryPage, error) {
	out := integration.MemoryPage{Schema: integration.MemorySchema, Records: []integration.SharedMemoryRecord{}}
	scope := integration.SharedMemoryScope{WorkspaceID: a.WorkspaceID, Kind: "project", ProjectID: project}
	if err := t.memoryAccess(ctx, a, scope, domain.PPMemoryRead); err != nil {
		return out, err
	}
	if len(query) > 200 || after != "" && !integration.Identifier(after) || id != "" && !integration.Identifier(id) {
		return out, domain.ErrInvalid
	}
	rows, err := t.q.QueryContext(ctx, `SELECT m.document FROM shared_memories m JOIN memory_relationships r ON r.workspace_id=m.workspace_id AND r.memory_id=m.id WHERE r.workspace_id=? AND r.project_id=? AND m.id>? AND (?='' OR m.id=?) ORDER BY m.id LIMIT 10001`, a.WorkspaceID, project, after, id, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var r integration.SharedMemoryRecord
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return out, err
		}
		if err = r.Validate(); err != nil {
			return out, domain.ErrInvalid
		}
		if r.State == "deleted" || r.State == "superseded" || !strings.Contains(strings.ToLower(r.Content.Summary), strings.ToLower(query)) {
			continue
		}
		if err = t.memoryAccess(ctx, a, r.Scope, domain.PPMemoryRead); err != nil {
			if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrConflict) {
				continue
			}
			return out, err
		}
		if r.Content.Classification == "restricted" {
			if err = t.memoryAccess(ctx, a, r.Scope, domain.PPMemoryReadRestricted); err != nil {
				if errors.Is(err, domain.ErrForbidden) {
					continue
				}
				return out, err
			}
		}
		if len(out.Records) == integration.MaxMemoryBatch {
			out.More = true
			out.Next = out.Records[len(out.Records)-1].ID
			break
		}
		f, e := t.memoryFreshness(ctx, r, now)
		if e != nil {
			return out, e
		}
		r.Freshness = &f
		out.Records = append(out.Records, r)
	}
	if id != "" && len(out.Records) == 0 {
		return out, domain.ErrNotFound
	}
	return out, rows.Err()
}

// ReconcileMemoryBatch is bounded per tick and restart safe: oldest checked
// records go first. Multiple hosts serialize through the normal quorum writer.
func (t *sqlTx) ReconcileMemoryBatch(ctx context.Context, now time.Time) error {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM memory_evidence_audit WHERE id IN (SELECT id FROM memory_evidence_audit WHERE created_at<? ORDER BY id LIMIT 500)`, ms(now.Add(-MemoryAuditRetention))); err != nil {
		return err
	}
	rows, err := t.q.QueryContext(ctx, `SELECT m.document FROM shared_memories m LEFT JOIN memory_reconciliation f ON f.workspace_id=m.workspace_id AND f.memory_id=m.id ORDER BY COALESCE(f.checked_at,0),m.workspace_id,m.id LIMIT 50`)
	if err != nil {
		return err
	}
	records := []integration.SharedMemoryRecord{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var r integration.SharedMemoryRecord
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			break
		}
		records = append(records, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		f, err := t.memoryFreshness(ctx, r, now)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(f)
		var old string
		err = t.q.QueryRowContext(ctx, `SELECT document FROM memory_reconciliation WHERE workspace_id=? AND memory_id=?`, r.Scope.WorkspaceID, r.ID).Scan(&old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if old != string(raw) {
			if _, err = t.q.ExecContext(ctx, `INSERT INTO memory_freshness_audit(workspace_id,memory_id,created_at,classification,document) VALUES(?,?,?,?,?)`, r.Scope.WorkspaceID, r.ID, ms(now), r.Content.Classification, string(raw)); err != nil {
				return err
			}
		}
		if _, err = t.q.ExecContext(ctx, `INSERT INTO memory_reconciliation VALUES(?,?,?,?) ON CONFLICT(workspace_id,memory_id) DO UPDATE SET checked_at=excluded.checked_at,document=excluded.document`, r.Scope.WorkspaceID, r.ID, ms(now), string(raw)); err != nil {
			return err
		}
		if _, err = t.q.ExecContext(ctx, `UPDATE memory_delivery_receipts SET document=json_object('eventId',event_id,'sequence',sequence,'record',json_object('id',?)) WHERE workspace_id=? AND json_extract(document,'$.record.id')=? AND CAST(strftime('%s',json_extract(document,'$.record.updatedAt')) AS INTEGER)*1000<?`, r.ID, r.Scope.WorkspaceID, r.ID, ms(now.Add(-MemoryAuditRetention))); err != nil {
			return err
		}
		// Keep a current record and durable idempotency keys forever within the
		// workspace quota. Historical payloads have explicit age AND count bounds.
		if _, err = t.q.ExecContext(ctx, `DELETE FROM shared_memory_revisions WHERE workspace_id=? AND memory_id=? AND version<>? AND (version NOT IN (SELECT version FROM shared_memory_revisions WHERE workspace_id=? AND memory_id=? ORDER BY version DESC LIMIT 20) OR CAST(strftime('%s',json_extract(document,'$.updatedAt')) AS INTEGER)*1000<?)`, r.Scope.WorkspaceID, r.ID, r.Version, r.Scope.WorkspaceID, r.ID, ms(now.Add(-MemoryAuditRetention))); err != nil {
			return err
		}
		if _, err = t.q.ExecContext(ctx, `DELETE FROM memory_freshness_audit WHERE workspace_id=? AND memory_id=? AND (id NOT IN (SELECT id FROM memory_freshness_audit WHERE workspace_id=? AND memory_id=? ORDER BY id DESC LIMIT 64) OR created_at<?)`, r.Scope.WorkspaceID, r.ID, r.Scope.WorkspaceID, r.ID, ms(now.Add(-MemoryAuditRetention))); err != nil {
			return err
		}
	}
	return nil
}

type MemoryEvidenceHistory struct {
	MemberID string                `json:"memberId"`
	DeviceID string                `json:"deviceId,omitempty"`
	Evidence domain.MemoryEvidence `json:"evidence"`
}
type MemoryHistory struct {
	Evidence      []MemoryEvidenceHistory          `json:"evidence"`
	Revisions     []integration.SharedMemoryRecord `json:"revisions"`
	Freshness     []integration.MemoryFreshness    `json:"freshness"`
	RetentionDays int                              `json:"retentionDays"`
}

func (t *sqlTx) MemoryHistory(ctx context.Context, a MemoryPrincipal, project, id string, now time.Time) (MemoryHistory, error) {
	out := MemoryHistory{Revisions: []integration.SharedMemoryRecord{}, Freshness: []integration.MemoryFreshness{}, RetentionDays: 90}
	// History obeys the current record's full scope and classification, even after
	// deletion or supersession. Old restricted content is checked independently.
	r, err := t.memory(ctx, a.WorkspaceID, id)
	if err != nil {
		return out, err
	}
	found := false
	for _, p := range r.Scope.Projects() {
		if p.ProjectID == project {
			found = true
		}
	}
	if !found {
		return out, domain.ErrNotFound
	}
	if err = t.memoryAccess(ctx, a, r.Scope, domain.PPMemoryRead); err != nil {
		return out, err
	}
	restricted := t.memoryAccess(ctx, a, r.Scope, domain.PPMemoryReadRestricted) == nil
	if r.Content.Classification == "restricted" && !restricted {
		return out, domain.ErrNotFound
	}
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM shared_memory_revisions WHERE workspace_id=? AND memory_id=? AND CAST(strftime('%s',json_extract(document,'$.updatedAt')) AS INTEGER)*1000>=? ORDER BY version DESC LIMIT 20`, a.WorkspaceID, id, ms(now.Add(-MemoryAuditRetention)))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var v integration.SharedMemoryRecord
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			break
		}
		if v.Content.Classification != "restricted" || restricted {
			out.Revisions = append(out.Revisions, v)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = t.q.QueryContext(ctx, `SELECT document FROM memory_freshness_audit WHERE workspace_id=? AND memory_id=? AND created_at>=? AND (classification='team' OR ?=1) ORDER BY id DESC LIMIT 64`, a.WorkspaceID, id, ms(now.Add(-MemoryAuditRetention)), b2i(restricted))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		var f integration.MemoryFreshness
		if err = json.Unmarshal([]byte(raw), &f); err != nil {
			return out, err
		}
		out.Freshness = append(out.Freshness, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if r.Content.Context == nil || len(r.Scope.Projects()) != 1 {
		return out, nil
	}
	rows, err = t.q.QueryContext(ctx, `SELECT member_id,device_id,document FROM memory_evidence_audit WHERE workspace_id=? AND project_id=? AND branch=? AND pr=? AND created_at>=? ORDER BY id DESC LIMIT 64`, a.WorkspaceID, project, r.Content.Context.Branch, r.Content.Context.PullRequest, ms(now.Add(-MemoryAuditRetention)))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e MemoryEvidenceHistory
		var raw string
		if err = rows.Scan(&e.MemberID, &e.DeviceID, &raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &e.Evidence); err != nil {
			return out, err
		}
		out.Evidence = append(out.Evidence, e)
	}
	return out, rows.Err()
}

func (t *sqlTx) ResolveMemory(ctx context.Context, a MemoryPrincipal, project, id string, in domain.MemoryResolution, now time.Time) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if in.Version < 1 || len(in.Replaced) < 1 || len(in.Replaced) > 8 {
		return out, domain.ErrInvalid
	}
	page, err := t.SearchMemories(ctx, a, project, "", "", id, now)
	if err != nil {
		return out, err
	}
	out = page.Records[0]
	out.Freshness = nil
	if out.Version != in.Version || out.State != "published" || out.Content.Context == nil {
		return out, domain.ErrConflict
	}
	for _, perm := range []domain.ProjectPermission{domain.PPMemoryEdit, domain.PPMemoryPublish, domain.PPMemoryApprove} {
		if err = t.memoryAccess(ctx, a, out.Scope, perm); err != nil {
			return out, err
		}
	}
	seen := map[string]bool{id: true}
	for _, v := range in.Replaced {
		if !integration.Identifier(v.ID) || v.Version < 1 || seen[v.ID] {
			return out, domain.ErrInvalid
		}
		seen[v.ID] = true
		old, err := t.memory(ctx, a.WorkspaceID, v.ID)
		if err != nil {
			return out, err
		}
		if memoryScopeKey(old.Scope) != memoryScopeKey(out.Scope) || old.Content.Classification != out.Content.Classification || old.Content.Context == nil || old.Content.Context.Claim != out.Content.Context.Claim || old.Content.Context.Branch != out.Content.Context.Branch {
			return out, domain.ErrNotFound
		}
		if old.Content.Kind == "policy" {
			if out.Content.Kind != "policy" || out.Content.Source.Kind != "human" {
				return out, domain.ErrForbidden
			}
			if err = t.memoryAccess(ctx, a, old.Scope, domain.PPMemoryPolicy); err != nil {
				return out, err
			}
		}
		if old.State == "superseded" && old.SupersededBy == out.ID && old.Version == v.Version+1 {
			continue
		}
		if old.Version != v.Version || old.State == "deleted" || old.State == "superseded" {
			return out, domain.ErrConflict
		}
		old.Version++
		old.State = "superseded"
		old.SupersededBy = out.ID
		old.UpdatedBy = a.MemberID
		old.UpdatedAt = now
		// A suggestion has no approval to preserve. Approved records are not yet
		// published; their supersession records carry the original approval as audit.
		if old.Approval != nil && old.Approval.Version+2 != old.Version {
			old.Approval = nil
		}
		if err = t.saveMemory(ctx, old, v.Version); err != nil {
			return out, err
		}
	}
	return out, nil
}
