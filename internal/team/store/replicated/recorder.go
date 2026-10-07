package replicated

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"devboard/internal/team/store"
)

// recorder is the SQL a use case runs through while it is run against the local copy: everything runs on
// the copy's transaction, and each statement that changed anything and succeeded is kept, in order, to be sent
// to the cluster. A read sees the use case's own earlier writes, because they are in that transaction.
type recorder struct {
	tx    *sql.Tx
	stmts []Stmt
	// err is the first thing a use case did that an update may not: it stops the update.
	err error
}

var _ store.Queryer = (*recorder)(nil)

var errForbidden = errors.New("replicated: that statement cannot be part of an update (it changes the connection, not the data)")

func (r *recorder) check(query string) (write bool, ok bool) {
	if strings.Contains(strings.ToLower(query), "_fence") {
		if r.err == nil {
			r.err = fmt.Errorf("%w: it names the cluster's position in history, which only the protocol changes", errForbidden)
		}
		return false, false
	}
	switch classify(query) {
	case kindRead:
		return false, true
	case kindWrite:
		return true, true
	}
	if r.err == nil {
		r.err = fmt.Errorf("%w: %.60s", errForbidden, query)
	}
	return false, false
}

func (r *recorder) keep(query string, args []any) error {
	na, err := normalize(args)
	if err != nil {
		if r.err == nil {
			r.err = err
		}
		return err
	}
	r.stmts = append(r.stmts, Stmt{SQL: query, Args: na})
	return nil
}

func (r *recorder) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	write, ok := r.check(query)
	if !ok {
		return nil, r.err
	}
	if write {
		if _, err := normalize(args); err != nil {
			r.err = err
			return nil, err
		}
	}
	res, err := r.tx.ExecContext(ctx, query, args...)
	if err == nil && write {
		if kerr := r.keep(query, args); kerr != nil {
			return nil, kerr
		}
	}
	return res, err
}

func (r *recorder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	write, ok := r.check(query)
	if !ok {
		return nil, r.err
	}
	rows, err := r.tx.QueryContext(ctx, query, args...)
	if err == nil && write {
		if kerr := r.keep(query, args); kerr != nil {
			_ = rows.Close()
			return nil, kerr
		}
	}
	return rows, err
}

// recordedRow keeps a write that came back through QueryRow (an UPDATE ... RETURNING) once it is known to
// have run: its error, if it has one, comes with Scan.
type recordedRow struct {
	row   *sql.Row
	r     *recorder
	query string
	args  []any
}

func (w *recordedRow) Scan(dest ...any) error {
	err := w.row.Scan(dest...)
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		if kerr := w.r.keep(w.query, w.args); kerr != nil {
			return kerr
		}
	}
	return err
}

type failedRow struct{ err error }

func (f failedRow) Scan(...any) error { return f.err }

func (r *recorder) QueryRowContext(ctx context.Context, query string, args ...any) store.Row {
	write, ok := r.check(query)
	if !ok {
		return failedRow{r.err}
	}
	if !write {
		return r.tx.QueryRowContext(ctx, query, args...)
	}
	if _, err := normalize(args); err != nil {
		r.err = err
		return failedRow{err}
	}
	return &recordedRow{row: r.tx.QueryRowContext(ctx, query, args...), r: r, query: query, args: args}
}
