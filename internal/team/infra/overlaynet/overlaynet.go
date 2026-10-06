// Package overlaynet is the workspace's private network as a transport.Transport:
// the production implementation of the contract in internal/transport for a Team
// deployment. Code written against that contract (the routing of authenticated
// messages between devices, when it is built) reaches the network through it and
// never names a network program, a lighthouse, a certificate or an address plan.
//
// It is thin on purpose. The network program is a supervised process that gives this
// machine an interface and an address inside the workspace's range; this package
// makes that address a transport: Listen binds to it (so nothing it accepts can come
// from outside the network), Dial leaves from it and refuses any destination outside
// the workspace's range (so the transport cannot be used to reach the Internet or a
// LAN), and the peers are the workspace's own devices, taken from its registry. What
// the program decides is not repeated here: whether a peer may be reached is the
// network's firewall's answer, and whether a request is allowed is the application's.
//
// Nothing here knows a particular program. Node is whatever starts and stops it
// (internal/team/infra/nebula's supervisor, adapted by the server), Stack is how
// connections are made (the operating system's, in production), and the peers come
// from a function.
package overlaynet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"devboard/internal/transport"
)

// State is what a Node reports.
type State string

const (
	StateStopped    State = "stopped"
	StateStarting   State = "starting"
	StateRunning    State = "running"
	StateRestarting State = "restarting"
	StateFailed     State = "failed"
)

// NodeState is a Node's state and, when it is not running, why.
type NodeState struct {
	State  State
	Detail string
}

// Node starts, stops and describes the network program on this machine.
type Node interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	State() NodeState
}

// Stack makes connections on the private network.
type Stack interface {
	// Present says whether the local address exists yet (the interface is up).
	Present(local netip.Addr) bool
	// Listen accepts connections to local:port.
	Listen(ctx context.Context, local netip.Addr, port int) (net.Listener, error)
	// Dial connects to address ("host:port") from local.
	Dial(ctx context.Context, local netip.Addr, address string) (net.Conn, error)
}

// Peer is another device of the workspace.
type Peer struct {
	ID   transport.NodeID
	Name string
	Addr netip.Addr
	// LastSeen is when the workspace last heard from the device (zero if never).
	LastSeen time.Time
}

// Config configures a Transport.
type Config struct {
	Node Node
	// Self is this device, and Network the workspace's address range.
	Self    transport.NodeInfo
	Network netip.Prefix
	Stack   Stack
	// Peers lists the workspace's other devices.
	Peers func(ctx context.Context) ([]Peer, error)
	// PeerWindow is how recently a device must have been seen to count as reachable.
	PeerWindow time.Duration
	// Now is the clock (tests).
	Now func() time.Time
}

// Transport implements transport.Transport on the workspace's private network.
type Transport struct {
	c Config

	mu        sync.Mutex
	started   bool
	listeners map[*listener]struct{}
}

var _ transport.Transport = (*Transport)(nil)

// New builds a Transport. It validates the configuration, so that a mistake is found
// when the server starts and not when something first dials.
func New(c Config) (*Transport, error) {
	switch {
	case c.Node == nil || c.Stack == nil || c.Peers == nil:
		return nil, errors.New("overlaynet: a node, a stack and a source of peers are required")
	case len(c.Self.Addrs) != 1 || !c.Network.IsValid() || !c.Network.Contains(c.Self.Addrs[0]):
		return nil, errors.New("overlaynet: this device needs exactly one address, inside the workspace's range")
	case c.Self.ID == "":
		return nil, errors.New("overlaynet: this device needs an identity")
	}
	if c.PeerWindow == 0 {
		c.PeerWindow = 2 * time.Minute
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Transport{c: c, listeners: map[*listener]struct{}{}}, nil
}

// Start implements transport.Transport. It starts the node and returns; Status says when it is on the network.
func (t *Transport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}
	if err := t.c.Node.Start(ctx); err != nil {
		return err
	}
	t.started = true
	return nil
}

// Stop implements transport.Transport: it closes every listener, then stops the node.
func (t *Transport) Stop(ctx context.Context) error {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()
		return nil
	}
	t.started = false
	ls := t.listeners
	t.listeners = map[*listener]struct{}{}
	t.mu.Unlock()
	for l := range ls {
		_ = l.Listener.Close()
	}
	return t.c.Node.Stop(ctx)
}

func (t *Transport) isStarted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.started
}

// Status implements transport.Transport.
func (t *Transport) Status(context.Context) (transport.Status, error) {
	if !t.isStarted() {
		return transport.Status{State: transport.StateStopped}, nil
	}
	ns := t.c.Node.State()
	st := transport.Status{Node: t.c.Self, Detail: ns.Detail}
	switch {
	case ns.State == StateFailed:
		st.State = transport.StateFailed
	case ns.State == StateRunning && t.c.Stack.Present(t.c.Self.Addrs[0]):
		st.State = transport.StateReady
	case ns.State == StateRunning:
		st.State, st.Detail = transport.StateStarting, "the network interface is not up yet"
	default:
		st.State = transport.StateStarting
	}
	return st, nil
}

func (t *Transport) ready() error {
	if !t.isStarted() {
		return transport.ErrNotStarted
	}
	if st, _ := t.Status(context.Background()); st.State != transport.StateReady {
		return transport.ErrNotReady
	}
	return nil
}

// Local implements transport.Transport.
func (t *Transport) Local(context.Context) (transport.NodeInfo, error) {
	if !t.isStarted() {
		return transport.NodeInfo{}, transport.ErrNotStarted
	}
	return t.c.Self, nil
}

// Listen implements transport.Transport: it accepts only on this device's own address on the network.
func (t *Transport) Listen(ctx context.Context, addr string) (net.Listener, error) {
	if err := t.ready(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(addr)
	p, perr := strconv.Atoi(port)
	if err != nil || perr != nil || p < 1 || p > 65535 || (host != "" && host != t.c.Self.Addrs[0].String()) {
		return nil, fmt.Errorf("overlaynet: %q: a listener is \":port\" on this device's address", addr)
	}
	ln, err := t.c.Stack.Listen(ctx, t.c.Self.Addrs[0], p)
	if err != nil {
		return nil, err
	}
	l := &listener{Listener: ln, t: t}
	t.mu.Lock()
	t.listeners[l] = struct{}{}
	t.mu.Unlock()
	return l, nil
}

type listener struct {
	net.Listener
	t *Transport
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, t: l.t, local: l.t.c.Self.Addrs[0].String()}, nil
}

func (l *listener) Close() error {
	l.t.mu.Lock()
	delete(l.t.listeners, l)
	l.t.mu.Unlock()
	return l.Listener.Close()
}

// Dial implements transport.Transport. A destination outside the workspace's range is refused: this is a
// transport for the workspace's private network and for nothing else.
func (t *Transport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if err := t.ready(); err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("overlaynet: %q is not host:port", addr)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !t.c.Network.Contains(ip) {
		return nil, fmt.Errorf("overlaynet: %q is not on the workspace's private network (%v)", addr, t.c.Network)
	}
	c, err := t.c.Stack.Dial(ctx, t.c.Self.Addrs[0], addr)
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, t: t, local: t.c.Self.Addrs[0].String(), dialled: ip.String()}, nil
}

// conn is a net.Conn that can describe itself.
type conn struct {
	net.Conn
	t       *Transport
	local   string
	dialled string
}

var _ transport.Conn = (*conn)(nil)

// Meta implements transport.Conn. The path is not known to a transport that leaves the choice of route to the
// network program, and says so; the connection is always encrypted and authenticated by the network, which is
// all Encrypted claims.
func (c *conn) Meta() transport.ConnMeta {
	remote := c.dialled
	if remote == "" {
		if a, ok := c.Conn.RemoteAddr().(*net.TCPAddr); ok {
			remote = a.IP.String()
		}
	}
	m := transport.ConnMeta{LocalAddr: c.local, RemoteAddr: remote, Path: transport.PathUnknown, Encrypted: true}
	if peers, err := c.t.c.Peers(context.Background()); err == nil {
		for _, p := range peers {
			if p.Addr.String() == remote {
				m.RemoteNode = p.ID
			}
		}
	}
	return m
}

// Peers implements transport.Transport.
func (t *Transport) Peers(ctx context.Context) ([]transport.PeerStatus, error) {
	if !t.isStarted() {
		return nil, transport.ErrNotStarted
	}
	peers, err := t.c.Peers(ctx)
	if err != nil {
		return nil, err
	}
	now := t.c.Now()
	out := make([]transport.PeerStatus, 0, len(peers))
	for _, p := range peers {
		if p.ID == t.c.Self.ID {
			continue
		}
		out = append(out, transport.PeerStatus{
			Node:      transport.NodeInfo{ID: p.ID, Name: p.Name, Addrs: []netip.Addr{p.Addr}},
			Reachable: !p.LastSeen.IsZero() && now.Sub(p.LastSeen) <= t.c.PeerWindow,
			Path:      transport.PathUnknown,
			LastSeen:  p.LastSeen,
		})
	}
	return out, nil
}

// OSStack makes connections with the operating system's own network stack, which the
// network program's interface is part of.
type OSStack struct{}

var _ Stack = OSStack{}

// Present implements Stack.
func (OSStack) Present(local netip.Addr) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr() == local {
			return true
		}
	}
	return false
}

// Listen implements Stack.
func (OSStack) Listen(ctx context.Context, local netip.Addr, port int) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", net.JoinHostPort(local.String(), strconv.Itoa(port)))
}

// Dial implements Stack: the connection leaves from this device's own address on the network.
func (OSStack) Dial(ctx context.Context, local netip.Addr, address string) (net.Conn, error) {
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: local.AsSlice()}, Timeout: 15 * time.Second}
	return d.DialContext(ctx, "tcp", address)
}
