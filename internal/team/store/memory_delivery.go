package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

func (t *sqlTx) DeliverMemory(ctx context.Context, a MemoryPrincipal, e integration.MemoryEvent, id string, now time.Time) (integration.MemoryReceipt, error) {
	var out integration.MemoryReceipt
	if e.Validate() != nil || a.DeviceID == "" {
		return out, domain.ErrInvalid
	}
	// Revalidate authority even when replaying an existing receipt.
	if err := t.memoryAccess(ctx, a, e.Proposal.Scope, domain.PPMemoryPropose); err != nil {
		return out, err
	}
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var oldDigest, receipt string
	err := t.q.QueryRowContext(ctx, `SELECT digest,document FROM memory_delivery_receipts WHERE workspace_id=? AND member_id=? AND device_id=? AND event_id=?`, a.WorkspaceID, a.MemberID, a.DeviceID, e.ID).Scan(&oldDigest, &receipt)
	if err == nil {
		if oldDigest != digest && e.Action != "cancel" {
			return out, domain.ErrConflict
		}
		err = json.Unmarshal([]byte(receipt), &out)
		if err == nil {
			out.Record, err = t.memory(ctx, a.WorkspaceID, out.Record.ID)
		}
		if err == nil && e.Action == "cancel" {
			if out.Sequence != e.Sequence || memoryScopeKey(out.Record.Scope) != memoryScopeKey(e.Proposal.Scope) || out.Record.AuthorDeviceID != a.DeviceID {
				return out, domain.ErrConflict
			}
			out.Record, err = t.withdrawProjection(ctx, a, out.Record, now)
			if err != nil {
				return out, err
			}
			doc, _ := json.Marshal(out)
			_, err = t.q.ExecContext(ctx, `UPDATE memory_delivery_receipts SET digest=?,document=? WHERE workspace_id=? AND member_id=? AND device_id=? AND event_id=?`, digest, string(doc), a.WorkspaceID, a.MemberID, a.DeviceID, e.ID)
		}
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var sequence int64
	err = t.q.QueryRowContext(ctx, `SELECT sequence FROM memory_delivery_streams WHERE workspace_id=? AND member_id=? AND device_id=?`, a.WorkspaceID, a.MemberID, a.DeviceID).Scan(&sequence)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if e.Sequence != sequence+1 {
		return out, domain.ErrConflict
	}
	var revision int64
	var previousAction string
	err = t.q.QueryRowContext(ctx, `SELECT revision,action FROM memory_delivery_sources WHERE workspace_id=? AND member_id=? AND device_id=? AND project_id=? AND source_id=?`, a.WorkspaceID, a.MemberID, a.DeviceID, e.Proposal.Scope.ProjectID, e.SourceID).Scan(&revision, &previousAction)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if e.Revision < revision || e.Action == "propose" && e.Revision == revision && previousAction == "propose" {
		return out, domain.ErrConflict
	}
	var count int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_delivery_receipts WHERE workspace_id=?`, a.WorkspaceID).Scan(&count); err != nil {
		return out, err
	}
	if count >= 10000 && e.Action == "propose" {
		return out, domain.ErrBusy
	}
	var previous *integration.SharedMemoryRecord
	if e.Previous != "" {
		var previousID string
		err = t.q.QueryRowContext(ctx, `SELECT id FROM shared_memories WHERE workspace_id=? AND author_id=? AND client_id=? UNION ALL SELECT memory_id FROM memory_proposal_aliases WHERE workspace_id=? AND author_id=? AND client_id=? LIMIT 1`, a.WorkspaceID, a.MemberID, e.Previous, a.WorkspaceID, a.MemberID, e.Previous).Scan(&previousID)
		if err != nil {
			return out, notFound(err, "memory")
		}
		r, err := t.memory(ctx, a.WorkspaceID, previousID)
		if err != nil {
			return out, err
		}
		if r.AuthorDeviceID != a.DeviceID || memoryScopeKey(r.Scope) != memoryScopeKey(e.Proposal.Scope) {
			return out, domain.ErrForbidden
		}
		if r.Content.Kind == "policy" && (r.State == "approved" || r.State == "published") {
			return out, domain.ErrForbidden
		}
		previous = &r
	}
	var record integration.SharedMemoryRecord
	if e.Action == "revoke" {
		if previous == nil {
			return out, domain.ErrInvalid
		}
		record = *previous
		// A source author may withdraw its own projection without gaining curator
		// authority over another author's records. Quota cannot prevent withdrawal.
		record, err = t.withdrawProjection(ctx, a, record, now)
		if err != nil {
			return out, err
		}

	} else {
		proposal := e.Proposal
		if e.Action == "cancel" {
			proposal.Content.Summary = "Publication withdrawn before delivery"
			proposal.Supersedes = ""
		}

		if e.Action != "cancel" && previous != nil && previous.State == "published" {
			proposal.Supersedes = previous.ID
		}
		// Corrections of unpublished suggestions withdraw their old contents.
		if previous != nil && (previous.State == "suggested" || previous.State == "approved") {
			r := *previous
			v := r.Version
			r.Version++
			r.State = "deleted"
			r.Content.Summary = ""
			r.Approval = nil
			r.UpdatedAt = now
			r.UpdatedBy = a.MemberID
			if err := t.saveMemory(ctx, r, v); err != nil {
				return out, err
			}
		}
		record, err = t.ProposeMemory(ctx, a, proposal, id, now)
		if err != nil {
			return out, err
		}
		if record.AuthorDeviceID != a.DeviceID {
			return out, domain.ErrConflict
		}
		if e.Action == "cancel" {
			record, err = t.withdrawProjection(ctx, a, record, now)
			if err != nil {
				return out, err
			}
		}
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO memory_delivery_sources VALUES(?,?,?,?,?,?,?) ON CONFLICT(workspace_id,member_id,device_id,project_id,source_id) DO UPDATE SET revision=excluded.revision,action=excluded.action`, a.WorkspaceID, a.MemberID, a.DeviceID, e.Proposal.Scope.ProjectID, e.SourceID, e.Revision, e.Action)
	if err != nil {
		return out, err
	}
	out = integration.MemoryReceipt{EventID: e.ID, Sequence: e.Sequence, Record: record}
	doc, _ := json.Marshal(out)
	_, err = t.q.ExecContext(ctx, `INSERT INTO memory_delivery_receipts VALUES(?,?,?,?,?,?,?)`, a.WorkspaceID, a.MemberID, a.DeviceID, e.ID, e.Sequence, digest, string(doc))
	if err != nil {
		return out, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO memory_delivery_streams VALUES(?,?,?,?) ON CONFLICT(workspace_id,member_id,device_id) DO UPDATE SET sequence=excluded.sequence`, a.WorkspaceID, a.MemberID, a.DeviceID, e.Sequence)
	return out, err
}

func (t *sqlTx) withdrawProjection(ctx context.Context, a MemoryPrincipal, r integration.SharedMemoryRecord, now time.Time) (integration.SharedMemoryRecord, error) {
	// Individual withdrawal cannot revoke a legitimately approved Team policy.
	if r.Content.Kind == "policy" && (r.State == "approved" || r.State == "published") {
		return r, domain.ErrForbidden
	}
	if r.State == "deleted" {
		return r, nil
	}
	version := r.Version
	r.Version++
	r.State = "deleted"
	r.Content.Summary = ""
	r.Approval = nil
	r.SupersededBy = ""
	r.UpdatedAt = now
	r.UpdatedBy = a.MemberID
	return r, t.saveMemory(ctx, r, version)
}
