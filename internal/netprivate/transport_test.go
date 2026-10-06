package netprivate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/transport"
	"devboard/internal/transport/transporttest"
)

// The embedded Tailscale node, behind the product-neutral contract, passes the
// same suite as every other transport: two real nodes on an in-process tailnet
// reach one another, see each other as peers, and say who a connection is from.
func TestTheTailscaleNodeConformsToTheTransportContract(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Tailscale nodes")
	}
	control := startControl(t, false, true)
	var n atomic.Int32
	transporttest.Run(t, func(t *testing.T) (transport.Transport, transport.Transport) {
		mk := func() transport.Transport {
			i := n.Add(1)
			return NewTailscaleTransport(TailscaleOptions{
				Dir: filepath.Join(t.TempDir(), "ts"), Hostname: fmt.Sprintf("wb-transport-%d", i),
				AuthKey: "tskey-test", ControlURL: control.HTTPTestServer.URL,
			})
		}
		return mk(), mk()
	}, 90*time.Second)
}

func TestAdapterMapsEveryBackendStateToTheContract(t *testing.T) {
	ctx := context.Background()
	f := &fakeBackend{}
	tr := NewTransport(f)

	if st, _ := tr.Status(ctx); st.State != transport.StateStopped {
		t.Fatalf("before Start: %+v", st)
	}
	if err := tr.Start(ctx); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		backend BackendStatus
		want    transport.State
		url     string
	}{
		{BackendStatus{State: "Starting"}, transport.StateStarting, ""},
		{BackendStatus{State: "NoState"}, transport.StateStarting, ""},
		{BackendStatus{State: backendNeedsLogin, AuthURL: "https://login.example/abc"}, transport.StateNeedsAction, "https://login.example/abc"},
		{BackendStatus{State: backendNeedsMachineAuth}, transport.StateNeedsAction, ""},
		{BackendStatus{State: backendRunning, DNSName: "wb.tail-scale.ts.net.", IPs: []netip.Addr{ip}, NodeID: "n1", Health: []string{"slow"}}, transport.StateReady, ""},
	}
	for _, c := range cases {
		f.set(c.backend)
		st, err := tr.Status(ctx)
		if err != nil || st.State != c.want || st.ActionURL != c.url {
			t.Errorf("backend %q → %+v, %v; want %q", c.backend.State, st, err, c.want)
		}
	}

	// A node that is not on its network yet has no identity to give.
	f.set(BackendStatus{State: backendNeedsLogin})
	if _, err := tr.Local(ctx); !errors.Is(err, transport.ErrNotReady) {
		t.Errorf("Local while signing in = %v", err)
	}
	if _, err := tr.Listen(ctx, ":1"); !errors.Is(err, transport.ErrNotReady) {
		t.Errorf("Listen while signing in = %v", err)
	}

	f.set(BackendStatus{State: backendRunning, DNSName: "wb.tail-scale.ts.net.", IPs: []netip.Addr{ip}, NodeID: "n1"})
	n, err := tr.Local(ctx)
	if err != nil || n.ID != "n1" || n.Name != "wb.tail-scale.ts.net" || len(n.Addrs) != 1 || n.Addrs[0] != ip {
		t.Errorf("Local = %+v, %v", n, err)
	}
	// Without a stable node ID the name, then the address, identifies it.
	f.set(BackendStatus{State: backendRunning, DNSName: "wb.tail-scale.ts.net.", IPs: []netip.Addr{ip}})
	if n, _ := tr.Local(ctx); n.ID != "wb.tail-scale.ts.net" {
		t.Errorf("fallback ID = %q", n.ID)
	}
	f.set(BackendStatus{State: backendRunning, IPs: []netip.Addr{ip}})
	if n, _ := tr.Local(ctx); n.ID != transport.NodeID(ip.String()) {
		t.Errorf("fallback ID = %q", n.ID)
	}
}

func TestAdapterFailureAndLifecycle(t *testing.T) {
	ctx := context.Background()

	f := &fakeBackend{startErr: errors.New("no state directory")}
	tr := NewTransport(f)
	if err := tr.Start(ctx); err == nil {
		t.Fatal("a failed Start reported success")
	}
	if st, _ := tr.Status(ctx); st.State != transport.StateFailed || st.Detail != "no state directory" {
		t.Errorf("after a failed Start: %+v", st)
	}

	f = &fakeBackend{}
	f.set(BackendStatus{State: backendRunning, IPs: []netip.Addr{ip}})
	tr = NewTransport(f)
	if err := tr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f.statusErr = errors.New("local API gone")
	if st, err := tr.Status(ctx); err != nil || st.State != transport.StateFailed {
		t.Errorf("a node that cannot be asked: %+v, %v", st, err)
	}
	f.statusErr = nil

	// Stop closes the backend and every listener the transport handed out, and is repeatable.
	l, err := tr.Listen(ctx, ":80")
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Stop(ctx); err != nil || !f.isClosed() {
		t.Fatalf("Stop: %v, closed=%v", err, f.isClosed())
	}
	if _, err := l.Accept(); err == nil {
		t.Error("a listener outlived Stop")
	}
	if err := tr.Stop(ctx); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	if _, err := tr.Local(ctx); !errors.Is(err, transport.ErrNotStarted) {
		t.Errorf("Local after Stop = %v", err)
	}
}

func TestABackendThatCannotDialSaysSo(t *testing.T) {
	ctx := context.Background()
	f := &fakeBackend{}
	f.set(BackendStatus{State: backendRunning, IPs: []netip.Addr{ip}})
	tr := NewTransport(f)
	_ = tr.Start(ctx)
	if _, err := tr.Dial(ctx, "100.1.1.1:80"); !errors.Is(err, transport.ErrUnsupported) {
		t.Errorf("Dial = %v", err)
	}
	if _, err := tr.Peers(ctx); !errors.Is(err, transport.ErrUnsupported) {
		t.Errorf("Peers = %v", err)
	}
}

// peerFake is a backend that can also dial and list peers.
type peerFake struct {
	fakeBackend
	peers []BackendPeer
	who   map[string]BackendPeer
}

func (p *peerFake) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}
func (p *peerFake) Peers(context.Context) ([]BackendPeer, error) { return p.peers, nil }
func (p *peerFake) WhoIs(_ context.Context, remote string) (BackendPeer, bool) {
	v, ok := p.who[remote]
	return v, ok
}

func TestPeersAndConnectionsAreDescribedInTheContractsTerms(t *testing.T) {
	ctx := context.Background()
	peerIP := netip.MustParseAddr("100.101.102.104")
	pf := &peerFake{peers: []BackendPeer{
		{ID: "direct", Name: "a.ts.net", IPs: []netip.Addr{peerIP}, Online: true, Direct: true},
		{ID: "relay", Name: "b.ts.net", Online: true, Relayed: true},
		{ID: "gone", Name: "c.ts.net", LastSeen: time.Unix(1700000000, 0)},
	}}
	pf.set(BackendStatus{State: backendRunning, IPs: []netip.Addr{ip}, NodeID: "me"})
	tr := NewTransport(pf)
	if err := tr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer tr.Stop(ctx)

	ps, err := tr.Peers(ctx)
	if err != nil || len(ps) != 3 {
		t.Fatalf("%+v, %v", ps, err)
	}
	if ps[0].Node.ID != "direct" || !ps[0].Reachable || ps[0].Path != transport.PathDirect || ps[0].Node.Addrs[0] != peerIP {
		t.Errorf("direct peer = %+v", ps[0])
	}
	if !ps[1].Reachable || ps[1].Path != transport.PathRelayed {
		t.Errorf("relayed peer = %+v", ps[1])
	}
	if ps[2].Reachable || ps[2].Path != transport.PathUnknown || ps[2].LastSeen.IsZero() {
		t.Errorf("offline peer = %+v", ps[2])
	}

	// A connection accepted through the transport names its peer, from the backend's WhoIs.
	l, err := tr.Listen(ctx, ":80")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := net.Dial("tcp", pf.addr(":80"))
		if err == nil {
			defer c.Close()
			time.Sleep(200 * time.Millisecond)
		}
	}()
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pf.who = map[string]BackendPeer{c.RemoteAddr().String(): {ID: "direct"}}
	m, ok := transport.MetaOf(c)
	if !ok || m.RemoteNode != "direct" || !m.Encrypted || m.RemoteAddr != c.RemoteAddr().String() {
		t.Errorf("meta = %+v, %v", m, ok)
	}
	if _, ok := transport.MetaOf(&net.TCPConn{}); ok {
		t.Error("a plain connection claimed metadata")
	}
}
