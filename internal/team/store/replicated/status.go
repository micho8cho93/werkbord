package replicated

import (
	"context"
	"fmt"
	"time"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Status reports the storage as it is now, honestly: how many hosts hold the workspace and vote, whether a
// quorum answers, whether writes are being accepted or the workspace is read-only, and how far this host's copy
// may be behind. It holds no secret and no path.
func (s *Store) Status(ctx context.Context) domain.StorageStatus {
	s.mu.Lock()
	stale := s.o.Now().Sub(s.state.nodesAt) > 2*time.Second
	s.mu.Unlock()
	if stale {
		s.refreshNodes(ctx)
	}
	s.mu.Lock()
	nodes := append([]NodeInfo(nil), s.state.nodes...)
	confirmed := s.state.confirmed
	writable, reason := s.state.writable, s.state.reason
	s.mu.Unlock()

	st := domain.StorageStatus{State: domain.StorageReplicated, Hosts: []domain.StorageHost{}}
	voters, up, nonVoters := 0, 0, 0
	for _, n := range nodes {
		st.Hosts = append(st.Hosts, domain.StorageHost{NodeID: n.ID, Voter: n.Voter, Reachable: n.Reachable, Leader: n.Leader, Error: n.Error})
		if n.Voter {
			voters++
			if n.Reachable {
				up++
			}
		} else {
			nonVoters++
		}
		if n.Leader {
			st.Leader = n.ID
		}
	}
	domain.SortStorageHosts(st.Hosts)
	st.Topology = domain.DescribeTopology(voters, nonVoters, up)
	switch {
	case len(nodes) == 0:
		st.ReadOnly = true
		st.Reason = "this host cannot reach the workspace's database cluster; it serves what its own copy last held"
	case !st.Topology.Writable:
		st.ReadOnly = true
		st.Reason = fmt.Sprintf("%d of %d voting Workspace Hosts answer and %d are needed for a safe write", up, voters, st.Topology.Quorum)
	case st.Leader == "":
		st.ReadOnly = true
		st.Reason = "the cluster has no leader yet"
	case !writable:
		st.ReadOnly = true
		st.Reason = reason
	}
	st.Writable = !st.ReadOnly
	if f, err := s.repl.fence(ctx); err == nil {
		st.Local.Position = f.Seq
	}
	if !confirmed.IsZero() {
		st.Local.BehindMillis = s.o.Now().Sub(confirmed).Milliseconds()
	} else {
		st.Local.BehindMillis = -1
	}
	if v, err := s.SchemaVersion(ctx); err == nil {
		st.SchemaVersion = v
	}
	if st.ReadOnly && st.Local.BehindMillis > 5000 {
		st.Notes = append(st.Notes, "This host's copy has not been confirmed current for a while: what it shows may be missing recent changes made through other hosts.")
	}
	for _, n := range nodes {
		if !n.Reachable {
			st.Notes = append(st.Notes, fmt.Sprintf("Workspace Host %s does not answer.", n.ID))
		}
	}
	return st
}

// Prune forgets old entries of the cluster's log of writes, which hosts use to catch up (a host that has
// fallen behind further than it reaches takes a fresh copy of the database instead). It keeps the newest
// walKeep and anything from the last hour. It is a write like any other.
func (s *Store) Prune(ctx context.Context) (int64, error) {
	var removed int64
	err := s.update(ctx, func(q store.Queryer) error {
		var top int64
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM _wal`).Scan(&top); err != nil {
			return err
		}
		cut := top - walKeep
		if cut <= 0 {
			return nil
		}
		var n int64
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM _wal WHERE seq <= ? AND at < ?`, cut, s.o.Now().Add(-walMinAge).UnixMilli()).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		removed = n
		_, err := q.ExecContext(ctx, `DELETE FROM _wal WHERE seq <= ? AND at < ?`, cut, s.o.Now().Add(-walMinAge).UnixMilli())
		return err
	})
	return removed, err
}

// ClusterID names the cluster whose data this is.
func (s *Store) ClusterID(ctx context.Context) (string, error) {
	var id string
	p, release, err := s.repl.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	err = p.Reader.QueryRowContext(ctx, `SELECT value FROM _meta WHERE key = 'cluster_id'`).Scan(&id)
	return id, err
}

var _ = sqlitekit.ForeignKeysOff
