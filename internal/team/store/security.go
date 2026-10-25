package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
)

type SecurityQueries interface {
	UseAPINonce(context.Context, string, string, string, time.Time, time.Time) (bool, error)
	// MemberPremiums maps each member who holds a valid unlock key to the key's ID.
	MemberPremiums(ctx context.Context, workspaceID string) (map[string]string, error)
	// SetMemberPremium records that the member presented the unlock key with this ID. A key already used by another member of
	// the workspace is domain.ErrConflict.
	SetMemberPremium(ctx context.Context, workspaceID, memberID, keyID string, at time.Time) error
	// SetEnrollmentPremium and EnrollmentPremium keep the key ID a joining device presented until its enrollment is decided.
	SetEnrollmentPremium(ctx context.Context, workspaceID, enrollmentID, keyID string) error
	EnrollmentPremium(ctx context.Context, workspaceID, enrollmentID string) (string, error)
}

func (t *sqlTx) MemberPremiums(ctx context.Context, workspaceID string) (map[string]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT member_id, key_id FROM member_premium WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var member, key string
		if err := rows.Scan(&member, &key); err != nil {
			return nil, err
		}
		out[member] = key
	}
	return out, rows.Err()
}

func (t *sqlTx) SetMemberPremium(ctx context.Context, workspaceID, memberID, keyID string, at time.Time) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO member_premium(workspace_id, member_id, key_id, verified_at) VALUES (?, ?, ?, ?)
	 ON CONFLICT(workspace_id, member_id) DO UPDATE SET key_id = excluded.key_id, verified_at = excluded.verified_at`, workspaceID, memberID, keyID, ms(at))
	if isUnique(err) {
		return fmt.Errorf("%w: this unlock key is already in use by another member of this team", domain.ErrConflict)
	}
	return err
}

func (t *sqlTx) SetEnrollmentPremium(ctx context.Context, workspaceID, enrollmentID, keyID string) error {
	_, err := t.q.ExecContext(ctx, `INSERT OR REPLACE INTO enrollment_premium(workspace_id, enrollment_id, key_id) VALUES (?, ?, ?)`, workspaceID, enrollmentID, keyID)
	return err
}

func (t *sqlTx) EnrollmentPremium(ctx context.Context, workspaceID, enrollmentID string) (string, error) {
	var key string
	err := t.q.QueryRowContext(ctx, `SELECT key_id FROM enrollment_premium WHERE workspace_id = ? AND enrollment_id = ?`, workspaceID, enrollmentID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return key, err
}

func (t *sqlTx) UseAPINonce(ctx context.Context, ws, dev, nonce string, expires, now time.Time) (bool, error) {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM api_replays WHERE expires_at <= ?`, ms(now)); err != nil {
		return false, err
	}
	// Capacity is per workspace; live entries are never evicted to make room.
	res, err := t.q.ExecContext(ctx, `INSERT OR IGNORE INTO api_replays(workspace_id, device_id, nonce, expires_at)
	 SELECT ?, ?, ?, ? WHERE (SELECT count(*) FROM api_replays WHERE workspace_id = ?) < 100000`, ws, dev, nonce, ms(expires), ws)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
