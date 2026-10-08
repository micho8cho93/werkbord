//go:build !race

package overlay_test

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/slackhq/nebula"
	"github.com/slackhq/nebula/config"
	nebulaoverlay "github.com/slackhq/nebula/overlay"
	"github.com/slackhq/nebula/service"

	"devboard/internal/team/infra/overlay"
	"devboard/internal/team/infra/pki"
)

// These tests run the policy this package generates through Nebula's own firewall,
// not through a reading of ours: several real Nebula nodes (the same code the bundled
// program is built from, at the pinned version, in its userspace mode so that no
// privileges are needed) are connected over loopback, each configured exactly as
// Render writes it, and then try to reach each other.

type live struct {
	name   string
	groups []string
	addr   netip.Addr
	port   int
	svc    *service.Service
}

type labNet struct {
	t     *testing.T
	ca    *pki.NetworkCA
	caPEM []byte
	nodes map[string]*live
	next  int
	dir   string
}

func newLab(t *testing.T) *labNet {
	t.Helper()
	ca, err := pki.NewNetworkCA("Lab", netip.MustParsePrefix("10.88.0.0/16"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	caPEM, _ := ca.CertificatePEM()
	return &labNet{t: t, ca: ca, caPEM: caPEM, nodes: map[string]*live{}, next: 1, dir: t.TempDir()}
}

func freePort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// reserve allocates identities first, so that every node can be told where every other is.
func (l *labNet) reserve(name string, groups ...string) *live {
	l.next++
	n := &live{name: name, groups: groups, addr: netip.AddrFrom4([4]byte{10, 88, 0, byte(l.next)}), port: freePort(l.t)}
	l.nodes[name] = n
	return n
}

// start brings a reserved node up. edit may change the generated settings before the
// node reads them (to give a node a configuration its own owner might write).
func (l *labNet) start(n *live, discovery []*live, edit func(map[string]any)) {
	l.t.Helper()
	priv, pub, err := pki.GenerateNodeKey()
	if err != nil {
		l.t.Fatal(err)
	}
	issued, err := l.ca.Issue(pki.NodeRequest{Name: n.name, Addr: n.addr, Groups: n.groups, PublicKeyPEM: pub}, time.Now())
	if err != nil {
		l.t.Fatal(err)
	}
	dir := filepath.Join(l.dir, n.name)
	_ = os.MkdirAll(dir, 0o700)
	files := map[string][]byte{"ca.crt": l.caPEM, "node.crt": issued.CertPEM, "node.key": priv}
	for f, b := range files {
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o600); err != nil {
			l.t.Fatal(err)
		}
	}
	isDiscovery := false
	for _, g := range n.groups {
		isDiscovery = isDiscovery || g == pki.GroupConnectivityHost
	}
	spec := overlay.NodeSpec{Name: n.name, Addr: n.addr, Bits: 16, Port: n.port, Discovery: isDiscovery, Relay: isDiscovery, UseRelays: true,
		CA: filepath.Join(dir, "ca.crt"), Cert: filepath.Join(dir, "node.crt"), Key: filepath.Join(dir, "node.key"),
		Policy: overlay.PolicyFor(n.groups, overlay.DefaultPorts()), LogLevel: "error"}
	for _, d := range discovery {
		if d != n {
			spec.Lighthouses = append(spec.Lighthouses, overlay.Peer{Addr: d.addr, Endpoints: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(d.port))}})
		}
	}
	rendered, err := overlay.Render(spec)
	if err != nil {
		l.t.Fatal(err)
	}
	var c config.C
	if err := c.LoadString(string(rendered)); err != nil {
		l.t.Fatalf("Nebula cannot read what Render wrote: %v\n%s", err, rendered)
	}
	// What the lab changes: userspace networking instead of an interface, and every node
	// knows every other's loopback address (there is no real network to discover them on).
	c.Settings["tun"] = map[string]any{"user": true}
	static := map[string]any{}
	for _, o := range l.nodes {
		if o != n {
			static[o.addr.String()] = []any{net.JoinHostPort("127.0.0.1", strconv.Itoa(o.port))}
		}
	}
	c.Settings["static_host_map"] = static
	if edit != nil {
		edit(c.Settings)
	}
	ctrl, err := nebula.Main(&c, false, "lab", slog.New(slog.DiscardHandler), nebulaoverlay.NewUserDeviceFromConfig)
	if err != nil {
		l.t.Fatalf("starting %s: %v", n.name, err)
	}
	svc, err := service.New(ctrl)
	if err != nil {
		l.t.Fatal(err)
	}
	n.svc = svc
	l.t.Cleanup(func() { _ = svc.Close(); _ = svc.Wait() })
}

// listen makes n accept TCP on a port, and answer with its own name.
func (l *labNet) listen(n *live, port int) {
	l.t.Helper()
	ln, err := n.svc.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		l.t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(n.name))
			_ = c.Close()
		}
	}()
	l.t.Cleanup(func() { _ = ln.Close() })
}

// reaches reports whether from can open a TCP connection to to:port, and read who answered.
func (l *labNet) reaches(from, to *live, port int, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	c, err := from.svc.DialContext(ctx, "tcp", net.JoinHostPort(to.addr.String(), strconv.Itoa(port)))
	if err != nil {
		return false
	}
	defer c.Close()
	buf := make([]byte, 64)
	_ = c.SetReadDeadline(time.Now().Add(within))
	n, _ := c.Read(buf)
	return string(buf[:n]) == to.name
}

// eventually: a tunnel takes a moment to come up the first time.
func (l *labNet) eventually(from, to *live, port int) bool {
	for i := 0; i < 8; i++ {
		if l.reaches(from, to, port, 2*time.Second) {
			return true
		}
	}
	return false
}

func TestThePolicyAsNebulaEnforcesIt(t *testing.T) {
	lab := newLab(t)
	host1 := lab.reserve("host1", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	host2 := lab.reserve("host2", pki.GroupWorkspaceHost)
	relayOnly := lab.reserve("connectivity", pki.GroupConnectivityHost)
	alice := lab.reserve("alice", pki.GroupMember, pki.GroupRunner)
	bob := lab.reserve("bob", pki.GroupMember, pki.GroupRunner)
	// Mallory is a member whose own configuration has been edited to allow everything
	// outbound: what stops her is what the machines she reaches for do.
	mallory := lab.reserve("mallory", pki.GroupMember, pki.GroupRunner)

	disc := []*live{host1, relayOnly}
	lab.start(host1, disc, nil)
	lab.start(host2, disc, nil)
	lab.start(relayOnly, disc, nil)
	lab.start(alice, disc, nil)
	lab.start(bob, disc, nil)
	lab.start(mallory, disc, func(s map[string]any) {
		fw := s["firewall"].(map[string]any)
		fw["outbound"] = []any{map[string]any{"port": "any", "proto": "any", "host": "any"}}
	})

	ports := overlay.DefaultPorts()
	for _, h := range []*live{host1, host2} {
		for _, p := range []int{ports.API, 4001, 4002, 22, 5432, 8080} {
			lab.listen(h, p)
		}
		lab.listen(h, ports.DeviceService)
	}
	for _, m := range []*live{alice, bob, mallory} {
		for _, p := range []int{ports.DeviceService, 22, 8080, 3000, 7420} {
			lab.listen(m, p)
		}
	}
	for _, p := range []int{ports.API, 22, 4001, 7450} {
		lab.listen(relayOnly, p)
	}

	type probe struct {
		name     string
		from, to *live
		port     int
		allowed  bool
	}
	probes := []probe{
		// what is meant to work
		{"a member reaches the workspace API on a host", alice, host1, ports.API, true},
		{"a member reaches the API on the other host too", bob, host2, ports.API, true},
		{"a host cannot dial a member device; mailbox delivery is polled", host1, alice, ports.DeviceService, false},
		{"a host reaches another host's API", host1, host2, ports.API, true},
		{"hosts replicate the database among themselves (4001)", host1, host2, 4001, true},
		{"hosts replicate the database among themselves (4002)", host2, host1, 4002, true},

		// no runner-to-runner access, whatever the sender's own configuration says
		{"a member cannot reach another member's device service", alice, bob, ports.DeviceService, false},
		{"a member cannot reach another member's SSH", alice, bob, 22, false},
		{"a member cannot reach another member's development server", alice, bob, 3000, false},
		{"a member cannot reach another member's controller port", alice, bob, 7420, false},
		{"a member with an edited config cannot reach another member's device service", mallory, bob, ports.DeviceService, false},
		{"a member with an edited config cannot reach another member's SSH", mallory, alice, 22, false},
		{"a member with an edited config cannot reach another member's other ports", mallory, alice, 8080, false},

		// no database access from the member role
		{"a member cannot reach the database (4001)", alice, host1, 4001, false},
		{"a member cannot reach the database (4002)", alice, host2, 4002, false},
		{"a member with an edited config cannot reach the database (4001)", mallory, host1, 4001, false},
		{"a member with an edited config cannot reach the database (4002)", mallory, host2, 4002, false},

		// no host administration ports, no arbitrary peer ports
		{"a member cannot reach a host's SSH", alice, host1, 22, false},
		{"a member with an edited config cannot reach a host's SSH", mallory, host2, 22, false},
		{"a member with an edited config cannot reach a host's other ports", mallory, host1, 8080, false},
		{"a member with an edited config cannot reach a host's database port", mallory, host1, 5432, false},

		// a Connectivity Host exposes nothing but what it relays
		{"a member cannot reach a Connectivity Host's ports", alice, relayOnly, ports.API, false},
		{"a member with an edited config cannot reach a Connectivity Host's SSH", mallory, relayOnly, 22, false},
		{"a host cannot reach a Connectivity Host's ports", host1, relayOnly, 4001, false},
	}
	// The first connection warms the tunnel; the rest are probed in parallel.
	if !lab.eventually(alice, host1, ports.API) {
		t.Fatal("alice could not reach the API at all: the lab is not working, so a refusal below would prove nothing")
	}
	var wg sync.WaitGroup
	results := make([]bool, len(probes))
	for i, p := range probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.allowed {
				results[i] = lab.eventually(p.from, p.to, p.port)
			} else {
				results[i] = lab.reaches(p.from, p.to, p.port, 3*time.Second)
			}
		}()
	}
	wg.Wait()
	for i, p := range probes {
		if results[i] != p.allowed {
			t.Errorf("%s: reachable=%v, want %v", p.name, results[i], p.allowed)
		}
	}
	// The same refusals matter more if the tunnels are up, so check a few again now that they are.
	for _, p := range probes {
		if !p.allowed && lab.reaches(p.from, p.to, p.port, time.Second) {
			t.Errorf("%s: reachable on a second attempt", p.name)
		}
	}
}

// A refusal proves something only if the same attempt succeeds when nothing forbids
// it. This is the control for the test above: two members with a permissive firewall
// (what a policy of "allow everything" would give) do reach each other's ports.
func TestTheLabCanTellAllowedFromBlocked(t *testing.T) {
	lab := newLab(t)
	host1 := lab.reserve("host1", pki.GroupWorkspaceHost, pki.GroupConnectivityHost)
	alice := lab.reserve("alice", pki.GroupMember, pki.GroupRunner)
	bob := lab.reserve("bob", pki.GroupMember, pki.GroupRunner)
	open := func(s map[string]any) {
		allow := []any{map[string]any{"port": "any", "proto": "any", "host": "any"}}
		fw := s["firewall"].(map[string]any)
		fw["outbound"], fw["inbound"] = allow, allow
	}
	disc := []*live{host1}
	lab.start(host1, disc, nil)
	lab.start(alice, disc, open)
	lab.start(bob, disc, open)
	lab.listen(bob, 3000)
	if !lab.eventually(alice, bob, 3000) {
		t.Fatal("with an open firewall one member could not reach another: the refusals in the other test would prove nothing")
	}
}

// The Connectivity Host is both lighthouse and relay. Existing direct peer paths
// must keep working when it disappears. This is not a forced-NAT relay-only test.
func TestDirectPeersSurviveConnectivityAndRelayLoss(t *testing.T) {
	lab := newLab(t)
	connectivity := lab.reserve("connectivity", pki.GroupConnectivityHost)
	host := lab.reserve("host", pki.GroupWorkspaceHost)
	member := lab.reserve("member", pki.GroupMember, pki.GroupRunner)
	discovery := []*live{connectivity}
	lab.start(connectivity, discovery, nil)
	lab.start(host, discovery, nil)
	lab.start(member, discovery, nil)
	lab.listen(host, overlay.APIPort)
	if !lab.eventually(member, host, overlay.APIPort) {
		t.Fatal("the direct peer path never became usable")
	}
	if err := connectivity.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Wait joins the stopped userspace node; its expected terminal error may be
	// context cancellation or a closed virtual device, as in the lab cleanup.
	_ = connectivity.svc.Wait()
	if !lab.eventually(member, host, overlay.APIPort) {
		t.Fatal("losing the lighthouse/relay stopped an established direct peer path")
	}
}

// And the policy as generated, put back to back with the rule set the test above
// relies on: a member's device is not told to accept anything from a member.
func TestNoRuleLetsAMemberReachAnotherMember(t *testing.T) {
	ports := overlay.DefaultPorts()
	for _, groups := range [][]string{{pki.GroupMember}, {pki.GroupMember, pki.GroupRunner}} {
		p := overlay.PolicyFor(groups, ports)
		for _, port := range []int{22, 80, 443, 3000, 4001, 4002, ports.API, ports.DeviceService, 7420, 8080} {
			if p.Allows(true, "tcp", port, []string{pki.GroupMember}) || p.Allows(true, "tcp", port, []string{pki.GroupRunner}) {
				t.Errorf("%v accepts tcp/%d from a member or a runner", groups, port)
			}
			if p.Allows(true, "udp", port, []string{pki.GroupMember}) {
				t.Errorf("%v accepts udp/%d from a member", groups, port)
			}
		}
		if p.Allows(true, "icmp", 0, []string{pki.GroupMember}) {
			t.Errorf("%v answers ping from a member", groups)
		}
	}
}
