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
