package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"devboard/internal/domain"
)

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

func nullMS(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromNullMS(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// notFound maps sql.ErrNoRows to domain.ErrNotFound.
func notFound(err error, what, id string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", what, id, domain.ErrNotFound)
	}
	return err
}

// isUniqueViolation reports whether err is a UNIQUE or PRIMARY KEY failure.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// isAbort reports whether err is a trigger's RAISE(ABORT, msg) from a migration.
func isAbort(err error, msg string) bool {
	return err != nil && strings.Contains(err.Error(), msg)
}

// isFKViolation reports whether err is a FOREIGN KEY failure.
func isFKViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// checkCAS turns a zero-rows UPDATE into ErrConflict or ErrNotFound.
func checkCAS(ctx context.Context, res sql.Result, q queryer, table, id string, version int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var exists int
	err = q.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id = ?", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s: %w", table, id, domain.ErrNotFound)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%s %s was modified concurrently (expected version %d): %w", table, id, version, domain.ErrConflict)
}

// encodePolicy stores an execution policy as JSON. It is normalized first, so
// an unset policy is stored as the default rather than as an empty value the
// database would refuse.
func encodePolicy(p domain.ExecutionPolicy) (string, error) {
	p = p.Normalized()
	if err := p.Validate(); err != nil {
		return "", err
	}
	return toJSON(p)
}

// decodePolicy reads one back. Fields a newer build wrote that this one does not
// know are ignored, and fields it does know but the row lacks take their default.
func decodePolicy(s string) (domain.ExecutionPolicy, error) {
	var p domain.ExecutionPolicy
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return p, fmt.Errorf("decode execution policy %q: %w", s, err)
	}
	return p.Normalized(), nil
}

// encodeExecution stores an execution configuration as JSON: only what is set.
func encodeExecution(c domain.ExecutionConfig) (string, error) {
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return "", err
	}
	return toJSON(c)
}

// decodeExecution reads one back. Fields a newer build wrote that this one does
// not know are ignored.
func decodeExecution(s string) (domain.ExecutionConfig, error) {
	var c domain.ExecutionConfig
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, fmt.Errorf("decode execution config %q: %w", s, err)
	}
	return c, nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
