package replicated

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

// ---- the position of the cluster ----

// clusterFence asks the cluster where it is in its history. At LevelLinearizable the leader confirms the
// answer with a quorum, so it is at least every write acknowledged before the call.
func (s *Store) clusterFence(ctx context.Context, level Level) (fence, error) {
	res, err := s.client.Query(ctx, level, Stmt{SQL: readFenceSQL})
	if err != nil {
		var se *StatementError
		if errors.As(err, &se) && strings.Contains(se.Msg, "no such table") {
			return fence{}, errNoFence
		}
		return fence{}, err
	}
	return fenceOf(res[0])
}

func fenceOf(r Result) (fence, error) {
	if len(r.Values) != 1 || len(r.Values[0]) != 3 {
		return fence{}, errNoFence
	}
	row := r.Values[0]
	seq, err := row[0].(json.Number).Int64()
	if err != nil {
		return fence{}, err
	}
	chain, _ := row[1].(string)
	epoch, _ := row[2].(string)
	return fence{Seq: seq, Chain: chain, Epoch: epoch}, nil
}

// ---- bringing the copy to a position ----

// catchUp brings the local copy to target, the cluster's position: it applies the writes after the copy's
// position, in order, and takes a fresh copy of the whole database instead when that cannot be trusted (a
// different history, a gap in the log, a copy that is somewhere the cluster has never been). The caller holds
// writeMu. local says to take the data from this host's own node and not from the leader.
func (s *Store) catchUp(ctx context.Context, target fence, local bool) error {
	for round := 0; round < 100; round++ {
		have, err := s.repl.fence(ctx)
		switch {
		case errors.Is(err, errNoFence):
			return s.snapshot(ctx, local)
		case err != nil:
			return err
		}
		switch {
		case have.Epoch != target.Epoch || have.Seq > target.Seq:
			return s.snapshot(ctx, local)
		case have.Seq == target.Seq:
			if have.Chain != target.Chain {
				return s.snapshot(ctx, local)
			}
			s.confirm()
			return nil
		}
		level := LevelWeak
		if local {
			level = LevelNone
		}
		rows, err := s.tail(ctx, level, have.Seq)
		if err != nil {
			return err
		}
		if len(rows) == 0 || rows[0].Seq != have.Seq+1 {
			// The log does not reach back to where this copy is (it was kept only so far), or has nothing to give.
			return s.snapshot(ctx, local)
		}
		applied, err := s.applyBatches(ctx, have, rows)
		if err != nil {
			s.log.Warn("this host's copy could not take in the cluster's writes: taking a fresh copy", "err", err)
			return s.snapshot(ctx, local)
		}
		s.count(func(c *Counters) { c.Applied += int64(applied) })
		if applied > 0 && s.o.OnRemoteChange != nil {
			s.o.OnRemoteChange()
		}
	}
	return errors.New("replicated: could not catch up with the cluster")
}

type logRow struct {
	Seq                   int64
	ID, Prev, Chain, Host string
	At                    int64
	Stmts                 string
}

const tailSQL = `SELECT seq, batch_id, prev_chain, chain, host, at, stmts FROM _wal WHERE seq > ? ORDER BY seq LIMIT 200`

func (s *Store) tail(ctx context.Context, level Level, after int64) ([]logRow, error) {
	res, err := s.client.Query(ctx, level, Stmt{SQL: tailSQL, Args: []any{after}})
	if err != nil {
		return nil, err
	}
	return parseLog(res[0])
}

func parseLog(r Result) ([]logRow, error) {
	var out []logRow
	for _, v := range r.Values {
		if len(v) != 7 {
			return nil, errors.New("replicated: the cluster's log has an unexpected shape")
		}
		var row logRow
		var err error
		if row.Seq, err = v[0].(json.Number).Int64(); err != nil {
			return nil, err
		}
		row.ID, _ = v[1].(string)
		row.Prev, _ = v[2].(string)
		row.Chain, _ = v[3].(string)
		row.Host, _ = v[4].(string)
		if row.At, err = v[5].(json.Number).Int64(); err != nil {
			return nil, err
		}
		row.Stmts, _ = v[6].(string)
		out = append(out, row)
	}
	return out, nil
}

// applyBatches applies log rows to the copy, each as one transaction, checking that each is the next link of
// the chain the copy is at. It returns how many it applied.
func (s *Store) applyBatches(ctx context.Context, at fence, rows []logRow) (int, error) {
	n := 0
	cur := at
	for _, row := range rows {
		if row.Seq != cur.Seq+1 || row.Prev != cur.Chain || chainOf(row.Prev, row.ID, row.Stmts) != row.Chain {
			return n, fmt.Errorf("write %d does not follow the copy's position %d", row.Seq, cur.Seq)
		}
		data, err := decodeStmts(row.Stmts)
		if err != nil {
			return n, err
		}
		b := batch{ID: row.ID, Seq: row.Seq, Prev: row.Prev, Chain: row.Chain, Host: row.Host, At: row.At, Data: data, stmts: row.Stmts, epoch: cur.Epoch}
		if err := s.applyOne(ctx, b); err != nil {
			return n, fmt.Errorf("write %d: %w", row.Seq, err)
		}
		cur = fence{Seq: row.Seq, Chain: row.Chain, Epoch: cur.Epoch}
		n++
		if touchesSchema(data) {
			if v, err := s.SchemaVersion(ctx); err == nil {
				s.noteSchema(v)
			}
		}
	}
	return n, nil
}

func (s *Store) applyOne(ctx context.Context, b batch) error {
	pool, err := s.repl.writer()
	if err != nil {
		return err
	}
	tx, err := pool.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, st := range b.statements() {
		if _, err := tx.ExecContext(ctx, st.SQL, st.Args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// snapshot replaces the copy with the cluster's database as it is now (the leader's, or with local this host's
// own node's), after checking the file it gets is whole and has a position. The caller holds writeMu.
func (s *Store) snapshot(ctx context.Context, local bool) error {
	tmp := filepath.Join(s.o.Dir, "replica.fetch")
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	bctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := s.client.Backup(bctx, f, local); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	pos, err := s.repl.install(ctx, tmp)
	if err != nil {
		return err
	}
	s.count(func(c *Counters) { c.Snapshots++ })
	s.log.Info("this host took a fresh copy of the workspace's database", "position", pos.Seq)
	if v, err := s.SchemaVersion(ctx); err == nil {
		s.noteSchema(v)
	}
	s.confirm()
	if s.o.OnRemoteChange != nil {
		s.o.OnRemoteChange()
	}
	return nil
}

// ---- keeping the copy current ----

func (s *Store) loop(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.o.Poll)
	defer t.Stop()
	var lastNodes time.Time
	lastPrune := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.poll(ctx)
		if time.Since(lastNodes) > time.Second {
			lastNodes = time.Now()
			s.refreshNodes(ctx)
		}
		if time.Since(lastPrune) > 15*time.Minute {
			lastPrune = time.Now()
			s.maybePrune(ctx)
		}
	}
}

// poll brings the copy up to date from the first node that answers, which is this host's own: it asks for that
// node's position and the writes after the copy's in one request, and needs no leader and no quorum.
func (s *Store) poll(ctx context.Context) {
	if !s.writeMu.TryLock() {
		return // a write is under way; it brings the copy up to date itself
	}
	defer s.writeMu.Unlock()
	if s.closed.Load() {
		return
	}
	have, err := s.repl.fence(ctx)
	if err != nil && !errors.Is(err, errNoFence) {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	res, err := s.client.Query(pctx, LevelNone, Stmt{SQL: readFenceSQL}, Stmt{SQL: tailSQL, Args: []any{have.Seq}})
	if err != nil {
		var se *StatementError
		if errors.As(err, &se) {
			return // the node has no tables yet
		}
		s.noteUnreachable(err)
		return
	}
	node, err := fenceOf(res[0])
	if err != nil {
		return
	}
	switch {
	case errors.Is(s.mustFence(ctx), errNoFence), have.Epoch != node.Epoch:
		// A copy of nothing, or of another history (the cluster's database was restored): take the node's.
		s.pollSnapshot(ctx)
		return
	case node.Seq < have.Seq:
		// The node is behind this copy (this copy took in a write the node has not applied yet) as long as it
		// is on the same history: that is checked against this copy's own log.
		if !s.sameHistory(ctx, node) {
			s.pollSnapshot(ctx)
		} else {
			s.confirm()
		}
		return
	case node.Seq == have.Seq:
		if node.Chain != have.Chain {
			s.pollSnapshot(ctx)
			return
		}
		s.confirm()
		return
	}
	rows, err := parseLog(res[1])
	if err != nil || len(rows) == 0 || rows[0].Seq != have.Seq+1 {
		s.pollSnapshot(ctx)
		return
	}
	applied, err := s.applyBatches(ctx, have, rows)
	if err != nil {
		s.log.Warn("this host's copy could not take in the cluster's writes: taking a fresh copy", "err", err)
		s.pollSnapshot(ctx)
		return
	}
	s.count(func(c *Counters) { c.Applied += int64(applied) })
	if applied > 0 && s.o.OnRemoteChange != nil {
		s.o.OnRemoteChange()
	}
	if applied == len(rows) {
		s.confirm()
	}
}

func (s *Store) mustFence(ctx context.Context) error {
	_, err := s.repl.fence(ctx)
	return err
}

// sameHistory reports whether the copy's own log has the position the node reports (so the node is merely behind).
func (s *Store) sameHistory(ctx context.Context, node fence) bool {
	pool, release, err := s.repl.acquire()
	if err != nil {
		return false
	}
	defer release()
	var chain string
	err = pool.Reader.QueryRowContext(ctx, `SELECT chain FROM _wal WHERE seq = ?`, node.Seq).Scan(&chain)
	if errors.Is(err, sql.ErrNoRows) {
		// This copy's log does not have that position (it was kept only so far, or the history is another):
		// it cannot be shown that the node is merely behind, so the copy is made again.
		return false
	}
	return err == nil && chain == node.Chain
}

func (s *Store) pollSnapshot(ctx context.Context) {
	if err := s.snapshot(ctx, true); err != nil {
		s.noteUnreachable(err)
	}
}

func (s *Store) noteUnreachable(err error) {
	s.mu.Lock()
	if s.state.reason == "" || s.state.writable {
		s.state.changedAt = s.o.Now()
	}
	s.state.writable, s.state.reason = false, err.Error()
	s.mu.Unlock()
}

// refreshNodes keeps what is known of the cluster's members current, which is what says whether writes can
// happen (a leader, and a quorum of voters that answer) and lets the client reach nodes it was not told about.
func (s *Store) refreshNodes(ctx context.Context) {
	nctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	nodes, err := s.client.ClusterNodes(nctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state.nodes = nil
		s.state.nodesAt = s.o.Now()
		if s.state.writable {
			s.state.writable, s.state.reason, s.state.changedAt = false, err.Error(), s.o.Now()
		}
		return
	}
	s.state.nodes, s.state.nodesAt = nodes, s.o.Now()
	s.learn(nodes)
	voters, up, leader := 0, 0, false
	for _, n := range nodes {
		if n.Voter {
			voters++
			if n.Reachable {
				up++
			}
		}
		leader = leader || n.Leader
	}
	topo := domain.DescribeTopology(voters, len(nodes)-voters, up)
	switch {
	case topo.Writable && leader:
		if !s.state.writable {
			s.state.changedAt = s.o.Now()
		}
		s.state.writable, s.state.reason = true, ""
	default:
		reason := "the cluster has no leader"
		if !topo.Writable {
			reason = fmt.Sprintf("%d of %d voting hosts answer and %d are needed", up, voters, topo.Quorum)
		}
		if s.state.writable {
			s.state.changedAt = s.o.Now()
		}
		s.state.writable, s.state.reason = false, reason
	}
}

// learn tells the client of members it was not given, keeping the order it has (its own node first).
func (s *Store) learn(nodes []NodeInfo) {
	if s.o.FixedNodes {
		return
	}
	known := s.client.Nodes()
	have := map[string]bool{}
	for _, k := range known {
		have[k] = true
	}
	added := false
	for _, n := range nodes {
		if n.API != "" && !have[n.API] {
			if _, err := ParseAddrs([]string{n.API}); err == nil {
				known = append(known, n.API)
				have[n.API] = true
				added = true
			}
		}
	}
	if added {
		_ = s.client.SetNodes(known)
	}
}

// maybePrune trims the log when it has grown well past what is kept, if the cluster can take the write.
func (s *Store) maybePrune(ctx context.Context) {
	var n int64
	if pool, release, err := s.repl.acquire(); err == nil {
		_ = pool.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM _wal`).Scan(&n)
		release()
	}
	if n < walKeep+1000 {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if removed, err := s.Prune(pctx); err != nil {
		s.log.Debug("could not trim the log of writes", "err", err)
	} else if removed > 0 {
		s.log.Info("trimmed the log of writes", "removed", removed)
	}
}
