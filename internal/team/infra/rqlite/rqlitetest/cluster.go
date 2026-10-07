//go:build !windows

package rqlitetest

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"sync"
	"syscall"
	"time"

	"devboard/internal/team/infra/rqlite"
)

// TB is the part of testing.TB the harness uses, so that a TestMain, which has no *testing.T, can start a cluster too
// (MainTB).
type TB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Cleanup(func())
	Skip(args ...any)
	Skipf(format string, args ...any)
}

// Options shape a test cluster.
type Options struct {
	// Nodes is how many voters to start (default 3).
	Nodes int
	// ElectionTimeout and HeartbeatTimeout are Raft's; the defaults (600ms) make a failover take about a
	// second instead of several.
	ElectionTimeout, HeartbeatTimeout time.Duration
	// Owner tells which process owns the other end of a connection a node made to another (the process ID, or 0), so
	// that a partition can tell whose connection it is when the connection does not say. The harness itself starts
	// nothing and asks no one: a test that cuts the network supplies it (from lsof, say).
	Owner func(remote net.Addr) int
}

// Cluster is a set of real rqlite nodes on loopback.
type Cluster struct {
	tb    TB
	Creds rqlite.Credentials
	opts  Options
	bin   string

	mu    sync.Mutex
	nodes []*Node
	cut   map[[2]int]bool
	pairs map[[2]int]*pairProxy

	traceMu sync.Mutex
	trace   []string
}

// tracef records what the harness decided about a connection or a partition, with the time, so that a test that sees
// something it should not have can say what happened (Trace). RQT_DEBUG also prints each line as it is recorded.
func (c *Cluster) tracef(format string, a ...any) {
	line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, a...)
	c.traceMu.Lock()
	if len(c.trace) >= 600 {
		c.trace = c.trace[1:]
	}
	c.trace = append(c.trace, line)
	c.traceMu.Unlock()
	if os.Getenv("RQT_DEBUG") != "" {
		fmt.Fprintln(os.Stderr, line)
	}
}

// Trace is what the harness recorded about connections and partitions, oldest first.
func (c *Cluster) Trace() []string {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	return append([]string(nil), c.trace...)
}

// Node is one of the cluster's nodes.
type Node struct {
	c        *Cluster
	Index    int
	ID       string
	HTTP     netip.AddrPort
	RaftBind netip.AddrPort
	// RaftAdv is the address the others use: the proxy's.
	RaftAdv  netip.AddrPort
	Dir      string
	NonVoter bool
	Sup      *rqlite.Supervisor
	proxy    *raftProxy
	cfg      rqlite.NodeConfig
}

// FreePort is a TCP port on loopback that nothing is listening on.
func FreePort(tb TB) int { return freePort(tb) }

func freePort(tb TB) int {
	tb.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// New starts a cluster of n voters and waits for it to have a leader and every member.
func New(tb TB, opts Options) *Cluster {
	tb.Helper()
	if opts.Nodes == 0 {
		opts.Nodes = 3
	}
	if opts.ElectionTimeout == 0 {
		opts.ElectionTimeout = 600 * time.Millisecond
	}
	if opts.HeartbeatTimeout == 0 {
		opts.HeartbeatTimeout = 600 * time.Millisecond
	}
	creds, err := rqlite.NewCredentials()
	if err != nil {
		tb.Fatal(err)
	}
	c := &Cluster{tb: tb, Creds: creds, opts: opts, bin: BinaryDir(tb), cut: map[[2]int]bool{}, pairs: map[[2]int]*pairProxy{}}
	tb.Cleanup(c.Close)
	for i := 0; i < opts.Nodes; i++ {
		c.Add(false)
	}
	c.WaitMembers(30*time.Second, opts.Nodes)
	return c
}

// Add starts one more node, joining the cluster (the first node starts it). It returns when the node is
// part of it and has caught up.
func (c *Cluster) Add(nonVoter bool) *Node {
	c.tb.Helper()
	c.mu.Lock()
	idx := len(c.nodes)
	n := &Node{c: c, Index: idx, ID: fmt.Sprintf("host-%d", idx+1), NonVoter: nonVoter}
	ip := netip.MustParseAddr("127.0.0.1")
	n.HTTP = netip.AddrPortFrom(ip, uint16(freePort(c.tb)))
	n.RaftBind = netip.AddrPortFrom(ip, uint16(freePort(c.tb)))
	n.RaftAdv = netip.AddrPortFrom(ip, uint16(freePort(c.tb)))
	var join []netip.AddrPort
	for _, o := range c.nodes {
		join = append(join, o.RaftAdv)
	}
	c.nodes = append(c.nodes, n)
	c.mu.Unlock()

	var err error
	if n.proxy, err = newRaftProxy(c, idx, n.RaftAdv.String(), n.RaftBind.String()); err != nil {
		c.tb.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "rqt-")
	if err != nil {
		c.tb.Fatal(err)
	}
	c.tb.Cleanup(func() { _ = os.RemoveAll(dir) })
	n.Dir = dir
	n.Sup, err = rqlite.New(rqlite.Options{BinaryDirs: []string{c.bin}, NoRestart: true, StopTimeout: 20 * time.Second, StartTimeout: 40 * time.Second})
	if err != nil {
		c.tb.Fatal(err)
	}
	n.cfg = rqlite.NodeConfig{DataDir: dir, NodeID: n.ID, HTTPAddr: n.HTTP, RaftAddr: n.RaftBind, RaftAdvAddr: n.RaftAdv, Join: join, NonVoter: nonVoter,
		Credentials: c.Creds, ElectionTimeout: c.opts.ElectionTimeout, HeartbeatTimeout: c.opts.HeartbeatTimeout}
	n.Start()
	return n
}

// Nodes returns the nodes started so far.
func (c *Cluster) Nodes() []*Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*Node(nil), c.nodes...)
}

// Node returns the i-th node.
func (c *Cluster) Node(i int) *Node { return c.Nodes()[i] }

// Addrs are the HTTP addresses of every node, as the storage layer is given them.
func (c *Cluster) Addrs() []string {
	var out []string
	for _, n := range c.Nodes() {
		out = append(out, n.HTTP.String())
	}
	return out
}

// ClientAddrs are the HTTP addresses of the nodes as host i's storage layer reaches them: its own node directly,
// and every other node through a proxy that a partition cuts, so that a host that is cut off from the others cannot
// reach their APIs either. (A host reaches its own node whatever the network does.)
func (c *Cluster) ClientAddrs(i int) []string {
	c.tb.Helper()
	nodes := c.Nodes()
	out := []string{nodes[i].HTTP.String()}
	for j, n := range nodes {
		if j == i {
			continue
		}
		c.mu.Lock()
		p := c.pairs[[2]int{i, j}]
		c.mu.Unlock()
		if p == nil {
			var err error
			if p, err = newPairProxy(c, i, j, n.HTTP.String()); err != nil {
				c.tb.Fatal(err)
			}
			c.mu.Lock()
			c.pairs[[2]int{i, j}] = p
			c.mu.Unlock()
		}
		out = append(out, p.addr)
	}
	return out
}

// Close stops every node and proxy.
func (c *Cluster) Close() {
	c.mu.Lock()
	for _, p := range c.pairs {
		p.close()
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, n := range c.Nodes() {
		_ = n.Sup.Stop(ctx)
		if n.proxy != nil {
			n.proxy.close()
		}
	}
}

func (c *Cluster) nodeOfPID(pid int) int {
	for _, n := range c.Nodes() {
		if n.Sup.Status().PID == pid {
			return n.Index
		}
	}
	return -1
}

func (c *Cluster) blocked(src, dst int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cut[[2]int{src, dst}]
}

// refuses reports whether a connection to dst from src, a caller that has been told apart, must be refused now: it is cut
// off from dst. A caller that could not be told apart (src < 0) is not refused: refusing those starved the healthy
// side of a partition too, because the nodes' pooled requests to each other do not say who sends them. cut() closes
// the ones the proxy holds when a partition begins instead.
func (c *Cluster) refuses(src, dst int) bool {
	if src < 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cut[[2]int{src, dst}]
}

// whoSent tells which node sent the first bytes of a connection.
func (c *Cluster) whoSent(first []byte) int {
	nodes := c.Nodes()
	addrs := make([]string, len(nodes))
	for i, n := range nodes {
		addrs[i] = n.RaftAdv.String()
	}
	return sniff(first, addrs)
}

// Partition cuts the network between every pair of nodes that are not in the same group (a node named in no
// group is cut off from all of them), in both directions, and closes the connections that crossed it.
func (c *Cluster) Partition(groups ...[]int) {
	c.tb.Helper()
	group := map[int]int{}
	for g, members := range groups {
		for _, m := range members {
			group[m] = g
		}
	}
	c.mu.Lock()
	c.cut = map[[2]int]bool{}
	nodes := append([]*Node(nil), c.nodes...)
	for _, a := range nodes {
		for _, b := range nodes {
			if a.Index == b.Index {
				continue
			}
			ga, okA := group[a.Index]
			gb, okB := group[b.Index]
			if !okA || !okB || ga != gb {
				c.cut[[2]int{a.Index, b.Index}] = true
			}
		}
	}
	cutNow := len(c.cut)
	c.mu.Unlock()
	c.tracef("partition begins: %d directed pairs cut", cutNow)
	for _, n := range nodes {
		n.proxy.cut()
	}
	c.mu.Lock()
	pairs := make([]*pairProxy, 0, len(c.pairs))
	for _, p := range c.pairs {
		pairs = append(pairs, p)
	}
	c.mu.Unlock()
	for _, p := range pairs {
		p.sync()
	}
}

// Isolate cuts one node off from every other.
func (c *Cluster) Isolate(i int) {
	var rest []int
	for _, n := range c.Nodes() {
		if n.Index != i {
			rest = append(rest, n.Index)
		}
	}
	c.Partition(rest, []int{i})
}

// Heal mends the network.
func (c *Cluster) Heal() {
	c.tracef("network mended")
	c.mu.Lock()
	c.cut = map[[2]int]bool{}
	pairs := make([]*pairProxy, 0, len(c.pairs))
	for _, p := range c.pairs {
		pairs = append(pairs, p)
	}
	c.mu.Unlock()
	for _, p := range pairs {
		p.sync()
	}
}

// Start starts the node (again), with the data it has.
func (n *Node) Start() {
	n.c.tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := n.Sup.Start(ctx, n.cfg); err != nil {
		n.c.tb.Fatalf("starting %s: %v", n.ID, err)
	}
	if n.c.hasCut() {
		return // across a partition a node may have no leader to be ready with
	}
	if err := n.Sup.WaitReady(ctx, true); err != nil {
		n.c.tb.Fatalf("%s did not become ready: %v\n%v", n.ID, err, n.Sup.Status().Tail)
	}
}

func (c *Cluster) hasCut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cut) > 0
}

// StartAll starts the given nodes (again) without waiting for any to be ready, and then waits for the cluster to
// have a leader: after an outage of the whole cluster no node is ready until enough of them are up.
func (c *Cluster) StartAll(nodes ...*Node) {
	c.tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, n := range nodes {
		if err := n.Sup.Start(ctx, n.cfg); err != nil {
			c.tb.Fatalf("starting %s: %v", n.ID, err)
		}
	}
	c.WaitLeader(60*time.Second, nodes...)
}

// Stop stops the node the way an operator does, gracefully.
func (n *Node) Stop() {
	n.c.tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := n.Sup.Stop(ctx); err != nil {
		n.c.tb.Fatalf("stopping %s: %v", n.ID, err)
	}
}

// Kill ends the node's program at once, without a chance to say goodbye: a crash, or a power cut.
func (n *Node) Kill() {
	n.c.tb.Helper()
	pid := n.Sup.Status().PID
	if pid == 0 {
		n.c.tb.Fatalf("%s is not running", n.ID)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		n.c.tb.Fatalf("killing %s: %v", n.ID, err)
	}
	// The supervisor notices, and (NoRestart) leaves it dead; release its bookkeeping so Start works again.
	deadline := time.Now().Add(10 * time.Second)
	for n.Sup.Status().PID != 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = n.Sup.Stop(ctx)
}

// Admin is a client for this node.
func (n *Node) Admin() *rqlite.Admin { return rqlite.NewAdmin(n.HTTP, n.c.Creds) }

// RaftAddr is the address the other nodes use for this one.
func (n *Node) RaftAddr() netip.AddrPort { return n.RaftAdv }

// Config is the configuration the node runs with.
func (n *Node) Config() rqlite.NodeConfig { return n.cfg }

// WaitMembers waits until every running node lists want members (voters and replicas), and some node leads.
func (c *Cluster) WaitMembers(timeout time.Duration, want int) {
	c.tb.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		ok := true
		for _, n := range c.Nodes() {
			if n.Sup.Status().State != rqlite.StateRunning {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			nodes, err := n.Admin().Nodes(ctx)
			cancel()
			if err != nil || len(nodes) != want {
				ok = false
				last = fmt.Sprintf("%s: %d nodes, %v", n.ID, len(nodes), err)
			}
		}
		if ok && c.Leader() != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	c.tb.Fatalf("the cluster did not have %d members and a leader within %s (%s)", want, timeout, last)
}

// Leader is the running node that currently says it leads, or nil.
func (c *Cluster) Leader() *Node {
	for _, n := range c.Nodes() {
		if n.Sup.Status().State != rqlite.StateRunning {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		h, err := n.Admin().Health(ctx)
		cancel()
		if err == nil && h.State == "Leader" {
			return n
		}
	}
	return nil
}

// WaitLeader waits for some node among those given (all, if none) to lead, and returns it.
func (c *Cluster) WaitLeader(timeout time.Duration, among ...*Node) *Node {
	c.tb.Helper()
	if len(among) == 0 {
		among = c.Nodes()
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range among {
			if n.Sup.Status().State != rqlite.StateRunning {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			h, err := n.Admin().Health(ctx)
			cancel()
			if err == nil && h.State == "Leader" {
				return n
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	c.tb.Fatalf("no leader among the given nodes within %s", timeout)
	return nil
}

// IDs lists the nodes' IDs, sorted.
func (c *Cluster) IDs() []string {
	var out []string
	for _, n := range c.Nodes() {
		out = append(out, n.ID)
	}
	sort.Strings(out)
	return out
}
