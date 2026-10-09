package devicestate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/integration"
	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
)

const MaxPendingProgress = 256
const MaxAssociations = 4096

var ErrQueueFull = errors.New("progress queue is full; waiting for workspace acknowledgments")

// Association and outbound observations belong only to this user-owned journal.
type Association struct {
	Assignment      int64            `json:"assignment"`
	WorkspaceID     string           `json:"workspaceId"`
	TeamProjectID   string           `json:"teamProjectId"`
	TicketID        string           `json:"ticketId"`
	MemberID        string           `json:"memberId"`
	DeviceID        string           `json:"deviceId"`
	LocalProjectID  string           `json:"localProjectId"`
	TaskID          string           `json:"taskId"`
	Repository      string           `json:"repository"`
	ClaimAt         time.Time        `json:"claimAt"`
	Imported        integration.Text `json:"imported"`
	TeamContext     string           `json:"teamContext"`
	Cursor          int64            `json:"cursor"`
	NextSequence    int64            `json:"nextSequence"`
	LastSnapshotAt  time.Time        `json:"lastSnapshotAt"`
	LastObservation string           `json:"lastObservation"`
	Conflict        bool             `json:"conflict"`
	Suspended       string           `json:"suspended,omitempty"`
}

func (a Association) Key() string {
	return a.WorkspaceID + ":" + a.TeamProjectID + ":" + a.TicketID + ":" + a.MemberID + ":" + a.DeviceID
}

type PendingProgress struct {
	Key         string
	Progress    domain.Progress
	Attempts    int
	NextAttempt time.Time
}
type SyncJournal struct{ pool *sqlitekit.Pool }

func OpenSyncJournal(ctx context.Context, path string) (*SyncJournal, error) {
	p, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Product: "werkbord-team-connector", BackupPrefix: "connector", Migrations: []sqlitekit.Migration{{Version: 1, Name: "sync", SQL: `
CREATE TABLE associations (id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,document TEXT NOT NULL);
CREATE TABLE outbound (association_id TEXT NOT NULL REFERENCES associations(id),sequence INTEGER NOT NULL,document TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,next_attempt INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(association_id,sequence));
`}}})
	if err != nil {
		return nil, err
	}
	return &SyncJournal{pool: p}, nil
}
func (j *SyncJournal) Close() error { return j.pool.Close() }
func (j *SyncJournal) Associations(ctx context.Context, w string) ([]Association, error) {
	out := []Association{}
	err := j.pool.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document FROM associations WHERE workspace_id=? ORDER BY id`, w)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var a Association
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
func saveAssociation(ctx context.Context, tx *sql.Tx, a Association) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO associations(id,workspace_id,document) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document`, a.Key(), a.WorkspaceID, string(raw))
	return err
}
func (j *SyncJournal) Save(ctx context.Context, a Association) error {
	return j.pool.Update(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM associations WHERE id!=?`, a.Key()).Scan(&n); err != nil {
			return err
		}
		if n >= MaxAssociations {
			return fmt.Errorf("connector association limit reached")
		}
		return saveAssociation(ctx, tx, a)
	})
}

// Enqueue persists the cursor and observations together. A full queue advances
// neither: durable controller events remain the recovery source.
func (j *SyncJournal) Enqueue(ctx context.Context, a *Association, cursor int64, observations []domain.Progress) error {
	next := *a
	err := j.pool.Update(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM outbound WHERE association_id IN (SELECT id FROM associations WHERE workspace_id=?)`, a.WorkspaceID).Scan(&n); err != nil {
			return err
		}
		if n+len(observations) > MaxPendingProgress {
			return ErrQueueFull
		}
		for _, o := range observations {
			next.NextSequence++
			o.Sequence = next.NextSequence
			o.Schema = integration.Schema
			o.TaskID = a.TaskID
			o.ProjectID = a.LocalProjectID
			o.ClaimAt = a.ClaimAt
			o.Assignment = a.Assignment
			raw, err := json.Marshal(o)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO outbound(association_id,sequence,document) VALUES(?,?,?)`, a.Key(), o.Sequence, string(raw)); err != nil {
				return err
			}
		}
		next.Cursor = cursor
		return saveAssociation(ctx, tx, next)
	})
	if err == nil {
		*a = next
	}
	return err
}
func (j *SyncJournal) Pending(ctx context.Context, key string, now time.Time) ([]PendingProgress, error) {
	out := []PendingProgress{}
	err := j.pool.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT document,attempts,next_attempt FROM outbound WHERE association_id=? ORDER BY sequence LIMIT 32`, key)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			var at int64
			p := PendingProgress{Key: key}
			if err := rows.Scan(&raw, &p.Attempts, &at); err != nil {
				return err
			}
			p.NextAttempt = time.UnixMilli(at)
			if p.NextAttempt.After(now) {
				break
			}
			if err := json.Unmarshal([]byte(raw), &p.Progress); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}
func (j *SyncJournal) Ack(ctx context.Context, key string, sequence int64) error {
	return j.pool.Update(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM outbound WHERE association_id=? AND sequence<=?`, key, sequence)
		return err
	})
}
func (j *SyncJournal) Retry(ctx context.Context, p PendingProgress, now time.Time) error {
	delay := time.Second * time.Duration(1<<min(p.Attempts, 8))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return j.pool.Update(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE outbound SET attempts=attempts+1,next_attempt=? WHERE association_id=? AND sequence=?`, now.Add(delay).UnixMilli(), p.Key, p.Progress.Sequence)
		return err
	})
}

// Suspend discards unsent reports when authority ends, preserving local task/history.
func (j *SyncJournal) Suspend(ctx context.Context, a *Association, reason string) error {
	next := *a
	next.Suspended = reason
	err := j.pool.Update(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbound WHERE association_id=?`, a.Key()); err != nil {
			return err
		}
		return saveAssociation(ctx, tx, next)
	})
	if err == nil {
		*a = next
	}
	return err
}
