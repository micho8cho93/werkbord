// Package memtransport is an in-process private network for tests: nodes that
// start, listen, dial and see each other, without a socket or a network. It
// implements transport.Transport and is what the contract's own tests, and the
// tests of code that depends on the contract, run against.
package memtransport

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"devboard/internal/transport"
)

// Network is a set of nodes that can reach one another.
type Network struct {
	mu    sync.Mutex
	next  byte
	nodes map[netip.Addr]*Node
}

// NewNetwork returns an empty network.
func NewNetwork() *Network { return &Network{nodes: map[netip.Addr]*Node{}} }

// Node is one transport on a Network.
type Node struct {
	net  *Network
	id   transport.NodeID
	name string
	addr netip.Addr

	mu        sync.Mutex
	started   bool
	listeners map[int]*listener
	cut       map[transport.NodeID]bool
}

// NewNode adds a stopped node to the network.
func (n *Network) NewNode(name string) *Node {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.next++
	addr := netip.AddrFrom4([4]byte{100, 64, 0, n.next})
	return &Node{net: n, id: transport.NodeID("mem-" + addr.String()), name: name, addr: addr,
		listeners: map[int]*listener{}, cut: map[transport.NodeID]bool{}}
}

var _ transport.Transport = (*Node)(nil)

func (m *Node) info() transport.NodeInfo {
	return transport.NodeInfo{ID: m.id, Name: m.name, Addrs: []netip.Addr{m.addr}}
}

// Start implements transport.Transport.
func (m *Node) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	m.started = true
	m.net.mu.Lock()
	m.net.nodes[m.addr] = m
	m.net.mu.Unlock()
	return nil
}

// Stop implements transport.Transport.
func (m *Node) Stop(context.Context) error {
	m.mu.Lock()
	ls := m.listeners
	m.listeners, m.started = map[int]*listener{}, false
	m.mu.Unlock()
	for _, l := range ls {
		_ = l.Close()
	}
	m.net.mu.Lock()
	delete(m.net.nodes, m.addr)
	m.net.mu.Unlock()
	return nil
}

// Status implements transport.Transport.
func (m *Node) Status(context.Context) (transport.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return transport.Status{State: transport.StateStopped}, nil
	}
	return transport.Status{State: transport.StateReady, Node: m.info()}, nil
}

// Local implements transport.Transport.
func (m *Node) Local(context.Context) (transport.NodeInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return transport.NodeInfo{}, transport.ErrNotStarted
	}
	return m.info(), nil
}

// Cut makes the given node unreachable from this one, and this one from it.
func (m *Node) Cut(other *Node) {
	m.mu.Lock()
	m.cut[other.id] = true
	m.mu.Unlock()
	other.mu.Lock()
	other.cut[m.id] = true
	other.mu.Unlock()
}

// Heal undoes Cut.
func (m *Node) Heal(other *Node) {
	m.mu.Lock()
	delete(m.cut, other.id)
	m.mu.Unlock()
	other.mu.Lock()
	delete(other.cut, m.id)
	other.mu.Unlock()
}

func port(addr string) (int, error) {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		return 0, fmt.Errorf("invalid port in %q", addr)
	}
	return n, nil
}

// Listen implements transport.Transport.
func (m *Node) Listen(_ context.Context, addr string) (net.Listener, error) {
	p, err := port(addr)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return nil, transport.ErrNotStarted
	}
	if _, taken := m.listeners[p]; taken {
		return nil, fmt.Errorf("memtransport: %s: address already in use", addr)
	}
	l := &listener{node: m, port: p, conns: make(chan net.Conn), done: make(chan struct{})}
	m.listeners[p] = l
	return l, nil
}

// Dial implements transport.Transport.
func (m *Node) Dial(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	p, err := port(addr)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil, fmt.Errorf("memtransport: %q is not an address on this network", host)
	}
	m.mu.Lock()
	started, blocked := m.started, false
	m.mu.Unlock()
	if !started {
		return nil, transport.ErrNotStarted
	}
	m.net.mu.Lock()
	peer := m.net.nodes[ip]
	m.net.mu.Unlock()
	if peer != nil {
		m.mu.Lock()
		blocked = m.cut[peer.id]
		m.mu.Unlock()
	}
	if peer == nil || blocked {
		return nil, fmt.Errorf("memtransport: no route to %s", addr)
	}
	peer.mu.Lock()
	l := peer.listeners[p]
	peer.mu.Unlock()
	if l == nil {
		return nil, fmt.Errorf("memtransport: connection to %s refused", addr)
	}
	a, b := net.Pipe()
	meta := func(local, remote *Node) transport.ConnMeta {
		return transport.ConnMeta{LocalAddr: net.JoinHostPort(local.addr.String(), strconv.Itoa(p)), RemoteAddr: net.JoinHostPort(remote.addr.String(), strconv.Itoa(p)),
			RemoteNode: remote.id, Path: transport.PathDirect, Encrypted: true}
	}
	ca, cb := &conn{Conn: a, meta: meta(m, peer)}, &conn{Conn: b, meta: meta(peer, m)}
	select {
	case l.conns <- cb:
		return ca, nil
	case <-l.done:
		return nil, fmt.Errorf("memtransport: connection to %s refused", addr)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Peers implements transport.Transport.
func (m *Node) Peers(context.Context) ([]transport.PeerStatus, error) {
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()
	if !started {
		return nil, transport.ErrNotStarted
	}
	m.net.mu.Lock()
	var others []*Node
	for _, n := range m.net.nodes {
		if n != m {
			others = append(others, n)
		}
	}
	m.net.mu.Unlock()
	out := make([]transport.PeerStatus, 0, len(others))
	for _, n := range others {
		m.mu.Lock()
		blocked := m.cut[n.id]
		m.mu.Unlock()
		ps := transport.PeerStatus{Node: n.info(), Reachable: !blocked}
		if !blocked {
			ps.Path, ps.LastSeen = transport.PathDirect, time.Now()
		}
		out = append(out, ps)
	}
	return out, nil
}

type conn struct {
	net.Conn
	meta transport.ConnMeta
}

func (c *conn) Meta() transport.ConnMeta { return c.meta }

type listener struct {
	node  *Node
	port  int
	conns chan net.Conn
	once  sync.Once
	done  chan struct{}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error {
	l.once.Do(func() {
		close(l.done)
		l.node.mu.Lock()
		if l.node.listeners[l.port] == l {
			delete(l.node.listeners, l.port)
		}
		l.node.mu.Unlock()
	})
	return nil
}

func (l *listener) Addr() net.Addr {
	return &net.TCPAddr{IP: l.node.addr.AsSlice(), Port: l.port}
}
