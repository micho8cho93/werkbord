package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"devboard/internal/domain"
)

type settingsRepo struct{ q queryer }

func (r settingsRepo) Get(ctx context.Context, key string, dst any) error {
	var raw string
	err := r.q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("setting %s: %w", key, domain.ErrNotFound)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("decode setting %s: %w", key, err)
	}
	return nil
}

func (r settingsRepo) Set(ctx context.Context, key string, value any, at time.Time) error {
	raw, err := toJSON(value)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, raw, ms(at))
	return err
}

type runnerRepo struct{ q queryer }

const runnerCols = `id, name, kind, hostname, os, arch, version, created_at, last_seen_at, metadata`

func scanRunner(s interface{ Scan(...any) error }) (*domain.Runner, error) {
	var r domain.Runner
	var created, seen int64
	var raw string
	if err := s.Scan(&r.ID, &r.Name, &r.Kind, &r.Hostname, &r.OS, &r.Arch, &r.Version, &created, &seen, &raw); err != nil {
		return nil, err
	}
	var meta struct {
		PublicKey    string `json:"publicKey"`
		LastSequence int64  `json:"lastSequence"`
	}
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}
	r.PublicKey, r.LastSequence = meta.PublicKey, meta.LastSequence
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	if r.Capacity == 0 {
		r.Capacity = 1
		r.Automatic = true
	}
	r.CreatedAt, r.LastSeenAt = fromMS(created), fromMS(seen)
	return &r, nil
}

func (r runnerRepo) UpsertLocal(ctx context.Context, in *domain.Runner) (*domain.Runner, error) {
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO runners (`+runnerCols+`) VALUES (?, ?, 'local', ?, ?, ?, ?, ?, ?, '{}')
		ON CONFLICT (kind) WHERE kind = 'local' DO UPDATE SET
			name = CASE WHEN json_extract(runners.metadata,'$.name') IS NULL THEN excluded.name ELSE runners.name END, hostname = excluded.hostname, os = excluded.os, arch = excluded.arch,
			version = excluded.version, last_seen_at = excluded.last_seen_at`,
		in.ID, in.Name, in.Hostname, in.OS, in.Arch, in.Version, ms(in.CreatedAt), ms(in.LastSeenAt))
	if err != nil {
		return nil, err
	}
	return scanRunner(r.q.QueryRowContext(ctx, `SELECT `+runnerCols+` FROM runners WHERE kind = 'local'`))
}

func (r runnerRepo) List(ctx context.Context) ([]domain.Runner, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+runnerCols+` FROM runners ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Runner{}
	for rows.Next() {
		rn, err := scanRunner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rn)
	}
	return out, rows.Err()
}

func (r runnerRepo) Get(ctx context.Context, id string) (*domain.Runner, error) {
	out, err := scanRunner(r.q.QueryRowContext(ctx, `SELECT `+runnerCols+` FROM runners WHERE id=?`, id))
	return out, notFound(err, "runner", id)
}
func (r runnerRepo) Save(ctx context.Context, in *domain.Runner) error {
	// Public API hides authentication fields; persistence must retain them.
	raw, err := toJSON(struct {
		*domain.Runner
		PublicKey    string `json:"publicKey"`
		LastSequence int64  `json:"lastSequence"`
	}{in, in.PublicKey, in.LastSequence})
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO runners (`+runnerCols+`) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,last_seen_at=excluded.last_seen_at,metadata=excluded.metadata`, in.ID, in.Name, in.Kind, in.Hostname, in.OS, in.Arch, in.Version, ms(in.CreatedAt), ms(in.LastSeenAt), raw)
	return err
}
