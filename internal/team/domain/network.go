package domain

import (
	"net/netip"
	"sort"
	"strings"
	"time"
)

// What follows is how Team describes its customer-owned private network. It says
// nothing about which network program carries the traffic or how certificates are
// made: those are infrastructure (internal/team/infra), written against these terms
// and wired in by the server. "Discovery" is a machine that helps devices find each
// other; "Relay" carries traffic for devices that cannot reach each other directly.

// Reachability is whether a host can be reached from outside its own network, as far
// as it can be known without any outside service.
type Reachability string

const (
	// ReachUnknown: nothing has checked.
	ReachUnknown Reachability = "unknown"
	// ReachPublic: another of the workspace's devices reached it at an address that is
	// public (not private, shared or loopback).
	ReachPublic Reachability = "public"
	// ReachPrivateOnly: it can be reached only on a private network (a LAN, a VPN, an
	// address behind NAT). Devices elsewhere cannot be expected to reach it.
	ReachPrivateOnly Reachability = "private_only"
	// ReachUnreachable: it has a public address and was checked, and did not answer.
	ReachUnreachable Reachability = "unreachable"
)

// Valid reports whether r exists.
func (r Reachability) Valid() bool {
	switch r {
	case ReachUnknown, ReachPublic, ReachPrivateOnly, ReachUnreachable:
		return true
	}
	return false
}

// ApprovalPolicy is whether an administrator must approve a device before it joins.
type ApprovalPolicy string

const (
	// ApprovalAuto: a valid invitation is enough; whoever made it already decided.
	ApprovalAuto ApprovalPolicy = "auto"
	// ApprovalAdmin: every joining device waits for an administrator.
	ApprovalAdmin ApprovalPolicy = "admin"
)

// Valid reports whether p exists.
func (p ApprovalPolicy) Valid() bool { return p == ApprovalAuto || p == ApprovalAdmin }

// NetworkSettings are the public facts about a workspace's private network. Nothing
// in them is secret: the keys that sign for the workspace and the network are not
// here, and are held by Workspace Hosts alone.
type NetworkSettings struct {
	WorkspaceID string `json:"workspaceId"`
	// WorkspaceKey is the workspace's public key and Fingerprint its fingerprint: what a
	// joining device pins.
	WorkspaceKey string `json:"workspaceKey"`
	Fingerprint  string `json:"fingerprint"`
	// NetworkPrefix is the private network's address range (10.201.0.0/16).
	NetworkPrefix string `json:"networkPrefix"`
	// CACertificate is the network authority's certificate (PEM), public.
	CACertificate      string         `json:"caCertificate"`
	EnrollmentApproval ApprovalPolicy `json:"enrollmentApproval"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

// DeviceNetwork is what the workspace knows of a device's place on the network.
type DeviceNetwork struct {
	DeviceID    string `json:"deviceId"`
	WorkspaceID string `json:"workspaceId"`
	// OverlayAddr is the device's address on the private network.
	OverlayAddr string `json:"overlayAddr"`
	// NetworkPublicKey is the public half of the device's network key (PEM).
	NetworkPublicKey string `json:"-"`
	// SealingKey is, for a device that may become a Workspace Host, the public key the
	// workspace's secrets are sealed to for it.
	SealingKey string `json:"-"`
	// Groups are what the device's certificate says it is.
	Groups []string `json:"groups"`
	// Discovery says the device helps others find each other; Relay that it carries
	// traffic for devices that cannot reach each other. Both are for Connectivity Hosts.
	Discovery bool `json:"discovery"`
	Relay     bool `json:"relay"`
	// BootstrapEndpoints are where the device answers new devices' enrollment ("host:port"),
	// and NetworkEndpoints where the network program can be reached from outside.
	BootstrapEndpoints []string     `json:"bootstrapEndpoints"`
	NetworkEndpoints   []string     `json:"networkEndpoints"`
	Reachability       Reachability `json:"reachability"`
	// LastCheckAt is when anything last checked the device's reachability, and
	// LastExternalOKAt when a check from another device, at a public address, last succeeded.
	LastCheckAt      *time.Time `json:"lastCheckAt,omitempty"`
	LastExternalOKAt *time.Time `json:"lastExternalOkAt,omitempty"`
	// ProvisionPending says secrets sealed to this device are waiting for it to collect.
	ProvisionPending bool      `json:"provisionPending"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// NetworkCertificate is a record that the workspace issued a certificate to a device.
// The certificate itself is not kept (the device has it); its fingerprint is, so that
// it can be refused when the device is revoked.
type NetworkCertificate struct {
	Fingerprint string     `json:"fingerprint"`
	DeviceID    string     `json:"deviceId"`
	IssuedAt    time.Time  `json:"issuedAt"`
	NotAfter    time.Time  `json:"notAfter"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}

// EnrollInvitationState is where an invitation is.
type EnrollInvitationState string

const (
	InvitationOpen      EnrollInvitationState = "open"
	InvitationUsed      EnrollInvitationState = "used"
	InvitationWithdrawn EnrollInvitationState = "withdrawn"
)

// EnrollInvitation is the workspace's record of an invitation it made. It never has
// the credential: only a hash of it is kept, and the invitation itself (with the
// credential in it) is shown once, when it is made.
type EnrollInvitation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	// Label is a note for the people who administer: who this is for.
	Label string `json:"label"`
	// ForMemberID is the member the device will belong to when the invitation is for a
	// new device of an existing member; empty when the invitation makes a new member.
	ForMemberID string `json:"forMemberId,omitempty"`
	// Role is what a new member becomes.
	Role         Role         `json:"role"`
	Capabilities []Capability `json:"capabilities"`
	// RequireApproval says an administrator must approve the device before it joins.
	RequireApproval bool                  `json:"requireApproval"`
	State           EnrollInvitationState `json:"state"`
	CreatedBy       string                `json:"createdBy"`
	CreatedAt       time.Time             `json:"createdAt"`
	ExpiresAt       time.Time             `json:"expiresAt"`
	UsedAt          *time.Time            `json:"usedAt,omitempty"`
}

// Usable reports whether the invitation can still be used at now.
func (i EnrollInvitation) Usable(now time.Time) bool {
	return i.State == InvitationOpen && now.Before(i.ExpiresAt)
}

// EnrollmentState is where a device's request to join is.
type EnrollmentState string

const (
	EnrollmentPending  EnrollmentState = "pending"
	EnrollmentApproved EnrollmentState = "approved"
	EnrollmentDenied   EnrollmentState = "denied"
)

// Enrollment is a device's request to join, with what it asked to be.
type Enrollment struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspaceId"`
	InvitationID string          `json:"invitationId"`
	MemberID     string          `json:"memberId,omitempty"`
	MemberName   string          `json:"memberName"`
	DeviceID     string          `json:"deviceId"`
	DeviceName   string          `json:"deviceName"`
	Capabilities []Capability    `json:"capabilities"`
	State        EnrollmentState `json:"state"`
	// Fingerprint is the short fingerprint of the device's public key, for an
	// administrator to compare with what the person reads out.
	Fingerprint string     `json:"fingerprint"`
	RemoteAddr  string     `json:"remoteAddr,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	DecidedBy   string     `json:"decidedBy,omitempty"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
	// Delivered says the device has collected what it was issued.
	Delivered bool `json:"delivered"`

	// DeviceKey, NetworkPublicKey and SealingKey are what the device presented. They are
	// public keys, kept so that an approval can issue what the device asked for.
	DeviceKey        string `json:"-"`
	NetworkPublicKey string `json:"-"`
	SealingKey       string `json:"-"`
}

// NetworkPeer is a machine other devices find the network through.
type NetworkPeer struct {
	Addr      string   `json:"addr"`
	Endpoints []string `json:"endpoints"`
}

// NodeConfig is what the workspace tells a device about the network, in the
// workspace's own terms: what to render into the network program's configuration is
// the infrastructure's business. It holds nothing secret.
type NodeConfig struct {
	DeviceID     string       `json:"deviceId"`
	OverlayAddr  string       `json:"overlayAddr"`
	PrefixBits   int          `json:"prefixBits"`
	Capabilities []Capability `json:"capabilities"`
	Discovery    bool         `json:"discovery"`
	Relay        bool         `json:"relay"`
	// ListenPort is the UDP port a discovery or relay host's network program listens on;
	// 0 lets the system choose, for everything else.
	ListenPort int `json:"listenPort"`
	// Advertise are where this device can be reached from outside, when it is a host.
	Advertise []string `json:"advertise,omitempty"`
	// APIAddrs are the addresses on the private network of the Workspace Hosts, where the
	// workspace's API is served.
	APIAddrs []string `json:"apiAddrs"`
	// DiscoveryHosts are the other discovery hosts, and RelayAddrs the relay hosts.
	DiscoveryHosts []NetworkPeer `json:"discoveryHosts"`
	RelayAddrs     []string      `json:"relays,omitempty"`
	// Blocklist are fingerprints of certificates the network must refuse.
	Blocklist []string `json:"blocklist"`
}

// HostNetworkStatus is one host in the network's health report.
type HostNetworkStatus struct {
	DeviceID     string       `json:"deviceId"`
	Name         string       `json:"name"`
	OwnerID      string       `json:"ownerId"`
	Capabilities []Capability `json:"capabilities"`
	Online       bool         `json:"online"`
	// WorkspaceHost and ConnectivityHost are the roles the device holds and are active.
	WorkspaceHost    bool         `json:"workspaceHost"`
	ConnectivityHost bool         `json:"connectivityHost"`
	Discovery        bool         `json:"discovery"`
	Relay            bool         `json:"relay"`
	Reachability     Reachability `json:"reachability"`
	// AddressKinds says what kind of address each advertised endpoint is.
	Endpoints        []EndpointKind `json:"endpoints"`
	LastCheckAt      *time.Time     `json:"lastCheckAt,omitempty"`
	LastExternalOKAt *time.Time     `json:"lastExternalOkAt,omitempty"`
}

// EndpointKind is an advertised endpoint and what its address is.
type EndpointKind struct {
	Endpoint string `json:"endpoint"`
	Kind     string `json:"kind"` // public, private, shared, loopback, link_local, name
}

// NetworkHealth is what an administrator sees of the network.
type NetworkHealth struct {
	Settings NetworkSettings     `json:"settings"`
	Hosts    []HostNetworkStatus `json:"hosts"`
	// RemoteAccess is "available" when a Connectivity Host has been confirmed reachable
	// from outside, "not_guaranteed" otherwise, and "not_needed" for a workspace whose
	// every device is on one network (nothing says otherwise, so this is never claimed).
	RemoteAccess string `json:"remoteAccess"`
	// Warnings are what to fix, in words.
	Warnings []string `json:"warnings"`
	// RevokedCertificates is how many certificates the network is told to refuse.
	RevokedCertificates int `json:"revokedCertificates"`
}

// Remote access states.
const (
	RemoteAccessAvailable     = "available"
	RemoteAccessNotGuaranteed = "not_guaranteed"
)

// GroupsFor is what a certificate says a device is, from what the workspace has it
// do. A device that holds no host capability is a member's device; a runner is
// reachable by the Workspace Hosts for the workspace's messages. A host that is only a
// host is not a member's device: it serves the workspace and is not used as a client of it.
func GroupsFor(caps []Capability) []string {
	has := map[Capability]bool{}
	for _, c := range caps {
		has[c] = true
	}
	var g []string
	if has[CapabilityWorkspaceHost] {
		g = append(g, "workspace-host")
	}
	if has[CapabilityConnectivityHost] {
		g = append(g, "connectivity-host")
	}
	if has[CapabilityRunner] {
		g = append(g, "runner", "member")
	} else if !has[CapabilityWorkspaceHost] && !has[CapabilityConnectivityHost] {
		g = append(g, "member")
	}
	sort.Strings(g)
	return g
}

// CleanEndpoints checks a list of "host:port" endpoints and returns it without
// duplicates and in a stable order.
func CleanEndpoints(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range in {
		e = strings.TrimSpace(e)
		host, port, ok := splitHostPort(e)
		if !ok || host == "" || port == "" || len(e) > 255 || strings.ContainsAny(e, " \t\r\n\x00/\\\"") {
			return nil, invalid("endpoint %q is not host:port", e)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	if len(out) > 8 {
		return nil, invalid("at most 8 endpoints")
	}
	sort.Strings(out)
	return out, nil
}

func splitHostPort(s string) (host, port string, ok bool) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 || end+2 > len(s) || s[end+1] != ':' {
			return "", "", false
		}
		host, port = s[1:end], s[end+2:]
	} else {
		i := strings.LastIndex(s, ":")
		if i < 0 || strings.Contains(s[:i], ":") {
			return "", "", false
		}
		host, port = s[:i], s[i+1:]
	}
	n := 0
	for _, r := range port {
		if r < '0' || r > '9' || n > 65535 {
			return "", "", false
		}
		n = n*10 + int(r-'0')
	}
	return host, port, n >= 1 && n <= 65535
}

// Kinds of address an endpoint can have.
const (
	AddrPublic    = "public"
	AddrPrivate   = "private"    // RFC 1918 and unique-local: a LAN
	AddrShared    = "shared"     // 100.64.0.0/10, the carrier-grade NAT range
	AddrLoopback  = "loopback"   // this machine only
	AddrLinkLocal = "link_local" // one link only
	AddrName      = "name"       // a DNS name: where it leads is not known from here
)

var sharedRange = netip.MustParsePrefix("100.64.0.0/10")

// AddressKind says what kind of address the host part of an endpoint ("host:port" or
// a bare host) is. It is the only thing that can be said about reachability without
// asking something to reach it: a private, shared or loopback address cannot be reached
// from another network, and a public address might be.
func AddressKind(endpoint string) string {
	host := endpoint
	if h, _, ok := splitHostPort(endpoint); ok {
		host = h
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return AddrName
	}
	addr = addr.Unmap()
	switch {
	case addr.IsLoopback():
		return AddrLoopback
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsUnspecified():
		return AddrLinkLocal
	case addr.IsPrivate():
		return AddrPrivate
	case sharedRange.Contains(addr):
		return AddrShared
	case addr.IsGlobalUnicast():
		return AddrPublic
	}
	return AddrLinkLocal
}

// ExternallyAddressable reports whether a kind of address could be reached from
// another network.
func ExternallyAddressable(kind string) bool { return kind == AddrPublic || kind == AddrName }

// EndpointPort returns the port of a "host:port" endpoint.
func EndpointPort(endpoint string) (int, bool) {
	_, port, ok := splitHostPort(endpoint)
	if !ok {
		return 0, false
	}
	n := 0
	for _, r := range port {
		n = n*10 + int(r-'0')
	}
	return n, true
}
