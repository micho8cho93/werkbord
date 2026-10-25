package store

import (
	"context"
	"time"
)

type SecurityQueries interface {
	UseAPINonce(context.Context, string, string, string, time.Time, time.Time) (bool, error)
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
