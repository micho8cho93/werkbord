// Package transport is the product-neutral contract between Werkbord and
// whatever private network carries its traffic between devices.
//
// Application and domain code (Team's workspace logic, runner messaging, the
// controller) depends on this package and on nothing that names a particular
// network. A network is a Transport: it starts and stops, says who this node is,
// listens, dials, and reports which peers it can reach and how. Today the
// implementation is the embedded Tailscale node in internal/netprivate; the
// production Team design replaces it with another, and a later one could replace
// that, without a change to anything that only uses this package.
//
// What this package deliberately does not contain is as important as what it
// does: no coordination server, relay, lighthouse, account, tailnet or any other
// vendor concept, and no application identity. A NodeID names a node on the
// network and nothing else. Who a *device* is to Werkbord (the key that signs its
// messages, the member who owns it, what it may do) is internal/deviceid and
// internal/envelope, and the two identities are never the same key: being on the
// network authenticates a connection, not a request.
package transport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// Errors a Transport returns.
var (
	// ErrNotStarted: the operation needs a running transport.
	ErrNotStarted = errors.New("transport: not started")
	// ErrNotReady: the transport is running but not yet on its network (for
	// instance, waiting for a person to complete enrolment).
	ErrNotReady = errors.New("transport: not ready")
	// ErrUnsupported: this implementation cannot do that.
	ErrUnsupported = errors.New("transport: not supported by this implementation")
)

// NodeID names a node on the private network. It is opaque to callers, stable
// for as long as the node keeps its network state, and is never an application
// identity: do not use it to decide who may do what.
type NodeID string

// State is where a transport has got to.
type State string

const (
	// StateStopped: not running.
	StateStopped State = "stopped"
	// StateStarting: coming up, or reconnecting.
	StateStarting State = "starting"
	// StateNeedsAction: running, but a person has to do something before it can
	// join its network (Status.ActionURL says where, when there is a page for it).
	StateNeedsAction State = "needs_action"
	// StateReady: on the network; Listen, Dial and Peers work.
	StateReady State = "ready"
	// StateFailed: it could not be brought up; Status.Detail says why.
	StateFailed State = "failed"
)

// NodeInfo identifies a node on the network.
type NodeInfo struct {
	ID NodeID
	// Name is a human-readable name, which may be empty.
	Name string
	// Addrs are the node's addresses on the private network.
	Addrs []netip.Addr
}

// Status is a snapshot of a transport. It never contains a secret.
type Status struct {
	State State
	// Node is this node, once it has an identity on the network.
	Node NodeInfo
	// ActionURL is where a person completes what StateNeedsAction is waiting for.
	// It is a one-time link for that person: show it, never log it.
	ActionURL string
	// Detail says why the state is what it is, when that is worth saying.
	Detail string
	// Health lists problems the network reports about itself.
	Health []string
}

// Path is how traffic to a peer travels.
type Path string

const (
	// PathUnknown: the transport cannot tell.
	PathUnknown Path = ""
	// PathDirect: straight between the two nodes.
	PathDirect Path = "direct"
	// PathRelayed: through an intermediary the network operator runs.
	PathRelayed Path = "relayed"
)

// PeerStatus is what a transport knows about one other node.
type PeerStatus struct {
	Node NodeInfo
	// Reachable says the transport believes it can reach the node now.
	Reachable bool
	Path      Path
	// LastSeen is when the node was last known to be reachable; zero if never or unknown.
	LastSeen time.Time
}

// ConnMeta is the diagnostic metadata about one connection.
type ConnMeta struct {
	// LocalAddr and RemoteAddr are the addresses on the private network.
	LocalAddr, RemoteAddr string
	// RemoteNode is the peer's node, when the transport can name it.
	RemoteNode NodeID
	Path       Path
	// Encrypted says the transport itself encrypts and authenticates the
	// connection between the two nodes. It says nothing about the application
	// above it, which authorises every request on its own.
	Encrypted bool
}

// Conn is a net.Conn that can describe itself. Connections a Transport returns
// from Dial, and accepts from its listeners, implement it; use MetaOf rather than
// asserting, so a plain connection is handled too.
type Conn interface {
	net.Conn
	Meta() ConnMeta
}

// MetaOf returns the metadata of a connection from a Transport.
func MetaOf(c net.Conn) (ConnMeta, bool) {
	if m, ok := c.(interface{ Meta() ConnMeta }); ok {
		return m.Meta(), true
	}
	return ConnMeta{}, false
}

// Transport is a private network node.
type Transport interface {
	// Start brings the node up. It returns once the node is running, which is not
	// the same as being on its network: learn that from Status. Starting a started
	// transport does nothing.
	Start(ctx context.Context) error
	// Stop takes the node down and closes everything it listened on. Stopping a
	// stopped transport does nothing.
	Stop(ctx context.Context) error
	// Status describes the node now.
	Status(ctx context.Context) (Status, error)
	// Local is this node's identity. It is ErrNotStarted before Start, and
	// ErrNotReady until the node has one.
	Local(ctx context.Context) (NodeInfo, error)
	// Listen accepts connections made to this node on the private network, at addr
	// (":port"). Nothing it accepts comes from outside the network. The
	// connections it accepts implement Conn.
	Listen(ctx context.Context, addr string) (net.Listener, error)
	// Dial connects to a peer on the private network, by address ("host:port",
	// where host is one of the peer's NodeInfo.Addrs, or a name the network
	// resolves). The connection implements Conn.
	Dial(ctx context.Context, addr string) (net.Conn, error)
	// Peers lists the other nodes the transport knows about.
	Peers(ctx context.Context) ([]PeerStatus, error)
}
