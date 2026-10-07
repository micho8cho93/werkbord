package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
)

const messageCols = `id, workspace_id, from_device_id, to_device_id, member_id, action, envelope, state, result, created_at, expires_at, decided_at`

func scanMessage(s interface{ Scan(...any) error }) (domain.DeviceMessage, error) {
	var m domain.DeviceMessage
	var env, state, result string
	var created, expires int64
	var decided sql.NullInt64
	err := s.Scan(&m.ID, &m.WorkspaceID, &m.FromDeviceID, &m.ToDeviceID, &m.MemberID, &m.Action, &env, &state, &result, &created, &expires, &decided)
	m.Envelope, m.State = []byte(env), domain.MessageState(state)
	if result != "" {
		m.Result = []byte(result)
	}
	m.CreatedAt, m.ExpiresAt, m.DecidedAt = fromMS(created), fromMS(expires), optMS(decided)
	return m, err
}

// InsertMessage stores a signed request in the mailbox. A message with the same ID is a replay and is refused.
func (t *sqlTx) InsertMessage(ctx context.Context, m domain.DeviceMessage) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO device_messages (`+messageCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.WorkspaceID, m.FromDeviceID, m.ToDeviceID, m.MemberID, m.Action, string(m.Envelope), string(domain.MessageQueued), "", ms(m.CreatedAt), ms(m.ExpiresAt), nil)
	if isUnique(err) {
		return fmt.Errorf("%w: this message was already sent", domain.ErrConflict)
	}
	return err
}

// Message returns a message of a workspace.
func (t *sqlTx) Message(ctx context.Context, workspaceID, id string) (domain.DeviceMessage, error) {
	m, err := scanMessage(t.q.QueryRowContext(ctx, `SELECT `+messageCols+` FROM device_messages WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	return m, notFound(err, "message")
}

// QueuedMessagesFor lists the requests waiting for a device that have not expired, oldest first.
func (t *sqlTx) QueuedMessagesFor(ctx context.Context, workspaceID, deviceID string, now time.Time, limit int) ([]domain.DeviceMessage, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+messageCols+` FROM device_messages
		WHERE workspace_id = ? AND to_device_id = ? AND state = 'queued' AND expires_at > ? ORDER BY created_at, id LIMIT ?`, workspaceID, deviceID, ms(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DeviceMessage{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountQueuedMessagesFor is how many unexpired requests wait for a device.
func (t *sqlTx) CountQueuedMessagesFor(ctx context.Context, workspaceID, deviceID string, now time.Time) (int, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_messages WHERE workspace_id = ? AND to_device_id = ? AND state = 'queued' AND expires_at > ?`,
		workspaceID, deviceID, ms(now)).Scan(&n)
	return n, err
}

// MessagesOfMember lists a person's most recent requests, newest first.
func (t *sqlTx) MessagesOfMember(ctx context.Context, workspaceID, memberID string, limit int) ([]domain.DeviceMessage, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+messageCols+` FROM device_messages WHERE workspace_id = ? AND member_id = ? ORDER BY created_at DESC, id LIMIT ?`, workspaceID, memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DeviceMessage{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DecideMessage records what the device a request was for did with it. It happens once: a message that has been decided,
// or that has expired, is not decided again. It reports whether this call decided it.
func (t *sqlTx) DecideMessage(ctx context.Context, workspaceID, id, toDeviceID string, state domain.MessageState, result string, now time.Time) (bool, error) {
	if !state.Valid() {
		return false, errors.New("store: a message is decided as done or refused")
	}
	res, err := t.q.ExecContext(ctx, `UPDATE device_messages SET state = ?, result = ?, decided_at = ?
		WHERE workspace_id = ? AND id = ? AND to_device_id = ? AND state = 'queued' AND expires_at > ?`,
		string(state), result, ms(now), workspaceID, id, toDeviceID, ms(now))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PurgeMessages removes the messages that were decided or expired before the cutoff, and returns how many.
func (t *sqlTx) PurgeMessages(ctx context.Context, workspaceID string, before time.Time) (int, error) {
	res, err := t.q.ExecContext(ctx, `DELETE FROM device_messages WHERE workspace_id = ? AND (expires_at < ? OR (decided_at IS NOT NULL AND decided_at < ?))`,
		workspaceID, ms(before), ms(before))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
