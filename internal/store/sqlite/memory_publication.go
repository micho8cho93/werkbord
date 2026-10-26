package sqlite

import (
	"context"
	"encoding/json"
	"werkbord/internal/domain"
	"werkbord/internal/integration"
)

func (r memoryRepo) PublicationGrants(ctx context.Context, owner string) ([]domain.PublicationGrant, error) {
	out := []domain.PublicationGrant{}
	rows, err := r.q.QueryContext(ctx, `SELECT document FROM memory_publication_grants WHERE owner_id=? ORDER BY workspace_id,project_id`, owner)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var g domain.PublicationGrant
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &g); err != nil {
			return out, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (r memoryRepo) SavePublicationGrant(ctx context.Context, owner string, g domain.PublicationGrant) error {
	var n int
	if err := r.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_publication_grants WHERE owner_id=? AND NOT(workspace_id=? AND project_id=?)`, owner, g.WorkspaceID, g.ProjectID).Scan(&n); err != nil {
		return err
	}
	if n >= 64 {
		return domain.ErrConflict
	}
	raw, _ := json.Marshal(g)
	_, err := r.q.ExecContext(ctx, `INSERT INTO memory_publication_grants VALUES(?,?,?,?) ON CONFLICT(owner_id,workspace_id,project_id) DO UPDATE SET document=excluded.document`, owner, g.WorkspaceID, g.ProjectID, string(raw))
	return err
}
func (r memoryRepo) Publications(ctx context.Context, owner, w string) ([]domain.MemoryPublication, error) {
	out := []domain.MemoryPublication{}
	rows, err := r.q.QueryContext(ctx, `SELECT document FROM memory_publications WHERE owner_id=? AND workspace_id=? ORDER BY project_id,memory_id`, owner, w)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var p domain.MemoryPublication
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// The projection, stream sequence and outbound event commit in one transaction.
func (r memoryRepo) SavePublication(ctx context.Context, owner string, p *domain.MemoryPublication) error {
	if p.Event != nil && p.Event.Sequence == 0 {
		var n int
		if err := r.q.QueryRowContext(ctx, `SELECT count(*) FROM memory_publication_outbound WHERE owner_id=? AND workspace_id=?`, owner, p.Grant.WorkspaceID).Scan(&n); err != nil {
			return err
		}
		if n >= 2048 && p.Event.Action == "propose" {
			return domain.ErrPublicationQueueFull
		}
		_, err := r.q.ExecContext(ctx, `INSERT INTO memory_publication_streams(owner_id,workspace_id,sequence) VALUES(?,?,1) ON CONFLICT(owner_id,workspace_id) DO UPDATE SET sequence=sequence+1`, owner, p.Grant.WorkspaceID)
		if err != nil {
			return err
		}
		if err = r.q.QueryRowContext(ctx, `SELECT sequence FROM memory_publication_streams WHERE owner_id=? AND workspace_id=?`, owner, p.Grant.WorkspaceID).Scan(&p.Event.Sequence); err != nil {
			return err
		}
		if err = p.Event.Validate(); err != nil {
			return err
		}
		raw, _ := json.Marshal(p.Event)
		if _, err = r.q.ExecContext(ctx, `INSERT INTO memory_publication_outbound VALUES(?,?,?,?,?)`, owner, p.Grant.WorkspaceID, p.Event.Sequence, p.Event.ID, string(raw)); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(p)
	_, err := r.q.ExecContext(ctx, `INSERT INTO memory_publications VALUES(?,?,?,?,?) ON CONFLICT(owner_id,workspace_id,project_id,memory_id) DO UPDATE SET document=excluded.document`, owner, p.Grant.WorkspaceID, p.Grant.ProjectID, p.MemoryID, string(raw))
	return err
}
func (r memoryRepo) PublicationEvents(ctx context.Context, owner, w string) (integration.MemoryEventPage, error) {
	out := integration.MemoryEventPage{Schema: integration.MemorySchema, Events: []integration.MemoryEvent{}}
	rows, err := r.q.QueryContext(ctx, `SELECT document FROM memory_publication_outbound WHERE owner_id=? AND workspace_id=? ORDER BY sequence LIMIT ?`, owner, w, integration.MaxMemoryBatch)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var e integration.MemoryEvent
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &e); err != nil {
			return out, err
		}
		if err = e.Validate(); err != nil {
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	return out, rows.Err()
}
func (r memoryRepo) AckPublication(ctx context.Context, owner, w string, a integration.MemoryReceipt) error {
	var raw string
	var acked int64
	if err := r.q.QueryRowContext(ctx, `SELECT acked FROM memory_publication_streams WHERE owner_id=? AND workspace_id=?`, owner, w).Scan(&acked); err != nil {
		return err
	}
	// A lost local ack response can safely be retried, but cannot alter a projection.
	if a.Sequence <= acked {
		ps, err := r.Publications(ctx, owner, w)
		if err != nil {
			return err
		}
		for i := range ps {
			p := &ps[i]
			if p.Event != nil && p.Event.ID == a.EventID && p.Event.Sequence == a.Sequence && p.Receipt != nil {
				if a.Record.Validate() != nil || a.Record.ID != p.Receipt.Record.ID || a.Record.Version < p.Receipt.Record.Version {
					return domain.ErrInvalid
				}
				p.Receipt = &a
				if p.Event.Action != "revoke" {
					p.State = a.Record.State
				}
				return r.SavePublication(ctx, owner, p)
			}
		}
		return nil
	}
	if a.Sequence != acked+1 {
		return domain.ErrConflict
	}
	if err := r.q.QueryRowContext(ctx, `SELECT document FROM memory_publication_outbound WHERE owner_id=? AND workspace_id=? AND sequence=? AND event_id=?`, owner, w, a.Sequence, a.EventID).Scan(&raw); err != nil {
		return err
	}
	var e integration.MemoryEvent
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return err
	}
	scope, _ := json.Marshal(a.Record.Scope)
	want, _ := json.Marshal(e.Proposal.Scope)
	if a.Record.Validate() != nil || string(scope) != string(want) {
		return domain.ErrInvalid
	}
	ps, err := r.Publications(ctx, owner, w)
	if err != nil {
		return err
	}
	for i := range ps {
		p := &ps[i]
		if p.Event != nil && p.Event.ID == a.EventID {
			p.Receipt = &a
			if e.Action == "revoke" {
				if p.State != "awaiting_approval" {
					p.State = "revoked"
				}
			} else {
				p.State = a.Record.State
			}
			if err = r.SavePublication(ctx, owner, p); err != nil {
				return err
			}
		}
	}
	if _, err = r.q.ExecContext(ctx, `DELETE FROM memory_publication_outbound WHERE owner_id=? AND workspace_id=? AND sequence=?`, owner, w, a.Sequence); err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `UPDATE memory_publication_streams SET acked=? WHERE owner_id=? AND workspace_id=?`, a.Sequence, owner, w)
	return err
}

// Cancellation replaces only an undelivered proposal with a closed tombstone.
// The Host handles the uncertain case where the original commit lost its reply.
func (r memoryRepo) CancelPublicationEvent(ctx context.Context, owner string, e integration.MemoryEvent) error {
	if e.Action != "cancel" || e.Validate() != nil {
		return domain.ErrInvalid
	}
	raw, _ := json.Marshal(e)
	_, err := r.q.ExecContext(ctx, `UPDATE memory_publication_outbound SET document=? WHERE owner_id=? AND workspace_id=? AND sequence=? AND event_id=?`, string(raw), owner, e.Proposal.Scope.WorkspaceID, e.Sequence, e.ID)
	return err
}
