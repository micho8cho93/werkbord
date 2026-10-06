package overlaynet

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"devboard/internal/transport"
	"devboard/internal/transport/transporttest"
)

// node is a stand-in for the supervised network program.
type node struct {
	mu sync.Mutex
	st NodeState
}

func (n *node) Start(context.Context) error { n.set(StateRunning, ""); return nil }
func (n *node) Stop(context.Context) error  { n.set(StateStopped, ""); return nil }
func (n *node) State() NodeState            { n.mu.Lock(); defer n.mu.Unlock(); return n.st }
func (n *node) set(s State, d string)       { n.mu.Lock(); n.st = NodeState{s, d}; n.mu.Unlock() }

// loopStack puts every device's overlay address on this computer's loopback: a test has no
// interface to create, but the transport's behaviour does not depend on how a connection is made.
type loopStack struct {
	mu      sync.Mutex
	present map[netip.Addr]bool
}

func (s *loopStack) Present(a netip.Addr) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.present[a] }
func (s *loopStack) Listen(ctx context.Context, _ netip.Addr, port int) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(port))
}
func (s *loopStack) Dial(ctx context.Context, _ netip.Addr, address string) (net.Conn, error) {
	_, port, _ := net.SplitHostPort(address)
	var d net.Dialer
	return d.DialContext(ctx, "tcp", "127.0.0.1:"+port)
}

var prefix = netip.MustParsePrefix("10.99.0.0/16")

func pair(t *testing.T) (*Transport, *Transport, *node, *node, *loopStack) {
	t.Helper()
	st := &loopStack{present: map[netip.Addr]bool{}}
	var ts [2]*Transport
	var ns [2]*node
	selves := []transport.NodeInfo{
		{ID: "dev_a", Name: "a", Addrs: []netip.Addr{netip.MustParseAddr("10.99.0.1")}},
		{ID: "dev_b", Name: "b", Addrs: []netip.Addr{netip.MustParseAddr("10.99.0.2")}},
	}
	for i := range ts {
		st.present[selves[i].Addrs[0]] = true
		ns[i] = &node{}
		other := selves[1-i]
		tr, err := New(Config{Node: ns[i], Self: selves[i], Network: prefix, Stack: st,
			Peers: func(context.Context) ([]Peer, error) {
				return []Peer{{ID: other.ID, Name: other.Name, Addr: other.Addrs[0], LastSeen: time.Now()}}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		ts[i] = tr
	}
	return ts[0], ts[1], ns[0], ns[1], st
}

// The same suite every transport passes: the in-memory one and the Tailscale one in the individual
// product, and now the workspace's own private network.
func TestItIsATransport(t *testing.T) {
	transporttest.Run(t, func(t *testing.T) (transport.Transport, transport.Transport) {
		a, b, _, _, _ := pair(t)
		return a, b
	}, 5*time.Second)
}

func TestItDialsNothingOutsideTheWorkspacesNetwork(t *testing.T) {
	a, b, _, _, _ := pair(t)
	ctx := context.Background()
	for _, tr := range []*Transport{a, b} {
		if err := tr.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer tr.Stop(ctx)
	}
	l, err := b.Listen(ctx, ":4100")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, addr := range []string{"127.0.0.1:4100", "192.168.1.5:80", "8.8.8.8:53", "example.org:443", "10.98.0.2:4100", "[::1]:4100", "10.99.0.2"} {
		if c, err := a.Dial(ctx, addr); err == nil {
			c.Close()
			t.Errorf("Dial(%s) succeeded: the transport reached something that is not on the workspace's private network", addr)
		}
	}
	c, err := a.Dial(ctx, "10.99.0.2:4100")
	if err != nil {
		t.Fatalf("Dial of a peer on the network: %v", err)
	}
	c.Close()
}

func TestItListensOnlyOnItsOwnAddress(t *testing.T) {
	a, _, _, _, _ := pair(t)
	ctx := context.Background()
	a.Start(ctx)
	defer a.Stop(ctx)
	for _, addr := range []string{"0.0.0.0:4100", "127.0.0.1:4100", "10.99.0.2:4100", "4100", ":0", ":70000", ":x"} {
		if l, err := a.Listen(ctx, addr); err == nil {
			l.Close()
			t.Errorf("Listen(%s) succeeded", addr)
		}
	}
	for _, addr := range []string{":4100", "10.99.0.1:4101"} {
		l, err := a.Listen(ctx, addr)
		if err != nil {
			t.Errorf("Listen(%s): %v", addr, err)
			continue
		}
		l.Close()
	}
}

func TestStatusFollowsTheNodeAndTheInterface(t *testing.T) {
	a, _, na, _, st := pair(t)
	ctx := context.Background()
	if s, _ := a.Status(ctx); s.State != transport.StateStopped {
		t.Errorf("before Start: %s", s.State)
	}
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)
	// The node runs but the interface has not appeared: not ready, and Dial and Listen say so.
	st.mu.Lock()
	st.present[netip.MustParseAddr("10.99.0.1")] = false
	st.mu.Unlock()
	if s, _ := a.Status(ctx); s.State != transport.StateStarting || s.Detail == "" {
		t.Errorf("without an interface: %+v", s)
	}
	if _, err := a.Listen(ctx, ":4100"); err != transport.ErrNotReady {
		t.Errorf("Listen before ready = %v", err)
	}
	if _, err := a.Dial(ctx, "10.99.0.2:1"); err != transport.ErrNotReady {
		t.Errorf("Dial before ready = %v", err)
	}
	st.mu.Lock()
	st.present[netip.MustParseAddr("10.99.0.1")] = true
	st.mu.Unlock()
	if s, _ := a.Status(ctx); s.State != transport.StateReady || s.Node.ID != "dev_a" {
		t.Errorf("with an interface: %+v", s)
	}
	na.set(StateFailed, "the node could not create its interface: see docs/TEAM_NETWORK.md")
	if s, _ := a.Status(ctx); s.State != transport.StateFailed || s.Detail == "" {
		t.Errorf("when the node failed: %+v", s)
	}
	na.set(StateRestarting, "")
	if s, _ := a.Status(ctx); s.State != transport.StateStarting {
		t.Errorf("while the node restarts: %+v", s)
	}
}

func TestPeersAreTheWorkspacesDevicesAndNotYourself(t *testing.T) {
	st := &loopStack{present: map[netip.Addr]bool{}}
	now := time.Now()
	self := transport.NodeInfo{ID: "dev_a", Addrs: []netip.Addr{netip.MustParseAddr("10.99.0.1")}}
	tr, err := New(Config{Node: &node{}, Self: self, Network: prefix, Stack: st, Now: func() time.Time { return now }, PeerWindow: time.Minute,
		Peers: func(context.Context) ([]Peer, error) {
			return []Peer{
				{ID: "dev_a", Addr: self.Addrs[0], LastSeen: now},
				{ID: "dev_b", Name: "b", Addr: netip.MustParseAddr("10.99.0.2"), LastSeen: now.Add(-10 * time.Second)},
				{ID: "dev_c", Name: "c", Addr: netip.MustParseAddr("10.99.0.3"), LastSeen: now.Add(-time.Hour)},
				{ID: "dev_d", Name: "d", Addr: netip.MustParseAddr("10.99.0.4")},
			}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Peers(context.Background()); err != transport.ErrNotStarted {
		t.Errorf("Peers before Start = %v", err)
	}
	tr.Start(context.Background())
	peers, err := tr.Peers(context.Background())
	if err != nil || len(peers) != 3 {
		t.Fatalf("peers = %+v, %v", peers, err)
	}
	want := map[transport.NodeID]bool{"dev_b": true, "dev_c": false, "dev_d": false}
	for _, p := range peers {
		if p.Reachable != want[p.Node.ID] || p.Path != transport.PathUnknown {
			t.Errorf("%s: reachable=%v path=%q", p.Node.ID, p.Reachable, p.Path)
		}
	}
}

func TestItRefusesAMisconfiguration(t *testing.T) {
	self := transport.NodeInfo{ID: "x", Addrs: []netip.Addr{netip.MustParseAddr("10.99.0.1")}}
	peers := func(context.Context) ([]Peer, error) { return nil, nil }
	st := &loopStack{}
	for name, c := range map[string]Config{
		"no node":            {Self: self, Network: prefix, Stack: st, Peers: peers},
		"no stack":           {Node: &node{}, Self: self, Network: prefix, Peers: peers},
		"no peers":           {Node: &node{}, Self: self, Network: prefix, Stack: st},
		"an address outside": {Node: &node{}, Self: transport.NodeInfo{ID: "x", Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.1")}}, Network: prefix, Stack: st, Peers: peers},
		"two addresses":      {Node: &node{}, Self: transport.NodeInfo{ID: "x", Addrs: []netip.Addr{self.Addrs[0], netip.MustParseAddr("10.99.0.2")}}, Network: prefix, Stack: st, Peers: peers},
		"no identity":        {Node: &node{}, Self: transport.NodeInfo{Addrs: self.Addrs}, Network: prefix, Stack: st, Peers: peers},
		"no network":         {Node: &node{}, Self: self, Stack: st, Peers: peers},
	} {
		if _, err := New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
