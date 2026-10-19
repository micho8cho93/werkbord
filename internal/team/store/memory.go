package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"devboard/internal/integration"
	"devboard/internal/team/domain"
)

// MemoryPrincipal carries identity only. Roles and repository bindings are read
// again by each store operation in the very transaction that returns/changes data.
type MemoryPrincipal struct{ WorkspaceID, MemberID, DeviceID string }
type MemoryQueries interface {
	DeliverMemory(context.Context, MemoryPrincipal, integration.MemoryEvent, string, time.Time) (integration.MemoryReceipt, error)
	ProposeMemory(context.Context, MemoryPrincipal, integration.MemoryProposal, string, time.Time) (integration.SharedMemoryRecord, error)
	ChangeMemory(context.Context, MemoryPrincipal, string, integration.MemoryCommand, time.Time) (integration.SharedMemoryRecord, error)
	ReadMemories(context.Context, MemoryPrincipal, integration.SharedMemoryScope, string, bool) (integration.MemoryPage, error)
}

func memoryScopeKey(s integration.SharedMemoryScope) string {
	s.Targets = append([]integration.MemoryTarget(nil), s.Targets...)
	sort.Slice(s.Targets, func(i, j int) bool { return s.Targets[i].ProjectID < s.Targets[j].ProjectID })
	raw, _ := json.Marshal(s)
	return string(raw)
}
func (t *sqlTx) memoryAccess(ctx context.Context, a MemoryPrincipal, s integration.SharedMemoryScope, permission domain.ProjectPermission) error {
	if err := s.Validate(); err != nil {
		return domain.ErrInvalid
	}
	if s.WorkspaceID != a.WorkspaceID {
		return domain.ErrNotFound
	}
	m, err := t.Member(ctx, a.WorkspaceID, a.MemberID)
	if err != nil {
		return domain.ErrUnauthenticated
	}
	if a.DeviceID != "" {
		d, e := t.Device(ctx, a.WorkspaceID, a.DeviceID)
		if e != nil || d.Revoked() || d.MemberID != m.ID {
			return domain.ErrUnauthenticated
		}
	}
	if s.Kind == "workspace" {
		if !m.Can(domain.Permission(permission)) {
			return domain.ErrForbidden
		}
		return nil
	}
	// Check visibility for ALL targets before reporting any permission/repository error.
	targets := s.Projects()
	roles := make([]domain.ProjectRole, len(targets))
	projects := make([]domain.Project, len(targets))
	for i, target := range targets {
		p, e := t.Project(ctx, a.WorkspaceID, target.ProjectID)
		if e != nil {
			return e
		}
		role, on, e := t.ProjectRole(ctx, a.WorkspaceID, p.ID, m.ID)
		if e != nil {
			return e
		}
		if m.Can(domain.PermProjectsManage) {
			role = domain.ProjectOwner
		} else if !on {
			if !m.Can(domain.PermProjectsViewAll) {
				return domain.ErrNotFound
			}
			role = ""
		}
		roles[i] = role
		projects[i] = p
	}
	for i, target := range targets {
		if !roles[i].Can(permission) {
			return domain.ErrForbidden
		}
		if projects[i].Archived && permission != domain.PPMemoryRead && permission != domain.PPMemoryReadRestricted {
			return domain.ErrConflict
		}
		if target.Repository != "" && domain.RepositoryKey(projects[i].Repository) != target.Repository {
			return domain.ErrConflict
		}
	}
	return nil
}
func (t *sqlTx) memory(ctx context.Context, w, id string) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	var raw string
	err := t.q.QueryRowContext(ctx, `SELECT document FROM shared_memories WHERE workspace_id=? AND id=?`, w, id).Scan(&raw)
	if err != nil {
		return out, notFound(err, "memory")
	}
	err = json.Unmarshal([]byte(raw), &out)
	if err == nil {
		err = out.Validate()
	}
	return out, err
}
func (t *sqlTx) saveMemory(ctx context.Context, r integration.SharedMemoryRecord, previous int64) error {
	if err := r.Validate(); err != nil {
		return domain.ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(raw) > 16<<10 {
		return domain.ErrInvalid
	}
	res, err := t.q.ExecContext(ctx, `UPDATE shared_memories SET document=?,version=? WHERE workspace_id=? AND id=? AND version=?`, string(raw), r.Version, r.Scope.WorkspaceID, r.ID, previous)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO shared_memory_revisions(workspace_id,memory_id,version,document) VALUES(?,?,?,?)`, r.Scope.WorkspaceID, r.ID, r.Version, string(raw))
	return err
}
func (t *sqlTx) memoryCapacity(ctx context.Context, w string) error {
	var n int
	if err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM shared_memory_revisions WHERE workspace_id=?`, w).Scan(&n); err != nil {
		return err
	}
	if n >= 10000 {
		return domain.ErrBusy
	}
	return nil
}
func (t *sqlTx) ProposeMemory(ctx context.Context, a MemoryPrincipal, in integration.MemoryProposal, id string, now time.Time) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if err := in.Validate(); err != nil {
		return out, domain.ErrInvalid
	}
	if !integration.Identifier(id) {
		return out, domain.ErrInvalid
	}
	if err := t.memoryAccess(ctx, a, in.Scope, domain.PPMemoryPropose); err != nil {
		return out, err
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var oldID, oldDigest string
	err := t.q.QueryRowContext(ctx, `SELECT id,proposal_digest FROM shared_memories WHERE workspace_id=? AND author_id=? AND client_id=?`, a.WorkspaceID, a.MemberID, in.ClientID).Scan(&oldID, &oldDigest)
	if err == nil {
		if oldDigest != digest {
			return out, domain.ErrConflict
		}
		return t.memory(ctx, a.WorkspaceID, oldID)
	}
	if !errors.Is(notFound(err, "memory"), domain.ErrNotFound) {
		return out, err
	}
	if err := t.memoryCapacity(ctx, a.WorkspaceID); err != nil {
		return out, err
	}
	if in.Supersedes != "" {
		old, e := t.memory(ctx, a.WorkspaceID, in.Supersedes)
		if e != nil {
			return out, e
		}
		if memoryScopeKey(old.Scope) != memoryScopeKey(in.Scope) {
			return out, domain.ErrNotFound
		}
		if old.State != "published" {
			return out, domain.ErrConflict
		}
	}
	out = integration.SharedMemoryRecord{Schema: integration.MemorySchema, ID: id, Scope: in.Scope, Content: in.Content, Version: 1, State: "suggested", AuthorID: a.MemberID, AuthorDeviceID: a.DeviceID, UpdatedBy: a.MemberID, Supersedes: in.Supersedes, CreatedAt: now, UpdatedAt: now}
	if err := out.Validate(); err != nil {
		return out, domain.ErrInvalid
	}
	doc, _ := json.Marshal(out)
	if len(doc) > 16<<10 {
		return out, domain.ErrInvalid
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO shared_memories(workspace_id,id,schema_version,scope_key,author_id,client_id,proposal_digest,version,document) VALUES(?,?,1,?,?,?,?,1,?)`, a.WorkspaceID, id, memoryScopeKey(in.Scope), a.MemberID, in.ClientID, digest, string(doc))
	if err != nil {
		if isUnique(err) {
			err = domain.ErrConflict
		}
		return out, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO shared_memory_revisions(workspace_id,memory_id,version,document) VALUES(?,?,1,?)`, a.WorkspaceID, id, string(doc))
	return out, err
}
func (t *sqlTx) ChangeMemory(ctx context.Context, a MemoryPrincipal, id string, in integration.MemoryCommand, now time.Time) (integration.SharedMemoryRecord, error) {
	var out integration.SharedMemoryRecord
	if err := in.Validate(); err != nil {
		return out, domain.ErrInvalid
	}
	permissions := map[string]domain.ProjectPermission{"edit": domain.PPMemoryEdit, "approve": domain.PPMemoryApprove, "publish": domain.PPMemoryPublish, "delete": domain.PPMemoryDelete}
	if err := t.memoryAccess(ctx, a, in.Scope, permissions[in.Action]); err != nil {
		return out, err
	}
	r, err := t.memory(ctx, a.WorkspaceID, id)
	if err != nil {
		return out, err
	}
	if memoryScopeKey(r.Scope) != memoryScopeKey(in.Scope) {
		return out, domain.ErrNotFound
	}
	if r.Version != in.Version || r.State == "deleted" || r.State == "superseded" {
		return out, domain.ErrConflict
	}
	if in.Action != "delete" {
		if err := t.memoryCapacity(ctx, a.WorkspaceID); err != nil {
			return out, err
		}
	}
	switch in.Action {
	case "edit":
		r.Content = *in.Content
		r.State = "suggested"
		r.Approval = nil
	case "approve":
		// Only an authenticated human member/device may approve; MCP grants cannot
		// reach this service. Policies need the stronger policy authority on ALL scopes.
		if a.MemberID == r.AuthorID {
			return out, domain.ErrForbidden
		}
		if r.Content.Kind == "policy" {
			if err := t.memoryAccess(ctx, a, in.Scope, domain.PPMemoryPolicy); err != nil {
				return out, err
			}
		}
		if r.Content.Verification != "verified" || r.State != "suggested" {
			return out, domain.ErrConflict
		}
		r.Approval = &integration.MemoryApproval{MemberID: a.MemberID, DeviceID: a.DeviceID, Version: r.Version + 1, At: now}
		r.State = "approved"
	case "publish":
		if r.State != "approved" || r.Approval == nil || r.Approval.Version != r.Version || r.Content.Verification != "verified" {
			return out, domain.ErrConflict
		}
		// Approval is for these exact contents. Publishing does not change them.
		if r.Content.Kind == "policy" {
			if err := t.memoryAccess(ctx, a, in.Scope, domain.PPMemoryPolicy); err != nil {
				return out, err
			}
		}
		if r.Supersedes != "" {
			if err := t.memoryAccess(ctx, a, in.Scope, domain.PPMemoryEdit); err != nil {
				return out, err
			}
			old, e := t.memory(ctx, a.WorkspaceID, r.Supersedes)
			if e != nil {
				return out, e
			}
			if memoryScopeKey(old.Scope) != memoryScopeKey(r.Scope) {
				return out, domain.ErrNotFound
			}
			if old.State != "published" {
				return out, domain.ErrConflict
			}
			prev := old.Version
			old.Version++
			old.State = "superseded"
			old.SupersededBy = r.ID
			old.UpdatedBy = a.MemberID
			old.UpdatedAt = now
			if err := t.saveMemory(ctx, old, prev); err != nil {
				return out, err
			}
		}
		r.State = "published"
	case "delete":
		r.State = "deleted"
		r.Content.Summary = ""
		r.Approval = nil
	}
	previous := r.Version
	r.Version++
	r.UpdatedBy = a.MemberID
	r.UpdatedAt = now
	err = t.saveMemory(ctx, r, previous)
	return r, err
}
func (t *sqlTx) ReadMemories(ctx context.Context, a MemoryPrincipal, s integration.SharedMemoryScope, after string, published bool) (integration.MemoryPage, error) {
	out := integration.MemoryPage{Schema: integration.MemorySchema, Records: []integration.SharedMemoryRecord{}}
	if after != "" && !integration.Identifier(after) {
		return out, domain.ErrInvalid
	}
	if err := t.memoryAccess(ctx, a, s, domain.PPMemoryRead); err != nil {
		return out, err
	}
	restrictedErr := t.memoryAccess(ctx, a, s, domain.PPMemoryReadRestricted)
	if restrictedErr != nil && !errors.Is(restrictedErr, domain.ErrForbidden) {
		return out, restrictedErr
	}
	canReadRestricted := restrictedErr == nil
	rows, err := t.q.QueryContext(ctx, `SELECT document FROM shared_memories WHERE workspace_id=? AND scope_key=? AND id>? ORDER BY id`, a.WorkspaceID, memoryScopeKey(s), after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var r integration.SharedMemoryRecord
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return out, err
		}
		if err := r.Validate(); err != nil {
			return out, domain.ErrInvalid
		}
		if r.State == "deleted" || r.State == "superseded" || published && r.State != "published" {
			continue
		}
		if r.Content.Classification == "restricted" && !canReadRestricted {
			continue
		}
		if len(out.Records) == integration.MaxMemoryBatch {
			out.More = true
			if len(out.Records) > 0 {
				out.Next = out.Records[len(out.Records)-1].ID
			}
			break
		}
		out.Records = append(out.Records, r)
	}
	return out, rows.Err()
}
