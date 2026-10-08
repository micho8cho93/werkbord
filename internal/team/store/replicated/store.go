// Package replicated is Werkbord Team's storage when a workspace's data is held by several Workspace Hosts:
// the store.Store the service layer is written against, kept in rqlite (SQLite replicated with Raft) and
// reached through one client that only a Workspace Host's own service holds.
//
// What is rqlite's, and left to it: the replication of the data, electing a leader, deciding what is
// committed, changing who votes, and snapshots. What is here: how a use case that reads, decides and writes
// in one function (store.Store.Update) is turned into the one transaction request rqlite takes, and how every
// host keeps a copy to serve reads from. protocol.go says how, and why it is safe.
//
// If a quorum of the Workspace Hosts is not available, writes stop: Update returns domain.ErrReadOnly. They
// are never accepted on one host to be merged later, and no merging is done: there is one history, the
// cluster's, and a host either has the latest of it or knows it may not.
package replicated

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Options configure Open.
type Options struct {
	// Dir holds this host's copy of the database and what is made while taking it. It must be a directory
	// the configuration names (the data directory's), private to the user running Team.
	Dir string
	// Nodes are the HTTP addresses ("host:port", loopback or private) of the cluster's nodes, this host's own
	// first: reads are served from the first that answers.
	Nodes []string
	// Auth is the application user's credentials.
	Auth Auth
	// HostID names this host in the log of writes (its device's ID).
	HostID string
	Log    *slog.Logger
	// Poll is how often the copy is brought up to date from the cluster (default 250ms).
	Poll time.Duration
	// Migrations are applied when the store is opened (default: Team's).
	Migrations []sqlitekit.Migration
	// Product names the program in the message for a database newer than it (default werkbord-team).
	Product string
	// OnRemoteChange is called when the copy has taken in writes that another host made, so that whoever is
	// waiting for a change can look.
	OnRemoteChange func()
	// WaitForCluster is how long Open waits for the cluster to answer when this host has no copy to fall back
	// on (its first start, while its own node is still coming up).
	WaitForCluster time.Duration
	// FixedNodes stops the store from learning the cluster's other nodes' addresses from the cluster: it uses
	// exactly Nodes. A host whose paths to the others are fixed (and tests, which cut them) set it.
	FixedNodes bool
	// Create lets this host make the cluster's tables if the cluster has none: the first host of a new workspace.
	Create bool
	// NoMigrate opens the store without migrating the schema (it is still refused if newer than this build):
	// for a tool that only reads, such as a check of a backup.
	NoMigrate bool
	Now       func() time.Time
}

// Store is Team's storage on a replicated cluster.
type Store struct {
	o      Options
	log    *slog.Logger
	client *Client
	repl   *replica
	host   string

	// writeMu serialises everything that changes the copy: a use case's update, bringing the copy up to date,
	// taking a fresh one.
	writeMu sync.Mutex
	closed  atomic.Bool
	stop    context.CancelFunc
	done    chan struct{}

	mu       sync.Mutex
	state    health
	counters Counters
}

// Counters are what a host has done to keep its copy, for status and for tests.
type Counters struct {
	Commits, Conflicts, Retries, Applied, Snapshots, Resolved int64
}

type health struct {
	writable   bool
	reason     string
	changedAt  time.Time
	confirmed  time.Time // when the copy was last known to be current
	nodes      []NodeInfo
	nodesAt    time.Time
	schemaSeen int
}

var _ store.Store = (*Store)(nil)

// ErrSchemaNewer is returned for a workspace database made by a newer Werkbord Team.
type ErrSchemaNewer struct{ Have, Max int }

func (e ErrSchemaNewer) Error() string {
	return fmt.Sprintf("database schema version %d is newer than this build supports (%d); upgrade werkbord-team", e.Have, e.Max)
}

const (
	maxAttempts  = 25
	writeTimeout = 25 * time.Second
)

// walKeep is how many of the newest entries of the log of writes are kept when it is trimmed (a variable so that
// a test can make a host fall behind further than the log reaches).
var walKeep int64 = 20000

// walMinAge is how old an entry of the log must be before it is trimmed, so that a host that is only a little
// behind never finds the log cut under it.
var walMinAge = time.Hour

// Open opens the store: it opens this host's copy, finds the cluster's position, brings the copy to it, and
// migrates the schema if this build is newer (once, for the whole cluster). A host whose cluster cannot be
// reached opens read-only on the copy it has, if it has one.
func Open(ctx context.Context, o Options) (*Store, error) {
	if o.Dir == "" || !filepath.IsAbs(o.Dir) {
		return nil, errors.New("replicated: the directory for this host's copy must be an absolute path")
	}
	if o.Poll == 0 {
		o.Poll = 250 * time.Millisecond
	}
	if o.Product == "" {
		o.Product = "werkbord-team"
	}
	if o.Migrations == nil {
		ms, err := store.Migrations()
		if err != nil {
			return nil, err
		}
		o.Migrations = ms
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	client, err := NewClient(o.Nodes, o.Auth)
	if err != nil {
		return nil, err
	}
	repl, err := openReplica(ctx, o.Dir)
	if err != nil {
		return nil, err
	}
	s := &Store{o: o, log: log.With("component", "storage"), client: client, repl: repl, host: o.HostID, done: make(chan struct{})}
	if s.host == "" {
		s.host = "host"
	}
	if err := s.start(ctx); err != nil {
		_ = repl.close()
		return nil, err
	}
	bg, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.stop = cancel
	go s.loop(bg)
	return s, nil
}

func (s *Store) start(ctx context.Context) error {
	f, err := s.clusterFence(ctx, LevelLinearizable)
	if err != nil && !errors.Is(err, errNoFence) && s.o.WaitForCluster > 0 {
		if _, lerr := s.repl.fence(ctx); lerr != nil {
			deadline := time.Now().Add(s.o.WaitForCluster)
			for err != nil && !errors.Is(err, errNoFence) && time.Now().Before(deadline) && ctx.Err() == nil {
				time.Sleep(300 * time.Millisecond)
				f, err = s.clusterFence(ctx, LevelLinearizable)
			}
		}
	}
	if errors.Is(err, errNoFence) {
		if !s.o.Create {
			return errors.New("replicated: the database cluster holds no workspace yet, and this host was not asked to start one")
		}
		if _, _, err := s.client.Execute(ctx, genesisStatements()); err != nil {
			return fmt.Errorf("replicated: starting the workspace's database: %w", err)
		}
		f, err = s.clusterFence(ctx, LevelLinearizable)
	}
	if err != nil {
		// No cluster to ask. A copy that has a position can still be read.
		if _, lerr := s.repl.fence(ctx); lerr == nil {
			s.setUnwritable(err)
			s.log.Warn("the database cluster cannot be reached: serving reads from this host's copy; writes are refused until it can", "err", err)
			return nil
		}
		return fmt.Errorf("replicated: the database cluster cannot be reached and this host has no copy of it yet: %w", err)
	}
	s.writeMu.Lock()
	err = s.catchUp(ctx, f, false)
	s.writeMu.Unlock()
	if err != nil {
		return err
	}
	s.setWritable()
	if s.o.NoMigrate {
		return s.checkSchema(ctx)
	}
	return s.migrate(ctx)
}

// Close stops bringing the copy up to date and closes it.
func (s *Store) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.stop != nil {
		s.stop()
		<-s.done
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.client.hc.CloseIdleConnections()
	return s.repl.close()
}

// Counters returns what the host has done so far.
func (s *Store) Counters() Counters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters
}

func (s *Store) count(f func(*Counters)) {
	s.mu.Lock()
	f(&s.counters)
	s.mu.Unlock()
}

// Client returns the client the store talks to the cluster with, for the operations that are not a use case's
// (status, backup, removing a member). Only a Workspace Host's own code holds the store.
func (s *Store) Client() *Client { return s.client }

// ---- reading ----

// View runs fn against this host's copy: one consistent snapshot, which is the cluster's data as of the last
// write this host has applied. It needs no quorum, so a host that cannot reach the cluster still reads (and
// Status says how old the copy may be).
func (s *Store) View(ctx context.Context, fn func(store.Tx) error) error {
	if s.closed.Load() {
		return errors.New("replicated: closed")
	}
	pool, release, err := s.repl.acquire()
	if err != nil {
		return err
	}
	defer release()
	if err := s.schemaGuard(); err != nil {
		return err
	}
	return pool.View(ctx, func(tx *sql.Tx) error { return fn(store.NewSQLTx(tx)) })
}

// SchemaVersion reports the applied migration version.
// FreshView is reserved for authorization: a stale replica must not resurrect a revoked credential.
func (s *Store) FreshView(ctx context.Context, fn func(store.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	f, err := s.clusterFence(ctx, LevelLinearizable)
	if err != nil {
		return s.unavailable(err)
	}
	if err := s.catchUp(ctx, f, false); err != nil {
		return s.unavailable(err)
	}
	return s.View(ctx, fn)
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	pool, release, err := s.repl.acquire()
	if err != nil {
		return 0, err
	}
	defer release()
	return sqlitekit.SchemaVersion(ctx, pool.Reader)
}

// Ping checks that this host's copy answers.
func (s *Store) Ping(ctx context.Context) error {
	pool, release, err := s.repl.acquire()
	if err != nil {
		return err
	}
	defer release()
	return pool.Ping(ctx)
}

// ---- the schema ----

// schemaGuard refuses to read or write a database made by a newer build.
func (s *Store) schemaGuard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.schemaSeen > len(s.o.Migrations) {
		return ErrSchemaNewer{Have: s.state.schemaSeen, Max: len(s.o.Migrations)}
	}
	return nil
}

func (s *Store) noteSchema(v int) {
	s.mu.Lock()
	s.state.schemaSeen = v
	s.mu.Unlock()
}

func (s *Store) checkSchema(ctx context.Context) error {
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	s.noteSchema(v)
	return s.schemaGuard()
}

// migrate applies the migrations the cluster's database lacks, each as one write guarded like any other:
// of two hosts that start together the one whose write the cluster takes first migrates, the other finds
// the schema current when it looks again. A database newer than this build is refused, not touched.
func (s *Store) migrate(ctx context.Context) error {
	ms := s.o.Migrations
	for {
		if err := s.checkSchema(ctx); err != nil {
			return err
		}
		cur, err := s.SchemaVersion(ctx)
		if err != nil {
			return err
		}
		if cur >= len(ms) {
			return nil
		}
		m := ms[cur]
		if strings.HasPrefix(strings.TrimSpace(m.SQL), sqlitekit.ForeignKeysOff) {
			return fmt.Errorf("migration %04d_%s needs foreign keys off, which a migration of the replicated database cannot have", m.Version, m.Name)
		}
		stmts := splitScript(m.SQL)
		err = s.update(ctx, func(q store.Queryer) error {
			var have int
			if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&have); err != nil {
				return err
			}
			if have >= m.Version {
				return nil // another host got there first
			}
			for _, st := range stmts {
				if _, err := q.ExecContext(ctx, st); err != nil {
					return fmt.Errorf("migration %04d_%s: %w", m.Version, m.Name, err)
				}
			}
			_, err := q.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, m.Version, m.Name, s.o.Now().UnixMilli())
			return err
		})
		if err != nil {
			return err
		}
		s.log.Info("the workspace's database was migrated", "version", m.Version, "name", m.Name)
	}
}

// ---- writing ----

// Update runs fn as one write, atomically, serialised against every other write in the cluster. fn may be
// run more than once (its reads see the copy as of a position, and if another write got in first it runs
// again from the new one), so it must have no effect outside its transaction; Team's use cases have none.
// If the cluster cannot take the write, because it has no quorum, Update returns domain.ErrReadOnly and
// nothing was written.
func (s *Store) Update(ctx context.Context, fn func(store.Tx) error) error {
	return s.update(ctx, func(q store.Queryer) error { return fn(store.NewTx(q)) })
}

func (s *Store) update(ctx context.Context, fn func(store.Queryer) error) error {
	if s.closed.Load() {
		return errors.New("replicated: closed")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	began := time.Now()
	for attempt := 1; ; {
		if attempt > maxAttempts {
			return fmt.Errorf("%w: the workspace is too busy to take this change just now; try again", domain.ErrBusy)
		}
		if attempt > 1 {
			s.count(func(c *Counters) { c.Retries++ })
			d := time.Duration(attempt) * 4 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d + time.Duration(rand.Int64N(int64(d)+1))):
			}
		}
		f, err := s.clusterFence(ctx, LevelLinearizable)
		if err == nil {
			err = s.catchUp(ctx, f, false)
		}
		if err != nil {
			// A leader is being elected, or a node has just gone: give the cluster a moment before saying it cannot
			// take a write. Past the moment, it is the cluster that has no quorum, and the write is refused.
			if transient(err) && time.Since(began) < unavailableWindow && ctx.Err() == nil {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return s.unavailable(err)
		}
		if err := s.schemaGuard(); err != nil {
			return err
		}
		done, err := s.attempt(ctx, fn)
		if err != nil {
			if errors.Is(err, errRetryElection) && time.Since(began) < unavailableWindow && ctx.Err() == nil {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return s.unwrapRetry(err)
		}
		if done {
			return nil
		}
		attempt++
	}
}

// unavailableWindow is how long a write waits for a cluster that has just lost a node or a leader before it is refused.
const unavailableWindow = 8 * time.Second

// errRetryElection marks a write that the cluster did not take because it had no leader at that moment.
var errRetryElection = errors.New("replicated: no leader at that moment")

func (s *Store) unwrapRetry(err error) error {
	if errors.Is(err, errRetryElection) {
		return s.unavailable(ErrNoLeader)
	}
	return err
}

// attempt runs fn once against the copy and, if it wrote, asks the cluster to take the write. It reports
// whether the update is finished (it wrote nothing, or the cluster took it) or must be run again.
func (s *Store) attempt(ctx context.Context, fn func(store.Queryer) error) (done bool, err error) {
	pool, err := s.repl.writer()
	if err != nil {
		return false, err
	}
	tx, err := pool.Writer.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Rollback()
		}
	}()
	rec := &recorder{tx: tx}
	if err := fn(rec); err != nil {
		return false, err
	}
	if rec.err != nil {
		return false, rec.err
	}
	if len(rec.stmts) == 0 {
		return true, nil
	}
	var at fence
	if err := tx.QueryRowContext(ctx, readFenceSQL).Scan(&at.Seq, &at.Chain, &at.Epoch); err != nil {
		return false, err
	}
	b, err := newBatch(at, s.host, s.o.Now(), rec.stmts)
	if err != nil {
		return false, err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_, outcome, err := s.client.Execute(wctx, b.statements())
	switch outcome {
	case OutcomeCommitted:
	case OutcomeRolledBack:
		if isGuardFailure(err) {
			// Another write got in first. This copy is behind; run the use case again from the new position.
			s.count(func(c *Counters) { c.Conflicts++ })
			return false, nil
		}
		// The cluster refused a statement the copy accepted: the copy is not the cluster's data. Take a new one.
		s.log.Error("the database refused a statement this host's copy accepted: taking a fresh copy", "err", err)
		finished = true
		_ = tx.Rollback()
		if serr := s.snapshot(ctx, false); serr != nil {
			return false, s.unavailable(serr)
		}
		return false, nil
	case OutcomeNotApplied:
		if transient(err) {
			return false, fmt.Errorf("%w: %v", errRetryElection, err)
		}
		return false, s.unavailable(err)
	default:
		finished = true
		_ = tx.Rollback()
		committed, rerr := s.resolve(ctx, b)
		if rerr != nil {
			return false, rerr
		}
		if committed {
			s.count(func(c *Counters) { c.Resolved++ })
			// The write is in the cluster's history; this host's copy takes it in with the others.
			if f, ferr := s.clusterFence(ctx, LevelLinearizable); ferr == nil {
				_ = s.catchUp(ctx, f, false)
			}
			return true, nil
		}
		return false, nil
	}
	// The cluster has it. Make the copy say so, in the transaction it is already in.
	for _, st := range b.bookkeeping() {
		if _, err := tx.ExecContext(ctx, st.SQL, st.Args...); err != nil {
			// The cluster took the write and this copy cannot record that: it will be made again.
			finished = true
			_ = tx.Rollback()
			s.log.Error("this host's copy could not record a write the cluster took: taking a fresh copy", "err", err)
			if serr := s.snapshot(ctx, false); serr != nil {
				s.log.Warn("could not take a fresh copy yet", "err", serr)
			}
			return true, nil
		}
	}
	finished = true
	if err := tx.Commit(); err != nil {
		s.log.Error("this host's copy could not keep a write the cluster took: taking a fresh copy", "err", err)
		if serr := s.snapshot(ctx, false); serr != nil {
			s.log.Warn("could not take a fresh copy yet", "err", serr)
		}
		return true, nil
	}
	s.count(func(c *Counters) { c.Commits++ })
	s.setWritable()
	s.confirm()
	if touchesSchema(b.Data) {
		if v, err := s.SchemaVersion(ctx); err == nil {
			s.noteSchema(v)
		}
	}
	return true, nil
}

func touchesSchema(data []Stmt) bool {
	for _, d := range data {
		if strings.Contains(d.SQL, "schema_migrations") {
			return true
		}
	}
	return false
}

// writeBatch makes one write of statements that do not come from a use case: the protocol's own upkeep (a new
// identity for the history after a restore). It takes the same road a use case's write does, except that its
// statements are known beforehand, so it is applied to this host's copy in the order the cluster applied it.
func (s *Store) writeBatch(ctx context.Context, data []Stmt) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		f, err := s.clusterFence(ctx, LevelLinearizable)
		if err != nil {
			return s.unavailable(err)
		}
		if err := s.catchUp(ctx, f, false); err != nil {
			return s.unavailable(err)
		}
		at, err := s.repl.fence(ctx)
		if err != nil {
			return err
		}
		b, err := newBatch(at, s.host, s.o.Now(), data)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		_, outcome, err := s.client.Execute(wctx, b.statements())
		cancel()
		switch outcome {
		case OutcomeCommitted:
			if aerr := s.applyOne(ctx, b); aerr != nil {
				return s.snapshot(ctx, false)
			}
			return nil
		case OutcomeRolledBack:
			if !isGuardFailure(err) {
				return err
			}
		case OutcomeNotApplied:
			return s.unavailable(err)
		default:
			committed, rerr := s.resolve(ctx, b)
			if rerr != nil {
				return rerr
			}
			if committed {
				if f, ferr := s.clusterFence(ctx, LevelLinearizable); ferr == nil {
					_ = s.catchUp(ctx, f, false)
				}
				return nil
			}
		}
		time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
	}
	return fmt.Errorf("%w: the workspace is too busy to take this change just now; try again", domain.ErrBusy)
}

// resolve finds out what became of a write whose answer was lost, without ever sending it again. It does so
// by moving the cluster's position itself, with a write that does nothing: from then on the lost write, whose
// guard names the old position, can never be applied; and the one that moved the position first is told from
// the log (every write leaves its ID in it). It returns whether the lost write was applied.
func (s *Store) resolve(ctx context.Context, lost batch) (bool, error) {
	at := fence{Seq: lost.Seq - 1, Chain: lost.Prev, Epoch: lost.epoch}
	barrier, err := newBatch(at, s.host, s.o.Now(), nil)
	if err != nil {
		return false, err
	}
	deadline := time.Now().Add(resolveWait)
	for {
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false, fmt.Errorf("%w: the cluster did not confirm whether your change was applied (it may or may not have been); look at the workspace before repeating it", domain.ErrReadOnly)
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, outcome, berr := s.client.Execute(rctx, barrier.statements())
		cancel()
		switch outcome {
		case OutcomeCommitted:
			return false, nil
		case OutcomeRolledBack:
			if !isGuardFailure(berr) {
				return false, berr
			}
		}
		// The position has moved, or we could not tell: the log says who moved it.
		found, ferr := s.inLog(ctx, lost.ID, barrier.ID)
		switch {
		case ferr != nil:
		case found == lost.ID:
			return true, nil
		case found == barrier.ID:
			return false, nil
		case outcome == OutcomeRolledBack:
			// Someone else's write moved it; the lost one can no longer be applied, and was not.
			return false, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
}

const resolveWait = 45 * time.Second

// inLog returns which of the two batch IDs is in the cluster's log of writes (a confirmed read), or "".
func (s *Store) inLog(ctx context.Context, ids ...string) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := s.client.Query(rctx, LevelLinearizable, Stmt{SQL: `SELECT batch_id FROM _wal WHERE batch_id IN (?, ?)`, Args: []any{ids[0], ids[1]}})
	if err != nil {
		return "", err
	}
	if len(res) == 1 && len(res[0].Values) > 0 {
		if id, ok := res[0].Values[0][0].(string); ok {
			return id, nil
		}
	}
	return "", nil
}

// ---- what the cluster can and cannot do right now ----

func (s *Store) setWritable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.writable {
		s.state.changedAt = s.o.Now()
	}
	s.state.writable, s.state.reason = true, ""
}

func (s *Store) setUnwritable(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.writable || s.state.reason == "" {
		s.state.changedAt = s.o.Now()
	}
	s.state.writable, s.state.reason = false, err.Error()
}

func (s *Store) confirm() {
	s.mu.Lock()
	s.state.confirmed = s.o.Now()
	s.mu.Unlock()
}

// unavailable turns what the cluster said into what a caller is told: no write can be accepted now.
func (s *Store) unavailable(err error) error {
	if errors.Is(err, domain.ErrReadOnly) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var rerr *RemoteError
	if errors.As(err, &rerr) && rerr.Status >= 400 && rerr.Status < 500 {
		return err // the database refused this host: not a quorum problem
	}
	s.setUnwritable(err)
	switch {
	case errors.Is(err, ErrNoLeader):
		return fmt.Errorf("%w: the workspace's Workspace Hosts do not have a quorum right now (no leader), so no change is accepted anywhere until enough of them are back; you can still read", domain.ErrReadOnly)
	case errors.Is(err, ErrUnreachable):
		return fmt.Errorf("%w: this host cannot reach the workspace's database cluster, so no change is accepted here; you can still read what it last had", domain.ErrReadOnly)
	}
	return fmt.Errorf("%w: the workspace's database cluster did not take the change (%v); nothing was written", domain.ErrReadOnly, err)
}

// Writable reports whether the last thing this host asked of the cluster worked.
func (s *Store) Writable() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.writable, s.state.reason
}
