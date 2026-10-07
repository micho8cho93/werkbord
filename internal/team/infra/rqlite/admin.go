package rqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Admin asks one node about the cluster, and makes the membership changes rqlite supports. It
// speaks to rqlite's HTTP API, to a node of this workspace, and to nothing else; it holds the
// admin user's credentials for as long as it lives and returns none.
type Admin struct {
	base string
	user string
	pass string
	hc   *http.Client
	// bad is why this Admin will not speak to its address: a node is only ever reached on loopback or
	// on the workspace's private network, never on a public address.
	bad error
}

// NewAdmin returns an Admin for the node at addr, as the admin user. An address that is not loopback or
// private is not spoken to: every call returns an error.
func NewAdmin(addr netip.AddrPort, c Credentials) *Admin {
	return &Admin{base: "http://" + addr.String(), user: UserAdmin, pass: c.Admin, bad: checkReach(addr),
		hc: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// checkReach is the rule for every address this package connects to.
func checkReach(addr netip.AddrPort) error {
	if !addr.IsValid() || addr.Port() == 0 {
		return fmt.Errorf("rqlite: %v is not an address and a port", addr)
	}
	if a := addr.Addr().Unmap(); !a.IsLoopback() && !a.IsPrivate() {
		return fmt.Errorf("rqlite: %s is not on loopback or a private network: the database is only reached inside the workspace's own network", addr)
	}
	return nil
}

// Node is one member of the cluster as a node reports it.
type Node struct {
	ID        string `json:"id"`
	API       string `json:"api_addr"`
	Raft      string `json:"addr"`
	Version   string `json:"version"`
	Voter     bool   `json:"voter"`
	Reachable bool   `json:"reachable"`
	Leader    bool   `json:"leader"`
	Error     string `json:"error,omitempty"`
}

// Health is what one node says about itself.
type Health struct {
	NodeID   string `json:"nodeId"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	State    string `json:"state"` // Leader, Follower, Candidate, ...
	Voter    bool   `json:"voter"`
	LeaderID string `json:"leaderId"`
	Ready    bool   `json:"ready"`
	// CommitIndex is the highest Raft log entry the node knows is committed; AppliedIndex the highest it has
	// applied to its database. A node that has caught up has applied what it knows is committed.
	CommitIndex  uint64 `json:"commitIndex"`
	AppliedIndex uint64 `json:"appliedIndex"`
	// Members are the nodes in the node's own view of the Raft configuration.
	Members []Member `json:"members"`
}

// Member is a node in the Raft configuration.
type Member struct {
	ID     string `json:"id"`
	Addr   string `json:"addr"`
	Voter  bool   `json:"voter"`
	Leader bool   `json:"leader,omitempty"`
}

// ErrNoLeader: the cluster has no leader the node can reach, which is what losing quorum looks like.
var ErrNoLeader = errors.New("rqlite: the cluster has no leader")

func (a *Admin) do(ctx context.Context, method, path string, body io.Reader, want ...int) ([]byte, int, error) {
	if a.bad != nil {
		return nil, 0, a.bad
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(a.user, a.pass)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if len(want) == 0 {
		want = []int{http.StatusOK}
	}
	for _, w := range want {
		if res.StatusCode == w {
			return b, res.StatusCode, nil
		}
	}
	msg := strings.TrimSpace(string(b))
	if res.StatusCode == http.StatusServiceUnavailable && strings.Contains(msg, "leader") {
		return b, res.StatusCode, ErrNoLeader
	}
	return b, res.StatusCode, fmt.Errorf("rqlite: %s %s: %s: %s", method, path, res.Status, msg)
}

// Alive reports whether the node's HTTP API answers (and the node is up), which says nothing about
// the cluster.
func (a *Admin) Alive(ctx context.Context) error {
	_, _, err := a.do(ctx, http.MethodGet, "/readyz?noleader", nil)
	return err
}

// Ready reports whether the node is part of a cluster that has a leader it can reach and has its
// database open. With sync it also waits until the node has applied everything the leader has
// committed, which is what "caught up" means.
func (a *Admin) Ready(ctx context.Context, sync bool) error {
	q := "/readyz"
	if sync {
		q += "?sync&timeout=10s"
	}
	_, code, err := a.do(ctx, http.MethodGet, q, nil, http.StatusOK, http.StatusServiceUnavailable)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return ErrNoLeader
	}
	return nil
}

// Nodes lists the cluster's members as this node sees them, read-only replicas included, sorted by ID.
func (a *Admin) Nodes(ctx context.Context) ([]Node, error) {
	b, _, err := a.do(ctx, http.MethodGet, "/nodes?nonvoters&timeout=3s", nil)
	if err != nil {
		return nil, err
	}
	m := map[string]Node{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("rqlite: the node list: %w", err)
	}
	out := make([]Node, 0, len(m))
	for id, n := range m {
		n.ID = id
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Health is what the node reports about itself.
func (a *Admin) Health(ctx context.Context) (Health, error) {
	b, _, err := a.do(ctx, http.MethodGet, "/status", nil)
	if err != nil {
		return Health{}, err
	}
	var raw struct {
		Build struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		} `json:"build"`
		Store struct {
			NodeID string `json:"node_id"`
			Ready  bool   `json:"ready"`
			Leader struct {
				NodeID string `json:"node_id"`
			} `json:"leader"`
			Nodes []struct {
				ID       string `json:"id"`
				Addr     string `json:"addr"`
				Suffrage string `json:"suffrage"`
			} `json:"nodes"`
			Raft struct {
				State        string `json:"state"`
				Voter        bool   `json:"voter"`
				CommitIndex  uint64 `json:"commit_index"`
				AppliedIndex uint64 `json:"applied_index"`
			} `json:"raft"`
			FSMIndex uint64 `json:"fsm_index"`
		} `json:"store"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Health{}, fmt.Errorf("rqlite: the node's status: %w", err)
	}
	h := Health{NodeID: raw.Store.NodeID, Version: raw.Build.Version, Commit: raw.Build.Commit, State: raw.Store.Raft.State, Voter: raw.Store.Raft.Voter,
		LeaderID: raw.Store.Leader.NodeID, Ready: raw.Store.Ready, CommitIndex: raw.Store.Raft.CommitIndex, AppliedIndex: raw.Store.FSMIndex}
	if h.AppliedIndex == 0 {
		h.AppliedIndex = raw.Store.Raft.AppliedIndex
	}
	for _, n := range raw.Store.Nodes {
		h.Members = append(h.Members, Member{ID: n.ID, Addr: n.Addr, Voter: n.Suffrage == "voter", Leader: n.ID == h.LeaderID})
	}
	return h, nil
}

// Leader returns the node the cluster currently has as leader.
func (a *Admin) Leader(ctx context.Context) (Node, error) {
	b, _, err := a.do(ctx, http.MethodGet, "/leader?timeout=3s", nil)
	if err != nil {
		return Node{}, err
	}
	var n Node
	if err := json.Unmarshal(b, &n); err != nil {
		return Node{}, fmt.Errorf("rqlite: the leader: %w", err)
	}
	return n, nil
}

// StepDown moves leadership away from the leader, to the node with the given ID if one is named,
// and waits for the change. It is how a leader is made ready to be removed or stopped for a long time.
func (a *Admin) StepDown(ctx context.Context, toID string) error {
	var body io.Reader
	if toID != "" {
		body = strings.NewReader(`{"id":` + strconv.Quote(toID) + `}`)
	}
	_, _, err := a.do(ctx, http.MethodPost, "/leader?wait=true&timeout=10s", body)
	return err
}

// Remove takes a node out of the cluster's Raft configuration with rqlite's own membership change.
// It is the supported way to drop a member; it does not check that quorum survives (that is the
// caller's job, see CheckRemoval) and never touches the node's own files.
func (a *Admin) Remove(ctx context.Context, id string) error {
	if !nodeIDRE.MatchString(id) {
		return fmt.Errorf("rqlite: node ID %q is not one", id)
	}
	_, _, err := a.do(ctx, http.MethodDelete, "/remove?timeout=15s", strings.NewReader(`{"id":`+strconv.Quote(id)+`}`))
	return err
}
