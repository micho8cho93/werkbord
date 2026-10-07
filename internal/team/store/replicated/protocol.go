package replicated

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// How the cluster is made to agree on one history of writes.
//
// rqlite stores the data and replicates it with Raft, and it runs a request of statements as one
// transaction; it has no transaction a client holds open while it thinks. A use case in Team reads, decides
// and writes in one function. So a use case runs against this host's own copy of the database, which is the
// cluster's data exactly as of some point in its history, and the statements that changed anything are sent
// to the cluster as one transaction that begins with a guard. The guard names the point in history the use
// case ran at (a counter that every write moves by one, and a hash that chains the writes together); the
// cluster applies the transaction only if that is still where it is, and rolls it back, whole, if another
// write got in first. The use case is then run again, from the new point. It is optimistic concurrency, the
// guard is rqlite's own transaction doing the checking, and the order of writes is Raft's.
//
// Every write also leaves one row in a log table in the same transaction: its statements, and the next link
// of the chain. Another host brings its copy up to date by reading the log after its own position and
// applying the same statements in the same order, so every copy is the cluster's data as of a position. A
// host that has fallen further behind than the log is kept, or whose chain does not match, takes a fresh
// copy of the database from the cluster instead.

// The tables that carry it. They are the cluster's own data, replicated with the rest.
var genesisSQL = []string{
	// The position in history, and who the history belongs to. ok exists so that a failed guard is an error of the
	// database's own (NOT NULL), which rolls the whole request back.
	`CREATE TABLE IF NOT EXISTS _fence (
		id    INTEGER PRIMARY KEY CHECK (id = 1),
		seq   INTEGER NOT NULL,
		chain TEXT    NOT NULL,
		epoch TEXT    NOT NULL,
		ok    INTEGER NOT NULL DEFAULT 1
	) STRICT`,
	// One row per write, in order.
	`CREATE TABLE IF NOT EXISTS _wal (
		seq        INTEGER PRIMARY KEY,
		batch_id   TEXT    NOT NULL UNIQUE,
		prev_chain TEXT    NOT NULL,
		chain      TEXT    NOT NULL,
		host       TEXT    NOT NULL,
		at         INTEGER NOT NULL,
		stmts      TEXT    NOT NULL
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS _meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	) STRICT`,
	// What sqlitekit's migrator keeps, so that the schema's version is where it has always been.
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at INTEGER NOT NULL
	) STRICT`,
}

const (
	genesisChain = "genesis"
	// formatVersion is the version of this protocol's tables.
	formatVersion = "1"
)

// fence is a position in the history.
type fence struct {
	Seq   int64
	Chain string
	Epoch string
}

const (
	readFenceSQL = `SELECT seq, chain, epoch FROM _fence WHERE id = 1`
	guardSQL     = `UPDATE _fence SET seq = ?, chain = ?, ok = CASE WHEN seq = ? AND chain = ? AND epoch = ? THEN 1 ELSE NULL END WHERE id = 1`
	walInsertSQL = `INSERT INTO _wal (seq, batch_id, prev_chain, chain, host, at, stmts) VALUES (?, ?, ?, ?, ?, ?, ?)`
	// guardFailure is what the database says when a guard does not hold.
	guardFailure = "_fence.ok"
)

// chainOf is the next link of the chain.
func chainOf(prev, batchID, stmts string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte{0})
	h.Write([]byte(batchID))
	h.Write([]byte{0})
	h.Write([]byte(stmts))
	return hex.EncodeToString(h.Sum(nil))
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// batch is one write as the cluster applies it.
type batch struct {
	ID    string
	Seq   int64
	Prev  string // the chain it follows
	Chain string
	Host  string
	At    int64
	Data  []Stmt
	// stmts is Data as it is kept in the log.
	stmts string
	epoch string
}

// newBatch makes the batch that follows position at.
func newBatch(at fence, host string, now time.Time, data []Stmt) (batch, error) {
	logged, err := encodeStmts(data)
	if err != nil {
		return batch{}, err
	}
	id := newID()
	return batch{ID: id, Seq: at.Seq + 1, Prev: at.Chain, Chain: chainOf(at.Chain, id, logged), Host: host, At: now.UnixMilli(), Data: data, stmts: logged, epoch: at.Epoch}, nil
}

// statements are what the cluster is sent, and what a copy runs to apply the batch: the guard, the log row, and the data.
func (b batch) statements() []Stmt {
	out := make([]Stmt, 0, len(b.Data)+2)
	out = append(out,
		Stmt{SQL: guardSQL, Args: []any{b.Seq, b.Chain, b.Seq - 1, b.Prev, b.epoch}},
		Stmt{SQL: walInsertSQL, Args: []any{b.Seq, b.ID, b.Prev, b.Chain, b.Host, b.At, b.stmts}})
	return append(out, b.Data...)
}

// bookkeeping are the statements of a batch that are not its data.
func (b batch) bookkeeping() []Stmt { return b.statements()[:2] }

func (b batch) String() string { return fmt.Sprintf("write %d (%s)", b.Seq, b.ID[:8]) }

// genesisStatements make a cluster's first state: the protocol's tables, and its position in history.
func genesisStatements() []Stmt {
	var out []Stmt
	for _, q := range genesisSQL {
		out = append(out, Stmt{SQL: q})
	}
	out = append(out,
		Stmt{SQL: `INSERT OR IGNORE INTO _fence (id, seq, chain, epoch) VALUES (1, 0, ?, ?)`, Args: []any{genesisChain, newID()}},
		Stmt{SQL: `INSERT OR IGNORE INTO _meta (key, value) VALUES ('format', ?)`, Args: []any{formatVersion}},
		Stmt{SQL: `INSERT OR IGNORE INTO _meta (key, value) VALUES ('cluster_id', ?)`, Args: []any{newID()}},
		Stmt{SQL: `INSERT OR IGNORE INTO _meta (key, value) VALUES ('created_at', ?)`, Args: []any{time.Now().UnixMilli()}})
	return out
}

func isGuardFailure(err error) bool {
	se, ok := err.(*StatementError)
	return ok && se.Index == 0 && strings.Contains(se.Msg, guardFailure)
}
