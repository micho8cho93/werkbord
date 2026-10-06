package netprivate

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"devboard/internal/transport"
)

// PeerBackend is what a Backend needs beyond Start/Status/Listen/Close to be a
// transport.Transport: dialling, and knowing the other nodes. The embedded
// Tailscale node has all of it.
type PeerBackend interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
	Peers(ctx context.Context) ([]BackendPeer, error)
	// WhoIs names the node at the other end of a connection by its address.
	WhoIs(ctx context.Context, remoteAddr string) (BackendPeer, bool)
}

// BackendPeer is another node, as the backend knows it.
type BackendPeer struct {
	ID      string
	Name    string
	IPs     []netip.Addr
	Online  bool
	Direct  bool
	Relayed bool
	// LastSeen is when the node was last seen; zero if it is online or unknown.
	LastSeen time.Time
}

// NewTransport adapts a Backend to the product-neutral transport.Transport. The
// Tailscale vocabulary stops here: states, sign-in links and tailnet names become
// the contract's, and nothing above it needs to know which network this is.
//
// A backend that is not also a PeerBackend can start, listen and report status
// but cannot dial or list peers (transport.ErrUnsupported).
func NewTransport(b Backend) transport.Transport { return &adapter{b: b} }

// NewTailscaleTransport is the embedded Tailscale node as a transport.Transport.
func NewTailscaleTransport(o TailscaleOptions) transport.Transport {
	return NewTransport(NewTailscale(o))
}

type adapter struct {
	b Backend

	mu        sync.Mutex
	started   bool
	startErr  error
	listeners map[*listener]struct{}
}

func (a *adapter) peers() (PeerBackend, bool) { p, ok := a.b.(PeerBackend); return p, ok }

func (a *adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return nil
	}
	if err := a.b.Start(ctx); err != nil {
		a.startErr = err
		return err
	}
	a.started, a.startErr = true, nil
	a.listeners = map[*listener]struct{}{}
	return nil
}

func (a *adapter) Stop(context.Context) error {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return nil
	}
	ls := a.listeners
	a.listeners, a.started = nil, false
	a.mu.Unlock()
	var err error
	for l := range ls {
		err = errors.Join(err, l.Close())
	}
	return errors.Join(err, a.b.Close())
}

func (a *adapter) isStarted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

func (a *adapter) Status(ctx context.Context) (transport.Status, error) {
	a.mu.Lock()
	started, startErr := a.started, a.startErr
	a.mu.Unlock()
	if !started {
		if startErr != nil {
			return transport.Status{State: transport.StateFailed, Detail: startErr.Error()}, nil
		}
		return transport.Status{State: transport.StateStopped}, nil
	}
	st, err := a.b.Status(ctx)
	if err != nil {
		return transport.Status{State: transport.StateFailed, Detail: err.Error()}, nil
	}
	out := transport.Status{Health: st.Health, Node: nodeOf(st)}
	switch st.State {
	case backendRunning:
		out.State = transport.StateReady
	case backendNeedsLogin:
		out.State, out.ActionURL, out.Detail = transport.StateNeedsAction, st.AuthURL, "sign-in required"
	case backendNeedsMachineAuth:
		out.State, out.Detail = transport.StateNeedsAction, "waiting for an administrator to approve this device"
	default:
		out.State = transport.StateStarting
	}
	return out, nil
}

func nodeOf(st BackendStatus) transport.NodeInfo {
	n := transport.NodeInfo{Name: strings.TrimSuffix(st.DNSName, "."), Addrs: append([]netip.Addr(nil), st.IPs...)}
	switch {
	case st.NodeID != "":
		n.ID = transport.NodeID(st.NodeID)
	case n.Name != "":
		n.ID = transport.NodeID(n.Name)
	case len(st.IPs) > 0:
		n.ID = transport.NodeID(st.IPs[0].String())
	}
	return n
}

func (a *adapter) Local(ctx context.Context) (transport.NodeInfo, error) {
	if !a.isStarted() {
		return transport.NodeInfo{}, transport.ErrNotStarted
	}
	st, err := a.b.Status(ctx)
	if err != nil {
		return transport.NodeInfo{}, err
	}
	if st.State != backendRunning {
		return transport.NodeInfo{}, transport.ErrNotReady
	}
	return nodeOf(st), nil
}

func (a *adapter) ready(ctx context.Context) error {
	if !a.isStarted() {
		return transport.ErrNotStarted
	}
	st, err := a.b.Status(ctx)
	if err != nil {
		return err
	}
	if st.State != backendRunning {
		return transport.ErrNotReady
	}
	return nil
}

func (a *adapter) Listen(ctx context.Context, addr string) (net.Listener, error) {
	if err := a.ready(ctx); err != nil {
		return nil, err
	}
	inner, err := a.b.Listen(addr, false)
	if err != nil {
		return nil, err
	}
	l := &listener{Listener: inner, a: a}
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		_ = inner.Close()
		return nil, transport.ErrNotStarted
	}
	a.listeners[l] = struct{}{}
	a.mu.Unlock()
	return l, nil
}

func (a *adapter) Dial(ctx context.Context, addr string) (net.Conn, error) {
	pb, ok := a.peers()
	if !ok {
		return nil, transport.ErrUnsupported
	}
	if err := a.ready(ctx); err != nil {
		return nil, err
	}
	c, err := pb.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, pb: pb}, nil
}

func (a *adapter) Peers(ctx context.Context) ([]transport.PeerStatus, error) {
	pb, ok := a.peers()
	if !ok {
		return nil, transport.ErrUnsupported
	}
	if err := a.ready(ctx); err != nil {
		return nil, err
	}
	ps, err := pb.Peers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]transport.PeerStatus, 0, len(ps))
	for _, p := range ps {
		path := transport.PathUnknown
		switch {
		case p.Direct:
			path = transport.PathDirect
		case p.Relayed:
			path = transport.PathRelayed
		}
		out = append(out, transport.PeerStatus{
			Node:      peerNode(p),
			Reachable: p.Online,
			Path:      path,
			LastSeen:  p.LastSeen,
		})
	}
	return out, nil
}

func peerNode(p BackendPeer) transport.NodeInfo {
	return transport.NodeInfo{ID: transport.NodeID(p.ID), Name: p.Name, Addrs: append([]netip.Addr(nil), p.IPs...)}
}

// listener wraps a connection acceptor so that what it accepts can say who it is from.
type listener struct {
	net.Listener
	a *adapter
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	pb, _ := l.a.peers()
	return &conn{Conn: c, pb: pb}, nil
}

func (l *listener) Close() error {
	l.a.mu.Lock()
	delete(l.a.listeners, l)
	l.a.mu.Unlock()
	return l.Listener.Close()
}

// conn is a connection that can describe itself. The peer is looked up the first
// time anyone asks, not on every accept.
type conn struct {
	net.Conn
	pb PeerBackend

	once sync.Once
	meta transport.ConnMeta
}

func (c *conn) Meta() transport.ConnMeta {
	c.once.Do(func() {
		c.meta = transport.ConnMeta{LocalAddr: addrString(c.LocalAddr()), RemoteAddr: addrString(c.RemoteAddr()),
			Path: transport.PathUnknown, Encrypted: true} // the tailnet encrypts and authenticates every packet between nodes
		if c.pb == nil || c.meta.RemoteAddr == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if p, ok := c.pb.WhoIs(ctx, c.meta.RemoteAddr); ok {
			c.meta.RemoteNode = transport.NodeID(p.ID)
		}
	})
	return c.meta
}

func addrString(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}
