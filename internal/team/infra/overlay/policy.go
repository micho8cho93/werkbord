package overlay

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"devboard/internal/team/infra/pki"
)

// The network's policy is default-deny in both directions, and is enforced by each
// node's own firewall on what reaches it (and on what it starts):
//
//   - nothing is allowed unless a rule below names it. The network program has no way
//     to write a "deny" and none is needed: what is not allowed is dropped;
//   - rules name a port and the group of the device on the other end. Groups are in
//     the certificate the authority signed, so a device cannot claim another's;
//   - a node's inbound rules are what protect it, and they are the rules that
//     matter: a node that edited its own file only changed what it accepts itself.
//     So the sensitive things (the database's ports, the workspace API) are guarded
//     by the Workspace Hosts' own rules, which are in the Workspace Hosts' control,
//     and a member's device guards itself the same way.
//
// What follows from the rules (policy_test.go proves each one against the network
// program's real firewall):
//
//	a member's device    may start: the workspace API on a Workspace Host.
//	                     accepts:   the workspace's own messages from a Workspace Host.
//	                     does not:  accept anything from another member's device, so another
//	                                member's runner, filesystem and ports are unreachable.
//	a Workspace Host     accepts:   the API from members and from other hosts; the database's
//	                                ports from other Workspace Hosts only.
//	                     does not:  expose the database to a member, or any administration
//	                                port (SSH, a shell, a debugger) to anything.
//	a Connectivity Host  accepts:   ping from hosts. Its job (finding and relaying) is done
//	                                by the network program below the firewall, and needs no rule.
//	nobody               accepts:   an arbitrary port from a peer.

// Rule allows one kind of traffic with the devices of one group.
type Rule struct {
	// Port is "any", a number, or a range ("4001-4002"). It is ignored for icmp.
	Port string
	// Proto is "tcp", "udp" or "icmp".
	Proto string
	// Group is the group of the device at the other end.
	Group string
	// Why is the reason, written into the configuration as a comment, so a person who
	// reads the file sees what each rule is for.
	Why string
}

// Policy is a node's rules. Anything not matched is dropped.
type Policy struct {
	Inbound  []Rule
	Outbound []Rule
}

var portRE = regexp.MustCompile(`^(any|[0-9]{1,5}(-[0-9]{1,5})?)$`)

// Validate checks every rule is well formed and names a group that exists.
func (p Policy) Validate() error {
	groups := map[string]bool{}
	for _, g := range pki.Groups() {
		groups[g] = true
	}
	for _, rules := range [][]Rule{p.Inbound, p.Outbound} {
		for _, r := range rules {
			switch {
			case r.Proto != "tcp" && r.Proto != "udp" && r.Proto != "icmp":
				return fmt.Errorf("overlay: rule %+v: protocol %q", r, r.Proto)
			case r.Proto != "icmp" && !portRE.MatchString(r.Port):
				return fmt.Errorf("overlay: rule %+v: port %q", r, r.Port)
			case r.Proto != "icmp" && r.Port == "any":
				return fmt.Errorf("overlay: rule %+v: \"any port\" is never allowed: name the port", r)
			case !groups[r.Group]:
				return fmt.Errorf("%w: %q", ErrNoSuchGroup, r.Group)
			case strings.ContainsAny(r.Why, "\r\n\x00"):
				return fmt.Errorf("overlay: a control character in a rule's reason")
			}
		}
	}
	return nil
}

func ruleTree(r Rule) tree {
	t := tree{}
	if r.Proto != "icmp" { // ICMP has no ports, and the network program says so if one is given
		t = t.add("port", r.Port)
	}
	return t.add("proto", r.Proto).add("group", r.Group)
}

func rulesTree(rules []Rule) []tree {
	out := make([]tree, 0, len(rules))
	for _, r := range rules {
		t := ruleTree(r)
		if r.Why != "" {
			t[0].note = r.Why
		}
		out = append(out, t)
	}
	return out
}

func (p Policy) tree() tree {
	return tree{}.
		add("outbound_action", "drop").
		add("inbound_action", "drop").
		add("conntrack", tree{}.add("tcp_timeout", "12m").add("udp_timeout", "3m").add("default_timeout", "10m")).
		add("outbound", rulesTree(p.Outbound)).
		add("inbound", rulesTree(p.Inbound))
}

// Ports are the ports the policy opens, so that they are written once.
type Ports struct {
	API           int
	DeviceService int
	Replication   string
}

// DefaultPorts are Team's.
func DefaultPorts() Ports {
	return Ports{API: APIPort, DeviceService: DeviceServicePort, Replication: ReplicationPorts}
}

// PolicyFor generates the policy of a node whose certificate carries groups. It is
// the only place network policy is written; callers never pass rules in.
func PolicyFor(groups []string, ports Ports) Policy {
	has := map[string]bool{}
	for _, g := range groups {
		has[g] = true
	}
	api := strconv.Itoa(ports.API)
	svc := strconv.Itoa(ports.DeviceService)
	var p Policy
	add := func(dst *[]Rule, r Rule) {
		for _, have := range *dst {
			if have == r {
				return
			}
		}
		*dst = append(*dst, r)
	}

	known := false
	for _, g := range pki.Groups() {
		known = known || has[g]
	}
	if !known {
		return p // a certificate with no group this policy knows is allowed nothing
	}
	// Every node answers ping from the machines that run the workspace, and only from them.
	for _, g := range []string{pki.GroupWorkspaceHost, pki.GroupConnectivityHost} {
		add(&p.Inbound, Rule{Proto: "icmp", Group: g, Why: "Workspace and Connectivity Hosts may ping this node, for diagnostics"})
		add(&p.Outbound, Rule{Proto: "icmp", Group: g, Why: "this node may ping the machines that run the workspace"})
	}

	if has[pki.GroupWorkspaceHost] {
		add(&p.Inbound, Rule{Port: api, Proto: "tcp", Group: pki.GroupMember, Why: "members reach the workspace's API on this host"})
		add(&p.Inbound, Rule{Port: api, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "other Workspace Hosts reach this host's API"})
		add(&p.Inbound, Rule{Port: ports.Replication, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "the replicated database: Workspace Hosts only, never a member"})
		add(&p.Outbound, Rule{Port: api, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "this host may reach other Workspace Hosts' APIs"})
		add(&p.Outbound, Rule{Port: ports.Replication, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "the replicated database, between Workspace Hosts"})
		add(&p.Outbound, Rule{Port: svc, Proto: "tcp", Group: pki.GroupRunner, Why: "this host delivers the workspace's messages to a member's device"})
	}
	if has[pki.GroupMember] || has[pki.GroupRunner] {
		add(&p.Inbound, Rule{Port: svc, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "only a Workspace Host may deliver the workspace's messages here; no other device may reach this one"})
		add(&p.Outbound, Rule{Port: api, Proto: "tcp", Group: pki.GroupWorkspaceHost, Why: "this device may reach the workspace's API, and nothing else"})
	}

	sortRules := func(rs []Rule) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Group != rs[j].Group {
				return rs[i].Group < rs[j].Group
			}
			if rs[i].Proto != rs[j].Proto {
				return rs[i].Proto < rs[j].Proto
			}
			return rs[i].Port < rs[j].Port
		})
	}
	sortRules(p.Inbound)
	sortRules(p.Outbound)
	return p
}

// Allows reports whether the policy accepts inbound traffic of proto to port from a
// device whose certificate carries groups. It is a reading of the rules for tests and
// for the status the API shows ("what may a member reach here"); the network program
// itself is what enforces them.
func (p Policy) Allows(inbound bool, proto string, port int, peerGroups []string) bool {
	rules := p.Outbound
	if inbound {
		rules = p.Inbound
	}
	for _, r := range rules {
		match := false
		for _, g := range peerGroups {
			match = match || g == r.Group
		}
		if !match || r.Proto != proto {
			continue
		}
		if proto == "icmp" {
			return true
		}
		if inRange(r.Port, port) {
			return true
		}
	}
	return false
}

func inRange(spec string, port int) bool {
	lo, hi, found := strings.Cut(spec, "-")
	a, err := strconv.Atoi(lo)
	if err != nil {
		return false
	}
	b := a
	if found {
		if b, err = strconv.Atoi(hi); err != nil {
			return false
		}
	}
	return port >= a && port <= b
}
