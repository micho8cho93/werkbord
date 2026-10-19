package sqlite

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/integration"
	"encoding/json"
	"fmt"
	"time"
)

func (r memoryRepo) ApproveShare(ctx context.Context, owner string, s integration.MemoryShare) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	var n int
	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_shares WHERE owner_id=?`, owner).Scan(&n); err != nil {
		return err
	}
	if n >= 256 {
		return fmt.Errorf("%w: revoke an old memory share before approving another", domain.ErrConflict)
	}
	raw, _ := json.Marshal(s)
	_, err := r.q.ExecContext(ctx, `INSERT INTO memory_shares(owner_id,id,workspace_id,expires_at,document) VALUES(?,?,?,?,?)`, owner, s.ID, s.Proposal.Scope.WorkspaceID, ms(s.ExpiresAt), string(raw))
	return err
}
func (r memoryRepo) Shares(ctx context.Context, owner, w, after string, now time.Time) (integration.MemoryShares, error) {
	out := integration.MemoryShares{Schema: integration.MemorySchema, Shares: []integration.MemoryShare{}}
	if !integration.Identifier(w) || after != "" && !integration.Identifier(after) {
		return out, domain.ErrInvalid
	}
	rows, err := r.q.QueryContext(ctx, `SELECT document FROM memory_shares WHERE owner_id=? AND workspace_id=? AND id>? AND expires_at>? ORDER BY id LIMIT ?`, owner, w, after, ms(now), integration.MaxMemoryBatch+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var s integration.MemoryShare
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return out, err
		}
		if err := s.Validate(); err != nil || s.Proposal.Scope.WorkspaceID != w {
			return out, domain.ErrInvalid
		}
		if len(out.Shares) == integration.MaxMemoryBatch {
			out.More = true
			if len(out.Shares) > 0 {
				out.Next = out.Shares[len(out.Shares)-1].ID
			}
			break
		}
		out.Shares = append(out.Shares, s)
	}
	return out, rows.Err()
}
func (r memoryRepo) RevokeShare(ctx context.Context, owner, id string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM memory_shares WHERE owner_id=? AND id=?`, owner, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrNotFound
	}
	return nil
}
