package replicated

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

// A Digest is a fingerprint of a database's contents: how many rows each table has and a hash of every row.
// Two databases with the same digest hold the same data. It is how a host's copy is checked against the
// cluster's, and how the data moved from a single-file database is checked against the original.
type Digest struct {
	// Tables maps a table's name to its row count.
	Tables map[string]int
	// Hashes maps a table's name to the hash of its rows, in order.
	Hashes map[string]string
	// Objects maps the name of each table, index and trigger to a hash of its definition.
	Objects map[string]string
}

// Restrict returns the digest of only the named tables, and of the indexes and triggers that belong to them.
func (d Digest) Restrict(tables map[string]bool, objectOwner map[string]string) Digest {
	out := Digest{Tables: map[string]int{}, Hashes: map[string]string{}, Objects: map[string]string{}}
	for n := range tables {
		if c, ok := d.Tables[n]; ok {
			out.Tables[n], out.Hashes[n] = c, d.Hashes[n]
		}
	}
	for obj, h := range d.Objects {
		if tables[obj] || tables[objectOwner[obj]] {
			out.Objects[obj] = h
		}
	}
	return out
}

// Equal reports the first difference between two digests, or "" when there is none.
func (d Digest) Diff(o Digest) string {
	var objs []string
	for n := range d.Objects {
		objs = append(objs, n)
	}
	for n := range o.Objects {
		if _, ok := d.Objects[n]; !ok {
			objs = append(objs, n)
		}
	}
	sort.Strings(objs)
	for _, n := range objs {
		if d.Objects[n] != o.Objects[n] {
			return fmt.Sprintf("the definition of %s differs", n)
		}
	}
	var names []string
	for n := range d.Tables {
		names = append(names, n)
	}
	for n := range o.Tables {
		if _, ok := d.Tables[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		a, aok := d.Tables[n]
		b, bok := o.Tables[n]
		switch {
		case !aok || !bok:
			return fmt.Sprintf("table %s is in only one of them", n)
		case a != b:
			return fmt.Sprintf("table %s has %d rows in one and %d in the other", n, a, b)
		case d.Hashes[n] != o.Hashes[n]:
			return fmt.Sprintf("table %s has different rows", n)
		}
	}
	return ""
}

// querier is what a digest is computed through: a SQL database, or the cluster.
type querier interface {
	rows(ctx context.Context, query string) ([][]string, error)
}

type sqlQuerier struct{ db *sql.DB }

func (q sqlQuerier) rows(ctx context.Context, query string) ([][]string, error) {
	rs, err := q.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	cols, err := rs.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for rs.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	return out, rs.Err()
}

type clusterQuerier struct {
	c     *Client
	level Level
}

func (q clusterQuerier) rows(ctx context.Context, query string) ([][]string, error) {
	res, err := q.c.Query(ctx, q.level, Stmt{SQL: query})
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, v := range res[0].Values {
		row := make([]string, len(v))
		for i, x := range v {
			switch t := x.(type) {
			case nil:
			case string:
				row[i] = t
			default:
				row[i] = fmt.Sprint(t)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// digestOf computes a database's digest through q. A row is rendered with SQLite's own quote(), which writes
// a value the one way whatever program holds it (a BLOB as X'..', a string with its quotes doubled).
func digestOf(ctx context.Context, q querier) (Digest, error) {
	d := Digest{Tables: map[string]int{}, Hashes: map[string]string{}, Objects: map[string]string{}}
	schema, err := q.rows(ctx, `SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL AND (name NOT LIKE 'sqlite_%' OR name = 'sqlite_sequence') ORDER BY type, name`)
	if err != nil {
		return d, err
	}
	var tables []string
	for _, r := range schema {
		// A comment is not part of the schema.
		sh := sha256.Sum256([]byte(r[0] + "\x00" + r[1] + "\x00" + strings.Join(strings.Fields(stripComments(r[2])), " ")))
		d.Objects[r[1]] = hex.EncodeToString(sh[:])
		if r[0] == "table" {
			tables = append(tables, r[1])
		}
	}
	for _, t := range tables {
		cols, err := q.rows(ctx, `SELECT name FROM pragma_table_info(`+quoteLit(t)+`) ORDER BY cid`)
		if err != nil {
			return d, err
		}
		if len(cols) == 0 {
			continue
		}
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = "quote(" + quoteIdent(c[0]) + ")"
		}
		rows, err := q.rows(ctx, `SELECT `+strings.Join(parts, ` || ',' || `)+` FROM `+quoteIdent(t)+` ORDER BY rowid`)
		if err != nil {
			return d, err
		}
		h := sha256.New()
		for _, r := range rows {
			h.Write([]byte(r[0]))
			h.Write([]byte{'\n'})
		}
		d.Tables[t] = len(rows)
		d.Hashes[t] = hex.EncodeToString(h.Sum(nil))
	}
	return d, nil
}

func quoteLit(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 && !strings.Contains(line[:i], "'") {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// DigestOfFile computes the digest of a SQLite file.
func DigestOfFile(ctx context.Context, db *sql.DB) (Digest, error) {
	return digestOf(ctx, sqlQuerier{db})
}

// Verify checks this host's copy against the cluster: it brings the copy to the cluster's current position
// (which needs a quorum), then compares a digest of every table of the copy with one the cluster computes
// from its own data. A copy that differs is replaced. It returns the digest and the position it holds.
func (s *Store) Verify(ctx context.Context) (Digest, int64, error) {
	var last error
	for try := 0; try < 6; try++ {
		d, pos, err := s.verifyOnce(ctx)
		if err == nil || !transient(err) {
			return d, pos, err
		}
		last = err
		select {
		case <-ctx.Done():
			return Digest{}, 0, ctx.Err()
		case <-time.After(time.Duration(try+1) * 300 * time.Millisecond):
		}
	}
	return Digest{}, 0, last
}

// transient reports whether an error is the cluster being between leaders or briefly out of reach.
func transient(err error) bool {
	var plain errPlain
	return errors.Is(err, ErrNoLeader) || errors.Is(err, ErrUnreachable) || errors.As(err, &plain) || errors.Is(err, domain.ErrReadOnly) || strings.Contains(err.Error(), "took a write while it was being checked")
}

func (s *Store) verifyOnce(ctx context.Context) (Digest, int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	f, err := s.clusterFence(ctx, LevelLinearizable)
	if err != nil {
		return Digest{}, 0, s.unavailable(err)
	}
	if err := s.catchUp(ctx, f, false); err != nil {
		return Digest{}, 0, err
	}
	pool, release, err := s.repl.acquire()
	if err != nil {
		return Digest{}, 0, err
	}
	local, err := digestOf(ctx, sqlQuerier{pool.Reader})
	release()
	if err != nil {
		return Digest{}, 0, err
	}
	// The cluster's data and its position are read together, so that no write falls between them.
	remote, err := digestOf(ctx, clusterQuerier{s.client, LevelLinearizable})
	if err != nil {
		return Digest{}, 0, err
	}
	if after, ferr := s.clusterFence(ctx, LevelLinearizable); ferr != nil || after != f {
		return Digest{}, 0, errors.New("replicated: the cluster took a write while it was being checked; try again")
	}
	if diff := local.Diff(remote); diff != "" {
		s.log.Error("this host's copy differs from the cluster's: replacing it", "difference", diff)
		if err := s.snapshot(ctx, false); err != nil {
			return Digest{}, 0, err
		}
		return local, f.Seq, fmt.Errorf("replicated: this host's copy differed from the cluster's (%s) and was replaced", diff)
	}
	return remote, f.Seq, nil
}
