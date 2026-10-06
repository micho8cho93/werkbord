// Package overlay describes a node of a workspace's private network and renders the
// configuration the network program needs for it: a typed description in, a
// configuration out, and nothing in between that a person or a request could write
// into.
//
// It is where the network's policy is decided. The policy is default-deny and is
// stated in groups (internal/team/infra/pki): a device may reach a thing only because
// a rule here names its group and that thing's port, and a device that is only a
// member may reach the workspace's API and nothing else (policy.go, and its tests,
// which also run the generated policy through the network program's own parser).
//
// The package is pure. It starts nothing, reads nothing and opens nothing; the
// supervisor (internal/team/infra/nebula) is what runs the result.
package overlay

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Defaults.
const (
	// DefaultPort is the UDP port a discovery or relay host listens on. It must be
	// fixed (and reachable) for those; any other node uses a port the system picks.
	DefaultPort = 4242
	// APIPort is where a Workspace Host serves the workspace's API on the private
	// network, and the one port members reach. It follows Team's own default.
	APIPort = 7430
	// DeviceServicePort is where a device accepts the workspace's own messages (the
	// routing of an authenticated envelope to a runner). Only Workspace Hosts may
	// reach it. It is provisional until the runner message path is built.
	DeviceServicePort = 7450
	// ReplicationPorts are what Workspace Hosts use among themselves for the
	// replicated database. Only Workspace Hosts may reach them.
	ReplicationPorts = "4001-4002"
)

// Peer is a machine of the network other nodes use to find or reach others.
type Peer struct {
	// Addr is its address on the private network.
	Addr netip.Addr
	// Endpoints are where it can be reached on the underlying network, "host:port".
	Endpoints []string
}

// NodeSpec is everything the configuration of one node depends on.
type NodeSpec struct {
	// Name is the node's name: its device ID. It is only used in comments.
	Name string
	// Addr is the node's address on the private network, and Bits the length of the
	// network's prefix.
	Addr netip.Addr
	Bits int
	// Discovery is true if this node helps others find each other (it is a lighthouse).
	Discovery bool
	// Relay is true if this node carries traffic for pairs that cannot reach each other.
	Relay bool
	// Port is the UDP port to listen on (0: the system chooses). A node that is
	// Discovery or Relay needs a fixed one.
	Port int
	// Advertise are the addresses ("host:port") this node says it can be reached at
	// from outside, when it knows them (a public IP, a forwarded port).
	Advertise []string
	// Lighthouses are the discovery hosts this node reports to and asks, other than
	// itself. More than one is better than one.
	Lighthouses []Peer
	// RelayedBy are the relay hosts others may use to reach this node.
	RelayedBy []netip.Addr
	// UseRelays lets this node reach others through relays when it cannot directly.
	UseRelays bool
	// Blocklist are fingerprints of certificates this node refuses to talk to.
	Blocklist []string
	// Policy is what this node accepts and may start.
	Policy Policy
	// Files are the paths of the authority's certificate, this node's certificate and
	// its private key.
	CA, Cert, Key string
	// TunDisabled runs the node without a network interface: it can still find
	// others and relay for them, but carries no traffic of its own. It needs no
	// special privileges, which a host that only connects others can use.
	TunDisabled bool
	// TunDev names the interface; empty lets the system choose.
	TunDev string
	// MTU is the interface's MTU (default 1300).
	MTU int
	// LogLevel is "info" unless set.
	LogLevel string
	// Stats, if set, is a loopback address ("127.0.0.1:port") on which the node serves
	// its metrics, for health checks.
	Stats string
}

var fingerprintRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks a spec is one the renderer can state exactly.
func (s NodeSpec) Validate() error {
	bad := func(format string, a ...any) error { return fmt.Errorf("overlay: "+format, a...) }
	switch {
	case !s.Addr.IsValid() || !s.Addr.Is4():
		return bad("the node needs an IPv4 address on the network")
	case s.Bits < 16 || s.Bits > 24:
		return bad("the network's prefix is /%d: it must be between /16 and /24", s.Bits)
	case s.Port < 0 || s.Port > 65535:
		return bad("port %d", s.Port)
	case (s.Discovery || s.Relay) && s.Port == 0:
		return bad("a discovery or relay host needs a fixed port")
	case s.CA == "" || s.Cert == "" || s.Key == "":
		return bad("the certificate and key paths are required")
	case s.MTU != 0 && (s.MTU < 576 || s.MTU > 9000):
		return bad("mtu %d", s.MTU)
	}
	if strings.ContainsAny(s.CA+s.Cert+s.Key+s.TunDev+s.Name, "\r\n\x00") {
		return bad("a control character in a path or name")
	}
	switch s.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return bad("log level %q", s.LogLevel)
	}
	for _, e := range append(append([]string(nil), s.Advertise...), peerEndpoints(s.Lighthouses)...) {
		if _, _, err := net.SplitHostPort(e); err != nil {
			return bad("endpoint %q is not host:port", e)
		}
	}
	seen := map[netip.Addr]bool{s.Addr: true}
	for _, l := range s.Lighthouses {
		if !l.Addr.IsValid() || seen[l.Addr] {
			return bad("lighthouse %v is invalid or repeated (or is this node)", l.Addr)
		}
		seen[l.Addr] = true
	}
	for _, f := range s.Blocklist {
		if !fingerprintRE.MatchString(f) {
			return bad("%q is not a certificate fingerprint", f)
		}
	}
	if s.Stats != "" {
		h, _, err := net.SplitHostPort(s.Stats)
		if ip := net.ParseIP(h); err != nil || ip == nil || !ip.IsLoopback() {
			return bad("metrics are served on a loopback address only")
		}
	}
	return s.Policy.Validate()
}

func peerEndpoints(ps []Peer) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Endpoints...)
	}
	return out
}

// ---- rendering ----

// node is one value of the tiny configuration tree the renderer writes.
type node struct {
	key  string
	val  any // string, int, bool, []string, []*node (a mapping), [][]*node (a list of mappings)
	note string
}

type tree []*node

func (t tree) add(key string, val any) tree { return append(t, &node{key: key, val: val}) }

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	}
	panic(fmt.Sprintf("overlay: unsupported scalar %T", v))
}

func (t tree) write(b *strings.Builder, indent int) {
	pad := strings.Repeat("  ", indent)
	for _, n := range t {
		switch v := n.val.(type) {
		case tree:
			if n.note != "" {
				fmt.Fprintf(b, "%s# %s\n", pad, n.note)
			}
			fmt.Fprintf(b, "%s%s:\n", pad, n.key)
			v.write(b, indent+1)
		case []string:
			if len(v) == 0 {
				fmt.Fprintf(b, "%s%s: []\n", pad, n.key)
				continue
			}
			fmt.Fprintf(b, "%s%s:\n", pad, n.key)
			for _, s := range v {
				fmt.Fprintf(b, "%s  - %s\n", pad, scalar(s))
			}
		case []tree:
			if len(v) == 0 {
				fmt.Fprintf(b, "%s%s: []\n", pad, n.key)
				continue
			}
			fmt.Fprintf(b, "%s%s:\n", pad, n.key)
			for _, item := range v {
				var inner strings.Builder
				item.write(&inner, 0)
				lines := strings.Split(strings.TrimRight(inner.String(), "\n"), "\n")
				for i, l := range lines {
					if i == 0 {
						fmt.Fprintf(b, "%s  - %s\n", pad, l)
					} else {
						fmt.Fprintf(b, "%s    %s\n", pad, l)
					}
				}
			}
		default:
			if n.note != "" {
				fmt.Fprintf(b, "%s%s: %s  # %s\n", pad, n.key, scalar(v), safeComment(n.note))
			} else {
				fmt.Fprintf(b, "%s%s: %s\n", pad, n.key, scalar(v))
			}
		}
	}
}

// Render writes the node's configuration. The result is deterministic: the same
// spec renders the same bytes, so a change is visible as one.
func Render(s NodeSpec) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	pki := tree{}.add("ca", s.CA).add("cert", s.Cert).add("key", s.Key).add("disconnect_invalid", true)
	if len(s.Blocklist) > 0 {
		bl := append([]string(nil), s.Blocklist...)
		sort.Strings(bl)
		pki = pki.add("blocklist", bl)
	}

	static := tree{}
	var hosts []string
	for _, l := range sortedPeers(s.Lighthouses) {
		hosts = append(hosts, l.Addr.String())
		if len(l.Endpoints) > 0 {
			static = static.add(l.Addr.String(), l.Endpoints)
		}
	}
	lh := tree{}.add("am_lighthouse", s.Discovery).add("interval", 60)
	if !s.Discovery {
		lh = lh.add("hosts", hosts)
	}
	if len(s.Advertise) > 0 {
		lh = lh.add("advertise_addrs", s.Advertise)
	}

	var relays []string
	for _, r := range s.RelayedBy {
		relays = append(relays, r.String())
	}
	sort.Strings(relays)
	relay := tree{}.add("am_relay", s.Relay).add("use_relays", s.UseRelays)
	if len(relays) > 0 {
		relay = relay.add("relays", relays)
	}

	mtu := s.MTU
	if mtu == 0 {
		mtu = 1300
	}
	tun := tree{}.add("disabled", s.TunDisabled)
	if s.TunDev != "" {
		tun = tun.add("dev", s.TunDev)
	}
	tun = tun.add("drop_local_broadcast", true).add("drop_multicast", true).add("mtu", mtu)

	level := s.LogLevel
	if level == "" {
		level = "info"
	}

	root := tree{}
	root = append(root, &node{key: "pki", val: pki, note: "Werkbord Team: generated for " + safeComment(s.Name) + ". Do not edit; Team rewrites this file."})
	root = root.add("static_host_map", static)
	root = root.add("lighthouse", lh)
	root = root.add("listen", tree{}.add("host", "0.0.0.0").add("port", s.Port))
	root = root.add("punchy", tree{}.add("punch", true).add("respond", true))
	root = root.add("relay", relay)
	root = root.add("tun", tun)
	root = root.add("logging", tree{}.add("level", level).add("format", "json"))
	if s.Stats != "" {
		root = root.add("stats", tree{}.add("type", "prometheus").add("listen", s.Stats).add("path", "/metrics").add("namespace", "nebula").add("interval", "10s"))
	}
	root = root.add("firewall", s.Policy.tree())

	var b strings.Builder
	root.write(&b, 0)
	return []byte(b.String()), nil
}

func sortedPeers(ps []Peer) []Peer {
	out := append([]Peer(nil), ps...)
	sort.Slice(out, func(i, j int) bool { return out[i].Addr.Less(out[j].Addr) })
	return out
}

func safeComment(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// ErrNoSuchGroup is returned for a rule that names a group no certificate can carry.
var ErrNoSuchGroup = errors.New("overlay: a rule names a group that does not exist")
