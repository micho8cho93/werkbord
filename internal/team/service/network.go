package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/enrollment"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// The customer-owned private network.
//
// This file is the workspace's side of it: who is on the network and at which
// address, which certificates were issued and which are refused, what each host is
// for and whether it can be reached, and what an administrator should fix. It is
// written against NetworkAuthority, which is what makes certificates and renders
// configuration; the service knows nothing of how.
//
// Nothing here talks to anything outside the customer's own machines. The network,
// its authority and every host are the customer's; Werkbord operates none of it.

// NetworkInfo describes the workspace's network identity. It is public.
type NetworkInfo struct {
	WorkspaceKey  string
	Fingerprint   string
	Prefix        netip.Prefix
	CACertificate string
}

// CertificateRequest is what the workspace asks its authority to certify.
type CertificateRequest struct {
	DeviceID         string
	Addr             netip.Addr
	Capabilities     []domain.Capability
	NetworkPublicKey string
	TTL              time.Duration
}

// IssuedCertificate is the authority's answer.
type IssuedCertificate struct {
	CertificatePEM string
	Fingerprint    string
	NotAfter       time.Time
}

// NetworkAuthority is what the service needs of the infrastructure behind the
// network. It exists only on a Workspace Host that holds the workspace's keys; any
// operation fails on a host that does not.
type NetworkAuthority interface {
	Info() NetworkInfo
	// SignInvitation signs an invitation with the workspace key.
	SignInvitation(inv enrollment.Invitation) (string, error)
	// IssueCertificate signs a node certificate for a device's network key.
	IssueCertificate(req CertificateRequest, now time.Time) (IssuedCertificate, error)
	// RenderConfig renders the configuration of a node, as a template in which
	// enrollment.ConfigDirToken stands for the directory the device keeps its files in.
	RenderConfig(cfg domain.NodeConfig) (string, error)
	// SealSecrets encrypts the workspace's signing material to a host's sealing key.
	SealSecrets(sealingKey, deviceID string) ([]byte, error)
}

// SetNetwork gives the service the authority behind this host's network. A server
// without one (no private network configured, or a host that has not been given the
// keys) leaves it nil, and the network's operations say so.
func (s *Service) SetNetwork(n NetworkAuthority) { s.net = n }

func errNoNetwork() error {
	return fmt.Errorf("%w: this Team deployment has no private network configured on this host (see docs/TEAM_NETWORK.md)", domain.ErrConflict)
}

func (s *Service) needNetwork() (NetworkAuthority, error) {
	if s.net == nil {
		return nil, errNoNetwork()
	}
	return s.net, nil
}

// ---- starting the network of the first workspace ----

// HostBootstrap is what the first Workspace Host brings when it creates its workspace's
// network.
type HostBootstrap struct {
	// Device is the host's public identity, and Proof the signature over
	// deviceid.RegistrationStatement that shows it holds the key.
	Device deviceid.Public
	Proof  []byte
	// NetworkPublicKey is the host's network public key (PEM), SealingKey its sealing key.
	NetworkPublicKey string
	SealingKey       string
	// BootstrapEndpoints and NetworkEndpoints are where it can be reached from outside.
	BootstrapEndpoints []string
	NetworkEndpoints   []string
	// Connectivity makes it a Connectivity Host too (discovery and relay). The caller
	// decides that from whether it can be reached; see ConnectivityPossible.
	Connectivity bool
	Approval     domain.ApprovalPolicy
}

// ConnectivityPossible says whether a set of advertised endpoints meets what a
// Connectivity Host needs: at least one address that another network could reach. It
// does not say it can be, only that it is not impossible; a check does that.
func ConnectivityPossible(networkEndpoints []string) bool {
	for _, e := range networkEndpoints {
		if domain.ExternallyAddressable(domain.AddressKind(e)) {
			return true
		}
	}
	return false
}

// BootstrapNetwork gives a new workspace its private network, on the host that made
// it: the network's public settings, the host as the first Workspace Host (and a
// Connectivity Host, if asked), its address and its first certificate. It is what
// `werkbord-team workspace create` does after the workspace exists, and it is not
// reachable over HTTP.
func (s *Service) BootstrapNetwork(ctx context.Context, workspaceID, ownerID string, in HostBootstrap) (domain.DeviceNetwork, error) {
	net, err := s.needNetwork()
	if err != nil {
		return domain.DeviceNetwork{}, err
	}
	info := net.Info()
	if in.Approval == "" {
		in.Approval = domain.ApprovalAuto
	}
	if !in.Approval.Valid() {
		return domain.DeviceNetwork{}, fmt.Errorf("%w: enrollment approval is auto or admin", domain.ErrInvalid)
	}
	name, err := deviceid.CleanName(in.Device.Name)
	if err != nil {
		return domain.DeviceNetwork{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	in.Device.Name = name
	if err := in.Device.Validate(); err != nil {
		return domain.DeviceNetwork{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if err := deviceid.VerifyRegistration(workspaceID, ownerID, in.Device, in.Proof); err != nil {
		return domain.DeviceNetwork{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	boot, err := domain.CleanEndpoints(in.BootstrapEndpoints)
	if err != nil {
		return domain.DeviceNetwork{}, err
	}
	nets, err := domain.CleanEndpoints(in.NetworkEndpoints)
	if err != nil {
		return domain.DeviceNetwork{}, err
	}
	if in.Connectivity && !ConnectivityPossible(nets) {
		return domain.DeviceNetwork{}, fmt.Errorf("%w: a Connectivity Host needs an address that can be reached from outside this network; none of %v is one", domain.ErrInvalid, nets)
	}
	caps := []domain.Capability{domain.CapabilityWorkspaceHost}
	if in.Connectivity {
		caps = append(caps, domain.CapabilityConnectivityHost)
	}
	now := s.stamp()
	var out domain.DeviceNetwork
	err = s.db.Update(ctx, func(tx store.Tx) error {
		if _, err := tx.NetworkSettings(ctx, workspaceID); err == nil {
			return fmt.Errorf("%w: this workspace already has a private network", domain.ErrConflict)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if _, err := tx.Workspace(ctx, workspaceID); err != nil {
			return err
		}
		if err := tx.InsertNetworkSettings(ctx, domain.NetworkSettings{WorkspaceID: workspaceID, WorkspaceKey: info.WorkspaceKey, Fingerprint: info.Fingerprint,
			NetworkPrefix: info.Prefix.String(), CACertificate: info.CACertificate, EnrollmentApproval: in.Approval, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		dev := domain.Device{ID: string(in.Device.ID), WorkspaceID: workspaceID, MemberID: ownerID, Name: name, PublicKey: in.Device.PublicKey,
			Capabilities: caps, HostStatus: domain.HostActive, ConnectivityStatus: initialStatus(caps, domain.CapabilityConnectivityHost), CreatedAt: now, UpdatedAt: now}
		if in.Connectivity {
			dev.ConnectivityStatus = domain.HostActive
		}
		if err := tx.InsertDevice(ctx, dev); err != nil {
			return err
		}
		n, _, err := s.placeOnNetwork(ctx, tx, net, info, dev, in.NetworkPublicKey, in.SealingKey, now)
		if err != nil {
			return err
		}
		n.BootstrapEndpoints, n.NetworkEndpoints = boot, nets
		// A Workspace Host helps devices find each other as soon as it says where it is: on one LAN that is the
		// whole of what the network needs. Relaying is for a Connectivity Host.
		n.Discovery, n.Relay = len(nets) > 0, in.Connectivity
		n.Reachability = classifyUnchecked(nets, boot)
		n.UpdatedAt = now
		if err := tx.SaveDeviceNetwork(ctx, n); err != nil {
			return err
		}
		out = n
		return nil
	})
	s.changed(workspaceID, err)
	return out, err
}

// placeOnNetwork gives a registered device an address, a first certificate and a
// record of both. It runs inside the caller's transaction.
func (s *Service) placeOnNetwork(ctx context.Context, tx store.Tx, net NetworkAuthority, info NetworkInfo, dev domain.Device, networkKey, sealingKey string, now time.Time) (domain.DeviceNetwork, IssuedCertificate, error) {
	existing, err := tx.DeviceNetworks(ctx, dev.WorkspaceID)
	if err != nil {
		return domain.DeviceNetwork{}, IssuedCertificate{}, err
	}
	used := map[netip.Addr]bool{}
	for _, e := range existing {
		if a, err := netip.ParseAddr(e.OverlayAddr); err == nil {
			used[a] = true
		}
	}
	addr, err := nextAddress(info.Prefix, used)
	if err != nil {
		return domain.DeviceNetwork{}, IssuedCertificate{}, err
	}
	issued, err := net.IssueCertificate(CertificateRequest{DeviceID: dev.ID, Addr: addr, Capabilities: dev.Capabilities, NetworkPublicKey: networkKey}, now)
	if err != nil {
		return domain.DeviceNetwork{}, IssuedCertificate{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	n := domain.DeviceNetwork{DeviceID: dev.ID, WorkspaceID: dev.WorkspaceID, OverlayAddr: addr.String(), NetworkPublicKey: networkKey, SealingKey: sealingKey,
		Groups: domain.GroupsFor(dev.Capabilities), BootstrapEndpoints: []string{}, NetworkEndpoints: []string{}, Reachability: domain.ReachUnknown, CreatedAt: now, UpdatedAt: now}
	if err := tx.InsertDeviceNetwork(ctx, n); err != nil {
		return domain.DeviceNetwork{}, IssuedCertificate{}, err
	}
	if err := tx.InsertCertificate(ctx, dev.WorkspaceID, domain.NetworkCertificate{Fingerprint: issued.Fingerprint, DeviceID: dev.ID, IssuedAt: now, NotAfter: issued.NotAfter}); err != nil {
		return domain.DeviceNetwork{}, IssuedCertificate{}, err
	}
	return n, issued, nil
}

// nextAddress is the lowest address in the network that is free, never the network's own.
func nextAddress(prefix netip.Prefix, used map[netip.Addr]bool) (netip.Addr, error) {
	for a := prefix.Addr().Next(); prefix.Contains(a); a = a.Next() {
		if !used[a] && prefix.Contains(a.Next()) { // never the last (broadcast) address
			return a, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%w: the private network %v has no free address", domain.ErrConflict, prefix)
}

// ---- reading the network ----

// NetworkSettings returns the workspace's network settings.
func (s *Service) NetworkSettings(ctx context.Context, a Actor) (domain.NetworkSettings, error) {
	var out domain.NetworkSettings
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.NetworkSettings(ctx, a.Workspace.ID); return })
	if errors.Is(err, domain.ErrNotFound) {
		return out, errNoNetwork()
	}
	return out, err
}

// SetEnrollmentApproval chooses whether an administrator must approve joining devices.
func (s *Service) SetEnrollmentApproval(ctx context.Context, a Actor, p domain.ApprovalPolicy) error {
	if err := a.require(domain.PermDevicesManage, "change who may join the network"); err != nil {
		return err
	}
	if !p.Valid() {
		return fmt.Errorf("%w: enrollment approval is auto or admin", domain.ErrInvalid)
	}
	return s.db.Update(ctx, func(tx store.Tx) error {
		if _, err := tx.NetworkSettings(ctx, a.Workspace.ID); err != nil {
			return errNoNetwork()
		}
		return tx.SetEnrollmentApproval(ctx, a.Workspace.ID, p, s.stamp())
	})
}

// NodeConfigOf computes what the network should be told about one device: its
// address, which hosts help others find each other and relay for them, and which
// certificates to refuse. It is used for a device's own node (by the server, for a
// host) and for what a device is sent. It holds nothing secret.
func (s *Service) NodeConfigOf(ctx context.Context, workspaceID, deviceID string) (domain.NodeConfig, error) {
	var cfg domain.NodeConfig
	err := s.db.View(ctx, func(tx store.Tx) (err error) { cfg, err = s.nodeConfig(ctx, tx, workspaceID, deviceID); return })
	return cfg, err
}

func (s *Service) nodeConfig(ctx context.Context, tx store.Tx, workspaceID, deviceID string) (domain.NodeConfig, error) {
	settings, err := tx.NetworkSettings(ctx, workspaceID)
	if err != nil {
		return domain.NodeConfig{}, errNoNetwork()
	}
	prefix, err := netip.ParsePrefix(settings.NetworkPrefix)
	if err != nil {
		return domain.NodeConfig{}, err
	}
	dev, err := tx.Device(ctx, workspaceID, deviceID)
	if err != nil {
		return domain.NodeConfig{}, err
	}
	if dev.Revoked() {
		return domain.NodeConfig{}, fmt.Errorf("%w: the device has been revoked", domain.ErrConflict)
	}
	self, err := tx.DeviceNetwork(ctx, workspaceID, deviceID)
	if err != nil {
		return domain.NodeConfig{}, err
	}
	all, err := tx.DeviceNetworks(ctx, workspaceID)
	if err != nil {
		return domain.NodeConfig{}, err
	}
	devs, err := tx.Devices(ctx, workspaceID, "")
	if err != nil {
		return domain.NodeConfig{}, err
	}
	live := map[string]domain.Device{}
	for _, d := range devs {
		if !d.Revoked() {
			live[d.ID] = d
		}
	}
	cfg := domain.NodeConfig{DeviceID: deviceID, OverlayAddr: self.OverlayAddr, PrefixBits: prefix.Bits(), Capabilities: dev.Capabilities,
		Discovery: self.Discovery && isHost(dev), Relay: self.Relay && dev.Has(domain.CapabilityConnectivityHost),
		Advertise: self.NetworkEndpoints, DiscoveryHosts: []domain.NetworkPeer{}, Blocklist: []string{}, APIAddrs: []string{}}
	if cfg.Discovery || cfg.Relay {
		cfg.ListenPort = portOf(self.NetworkEndpoints, DefaultNetworkPort)
	}
	for _, o := range all {
		d, ok := live[o.DeviceID]
		if ok && d.Has(domain.CapabilityWorkspaceHost) && d.HostStatus != domain.HostNone {
			cfg.APIAddrs = append(cfg.APIAddrs, o.OverlayAddr)
		}
		if !ok || o.DeviceID == deviceID {
			continue
		}
		if o.Discovery && isHost(d) && len(o.NetworkEndpoints) > 0 {
			cfg.DiscoveryHosts = append(cfg.DiscoveryHosts, domain.NetworkPeer{Addr: o.OverlayAddr, Endpoints: o.NetworkEndpoints})
		}
		if o.Relay && d.Has(domain.CapabilityConnectivityHost) {
			cfg.RelayAddrs = append(cfg.RelayAddrs, o.OverlayAddr)
		}
	}
	cfg.Blocklist, err = tx.Blocklist(ctx, workspaceID, s.stamp())
	return cfg, err
}

// isHost says a device runs the workspace or connects it: either may help other devices find each other.
func isHost(d domain.Device) bool {
	return d.Has(domain.CapabilityWorkspaceHost) || d.Has(domain.CapabilityConnectivityHost)
}

// DefaultNetworkPort is the UDP port a Connectivity Host's network program listens on
// unless its advertised endpoint says another.
const DefaultNetworkPort = 4242

func portOf(endpoints []string, def int) int {
	for _, e := range endpoints {
		if p, ok := domain.EndpointPort(e); ok {
			return p
		}
	}
	return def
}

// ---- a device's own view ----

// NetworkBundle is what a device receives to join, or to stay on, the network.
type NetworkBundle struct {
	CACertificate string
	Certificate   string
	Config        string
	OverlayAddr   string
	ExpiresAt     time.Time
	Discovery     []enrollment.Peer
	Relays        []string
	APIAddrs      []string
	// Node is the same, in the workspace's own terms, for a program that renders its own configuration
	// (a Workspace Host, which runs the network node itself) and not only for one that runs Config as it is.
	Node domain.NodeConfig
}

// bundleFor builds a device's bundle from its current certificate.
func (s *Service) bundleFor(ctx context.Context, tx store.Tx, net NetworkAuthority, workspaceID, deviceID string, issued IssuedCertificate) (NetworkBundle, error) {
	settings, err := tx.NetworkSettings(ctx, workspaceID)
	if err != nil {
		return NetworkBundle{}, errNoNetwork()
	}
	cfg, err := s.nodeConfig(ctx, tx, workspaceID, deviceID)
	if err != nil {
		return NetworkBundle{}, err
	}
	text, err := net.RenderConfig(cfg)
	if err != nil {
		return NetworkBundle{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	b := NetworkBundle{CACertificate: settings.CACertificate, Certificate: issued.CertificatePEM, Config: text, OverlayAddr: cfg.OverlayAddr, ExpiresAt: issued.NotAfter, Relays: cfg.RelayAddrs, APIAddrs: cfg.APIAddrs, Node: cfg}
	for _, p := range cfg.DiscoveryHosts {
		b.Discovery = append(b.Discovery, enrollment.Peer{OverlayAddr: p.Addr, Endpoints: p.Endpoints})
	}
	return b, nil
}

// Wire is the bundle as the protocol and the API carry it.
func (b NetworkBundle) Wire() *enrollment.NetworkBundle { return toWire(b) }

func toWire(b NetworkBundle) *enrollment.NetworkBundle {
	node, _ := json.Marshal(b.Node)
	return &enrollment.NetworkBundle{CACertificate: b.CACertificate, NodeCertificate: b.Certificate, Config: b.Config, OverlayAddr: b.OverlayAddr,
		ExpiresAt: b.ExpiresAt.Unix(), Discovery: b.Discovery, Relays: b.Relays, APIAddrs: b.APIAddrs, Node: node}
}

// RenewCertificate issues a device a new certificate for the same network key, and
// returns it with the current configuration. It needs the device's own credential: a
// revoked device has none, and is refused here however it reached the host. It is how
// a working device stays on a network whose certificates are short-lived, and how one
// whose membership has ended falls off it.
func (s *Service) RenewCertificate(ctx context.Context, a Actor) (NetworkBundle, error) {
	if a.Device == nil {
		return NetworkBundle{}, fmt.Errorf("%w: a device's own credential is needed to renew its certificate", domain.ErrForbidden)
	}
	return s.renew(ctx, a.Workspace.ID, a.Device.ID)
}

// RenewLocalCertificate is RenewCertificate for the host that is running this server,
// asking for its own: there is no request, so no credential, and it is not reachable
// over HTTP.
func (s *Service) RenewLocalCertificate(ctx context.Context, workspaceID, deviceID string) (NetworkBundle, error) {
	return s.renew(ctx, workspaceID, deviceID)
}

func (s *Service) renew(ctx context.Context, workspaceID, deviceID string) (NetworkBundle, error) {
	net, err := s.needNetwork()
	if err != nil {
		return NetworkBundle{}, err
	}
	var out NetworkBundle
	err = s.db.Update(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, workspaceID, deviceID)
		if err != nil {
			return err
		}
		if dev.Revoked() {
			return domain.ErrUnauthenticated
		}
		n, err := tx.DeviceNetwork(ctx, workspaceID, dev.ID)
		if err != nil {
			return err
		}
		addr, err := netip.ParseAddr(n.OverlayAddr)
		if err != nil {
			return err
		}
		now := s.stamp()
		issued, err := net.IssueCertificate(CertificateRequest{DeviceID: dev.ID, Addr: addr, Capabilities: dev.Capabilities, NetworkPublicKey: n.NetworkPublicKey}, now)
		if err != nil {
			return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
		if err := tx.InsertCertificate(ctx, workspaceID, domain.NetworkCertificate{Fingerprint: issued.Fingerprint, DeviceID: dev.ID, IssuedAt: now, NotAfter: issued.NotAfter}); err != nil {
			return err
		}
		n.Groups, n.UpdatedAt = domain.GroupsFor(dev.Capabilities), now
		if err := tx.SaveDeviceNetwork(ctx, n); err != nil {
			return err
		}
		out, err = s.bundleFor(ctx, tx, net, workspaceID, dev.ID, issued)
		return err
	})
	return out, err
}

// DeviceNetworkConfig returns the configuration a device should be running now,
// without issuing anything: the way a device learns of a new discovery host or a longer
// blocklist between renewals.
func (s *Service) DeviceNetworkConfig(ctx context.Context, a Actor) (NetworkBundle, error) {
	net, err := s.needNetwork()
	if err != nil {
		return NetworkBundle{}, err
	}
	if a.Device == nil {
		return NetworkBundle{}, fmt.Errorf("%w: a device's own credential is needed", domain.ErrForbidden)
	}
	var out NetworkBundle
	err = s.db.View(ctx, func(tx store.Tx) error {
		out, err = s.bundleFor(ctx, tx, net, a.Workspace.ID, a.Device.ID, IssuedCertificate{})
		return err
	})
	return out, err
}

// NetworkPatch is a change to what a host says about itself on the network.
type NetworkPatch struct {
	BootstrapEndpoints *[]string
	NetworkEndpoints   *[]string
	Discovery          *bool
	Relay              *bool
}

// SetDeviceNetwork changes where a host can be reached and what it does for the
// network. A host may say where it can be reached; making it a discovery host or a
// relay is for someone who manages devices.
func (s *Service) SetDeviceNetwork(ctx context.Context, a Actor, deviceID string, p NetworkPatch) (domain.DeviceNetwork, error) {
	self := a.Device != nil && a.Device.ID == deviceID
	if (p.Discovery != nil || p.Relay != nil || !self) && !a.Member.Can(domain.PermDevicesManage) {
		return domain.DeviceNetwork{}, forbidden("change what a device does for the network")
	}
	var out domain.DeviceNetwork
	err := s.db.Update(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		if dev.Revoked() {
			return fmt.Errorf("%w: the device has been revoked", domain.ErrConflict)
		}
		n, err := tx.DeviceNetwork(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		if p.BootstrapEndpoints != nil {
			if n.BootstrapEndpoints, err = domain.CleanEndpoints(*p.BootstrapEndpoints); err != nil {
				return err
			}
		}
		if p.NetworkEndpoints != nil {
			if n.NetworkEndpoints, err = domain.CleanEndpoints(*p.NetworkEndpoints); err != nil {
				return err
			}
		}
		if p.Discovery != nil {
			n.Discovery = *p.Discovery
		}
		if p.Relay != nil {
			n.Relay = *p.Relay
		}
		if n.Discovery && !isHost(dev) {
			return fmt.Errorf("%w: only a Workspace Host or a Connectivity Host can help others find each other", domain.ErrInvalid)
		}
		if n.Relay && !dev.Has(domain.CapabilityConnectivityHost) {
			return fmt.Errorf("%w: only a Connectivity Host can relay traffic for others", domain.ErrInvalid)
		}
		if (n.Discovery || n.Relay) && len(n.NetworkEndpoints) == 0 {
			return fmt.Errorf("%w: a discovery or relay host must say where it can be reached", domain.ErrInvalid)
		}
		n.UpdatedAt = s.stamp()
		if p.NetworkEndpoints != nil || p.BootstrapEndpoints != nil {
			n.Reachability, n.LastCheckAt, n.LastExternalOKAt = classifyUnchecked(n.NetworkEndpoints, n.BootstrapEndpoints), nil, nil
		}
		if err := tx.SaveDeviceNetwork(ctx, n); err != nil {
			return err
		}
		out, err = tx.DeviceNetwork(ctx, a.Workspace.ID, deviceID)
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// classifyUnchecked is what can be said of a host's reachability before anything has
// checked it: if every address it advertises is private, shared or loopback, devices
// elsewhere cannot reach it; otherwise nothing is known yet.
func classifyUnchecked(endpoints ...[]string) domain.Reachability {
	any := false
	for _, list := range endpoints {
		for _, e := range list {
			any = true
			if domain.ExternallyAddressable(domain.AddressKind(e)) {
				return domain.ReachUnknown
			}
		}
	}
	if any {
		return domain.ReachPrivateOnly
	}
	return domain.ReachUnknown
}

// ReachReport is the outcome of one device trying to reach a host.
type ReachReport struct {
	// Endpoint is the address that was tried; it must be one the host advertises.
	Endpoint string
	OK       bool
}

// RecordReachability records that a device tried to reach a host, and what happened.
// Whoever reports must be a device of the workspace or someone who manages devices.
// A check made by the host itself can show an address answers; it cannot show the
// address can be reached from elsewhere, and is not counted as an external check.
func (s *Service) RecordReachability(ctx context.Context, a Actor, hostID string, r ReachReport) error {
	if a.Device == nil && !a.Member.Can(domain.PermDevicesManage) {
		return forbidden("report on whether a host can be reached")
	}
	reporter := ""
	if a.Device != nil {
		reporter = a.Device.ID
	}
	return s.recordReach(ctx, a.Workspace.ID, reporter, hostID, r)
}

// RecordLocalReachability is RecordReachability for a check this host made itself.
func (s *Service) RecordLocalReachability(ctx context.Context, workspaceID, reporterDeviceID, hostID string, r ReachReport) error {
	return s.recordReach(ctx, workspaceID, reporterDeviceID, hostID, r)
}

func (s *Service) recordReach(ctx context.Context, workspaceID, reporterID, hostID string, r ReachReport) error {
	return s.db.Update(ctx, func(tx store.Tx) error {
		n, err := tx.DeviceNetwork(ctx, workspaceID, hostID)
		if err != nil {
			return err
		}
		known := false
		for _, e := range append(append([]string(nil), n.NetworkEndpoints...), n.BootstrapEndpoints...) {
			known = known || e == r.Endpoint
		}
		if !known {
			return fmt.Errorf("%w: %q is not an address that host advertises", domain.ErrInvalid, r.Endpoint)
		}
		kind := domain.AddressKind(r.Endpoint)
		var reach domain.Reachability
		switch {
		case !domain.ExternallyAddressable(kind):
			reach = domain.ReachPrivateOnly
		case r.OK:
			reach = domain.ReachPublic
		default:
			reach = domain.ReachUnreachable
		}
		external := r.OK && domain.ExternallyAddressable(kind) && reporterID != hostID
		return tx.SetReachability(ctx, workspaceID, hostID, reach, s.stamp(), external)
	})
}

// ProbeTarget is a host and where it says it answers new devices.
type ProbeTarget struct {
	DeviceID  string
	Endpoints []string
}

// ProbeTargets lists the unrevoked hosts that advertise somewhere to be reached at, for
// a host's own reachability checks of the others (and itself).
func (s *Service) ProbeTargets(ctx context.Context, workspaceID string) ([]ProbeTarget, error) {
	var out []ProbeTarget
	err := s.db.View(ctx, func(tx store.Tx) error {
		devs, err := tx.Devices(ctx, workspaceID, "")
		if err != nil {
			return err
		}
		nets, err := tx.DeviceNetworks(ctx, workspaceID)
		if err != nil {
			return err
		}
		live := map[string]domain.Device{}
		for _, d := range devs {
			if !d.Revoked() {
				live[d.ID] = d
			}
		}
		for _, n := range nets {
			d, ok := live[n.DeviceID]
			if ok && (d.Has(domain.CapabilityWorkspaceHost) || d.Has(domain.CapabilityConnectivityHost)) && len(n.BootstrapEndpoints) > 0 {
				out = append(out, ProbeTarget{DeviceID: n.DeviceID, Endpoints: n.BootstrapEndpoints})
			}
		}
		return nil
	})
	return out, err
}

// ---- health ----

// NetworkHealth reports the network as an administrator needs to see it: each host,
// whether it can be reached, and what is missing. It does not claim what it cannot
// know: when no Connectivity Host has been confirmed reachable from outside, it says
// remote access cannot be guaranteed, because it cannot.
func (s *Service) NetworkHealth(ctx context.Context, a Actor) (domain.NetworkHealth, error) {
	if err := a.require(domain.PermDevicesViewAll, "see the network's health"); err != nil {
		return domain.NetworkHealth{}, err
	}
	var h domain.NetworkHealth
	now := s.stamp()
	err := s.db.View(ctx, func(tx store.Tx) error {
		settings, err := tx.NetworkSettings(ctx, a.Workspace.ID)
		if err != nil {
			return errNoNetwork()
		}
		h.Settings = settings
		devs, err := tx.Devices(ctx, a.Workspace.ID, "")
		if err != nil {
			return err
		}
		nets, err := tx.DeviceNetworks(ctx, a.Workspace.ID)
		if err != nil {
			return err
		}
		byNet := map[string]domain.DeviceNetwork{}
		for _, n := range nets {
			byNet[n.DeviceID] = n
		}
		bl, err := tx.Blocklist(ctx, a.Workspace.ID, now)
		if err != nil {
			return err
		}
		h.RevokedCertificates = len(bl)
		h.Hosts = []domain.HostNetworkStatus{}
		for _, d := range devs {
			if d.Revoked() || !(d.Has(domain.CapabilityWorkspaceHost) || d.Has(domain.CapabilityConnectivityHost)) {
				continue
			}
			n := byNet[d.ID]
			st := domain.HostNetworkStatus{DeviceID: d.ID, Name: d.Name, OwnerID: d.MemberID, Capabilities: d.Capabilities, Online: d.OnlineAt(now),
				WorkspaceHost:    d.Has(domain.CapabilityWorkspaceHost) && d.HostStatus == domain.HostActive,
				ConnectivityHost: d.Has(domain.CapabilityConnectivityHost) && d.ConnectivityStatus == domain.HostActive,
				Discovery:        n.Discovery, Relay: n.Relay, Reachability: n.Reachability, LastCheckAt: n.LastCheckAt, LastExternalOKAt: n.LastExternalOKAt, Endpoints: []domain.EndpointKind{}}
			if st.Reachability == "" {
				st.Reachability = domain.ReachUnknown
			}
			for _, e := range append(append([]string(nil), n.NetworkEndpoints...), n.BootstrapEndpoints...) {
				st.Endpoints = append(st.Endpoints, domain.EndpointKind{Endpoint: e, Kind: domain.AddressKind(e)})
			}
			h.Hosts = append(h.Hosts, st)
		}
		sort.Slice(h.Hosts, func(i, j int) bool { return h.Hosts[i].Name < h.Hosts[j].Name })
		h.RemoteAccess, h.Warnings = assess(h.Hosts)
		return nil
	})
	return h, err
}

// assess is the judgement of NetworkHealth, separate so it can be tested on its own.
func assess(hosts []domain.HostNetworkStatus) (string, []string) {
	var warn []string
	var conn, connOK, discovery, relays, workspace int
	for _, h := range hosts {
		if h.WorkspaceHost {
			workspace++
		}
		if h.Discovery {
			discovery++
		}
		if !h.ConnectivityHost {
			continue
		}
		conn++
		if h.Relay {
			relays++
		}
		if h.Reachability == domain.ReachPublic && h.LastExternalOKAt != nil {
			connOK++
		}
	}
	remote := domain.RemoteAccessNotGuaranteed
	switch {
	case conn == 0:
		warn = append(warn, "No Connectivity Host: devices on different networks cannot be guaranteed to reach each other. Remote access needs at least one machine you own that can be reached from the Internet (and two, so that one can be down). Without one, only devices on the same network can be relied on.")
	case connOK == 0:
		warn = append(warn, "No Connectivity Host has been confirmed reachable from outside: remote access cannot be guaranteed. A host behind a home router, a company firewall or carrier-grade NAT may not be reachable from elsewhere even with a public-looking address. Check that its address is public and its port is open, then run a check from a device on another network.")
	default:
		remote = domain.RemoteAccessAvailable
		if connOK == 1 {
			warn = append(warn, "Only one Connectivity Host is confirmed reachable: if it is offline, devices on different networks lose each other. Add a second.")
		}
	}
	switch {
	case discovery == 0:
		warn = append(warn, "No host helps devices find each other: a device cannot learn where the workspace's machines are. Say where a host can be reached (WERKBORD_TEAM_ENDPOINTS) and enable discovery on it.")
	case discovery == 1:
		warn = append(warn, "Only one host helps devices find each other. Devices can use more than one; add another so that none is a single point of failure.")
	}
	if conn > 0 && relays == 0 {
		warn = append(warn, "No Connectivity Host relays traffic: devices that cannot reach each other directly (both behind strict NAT) will not connect. Enable relaying on a reachable host.")
	}
	if workspace == 0 {
		warn = append(warn, "No active Workspace Host.")
	}
	return remote, warn
}

// ---- handing the workspace's authority to another host ----

// ProvisionHost seals the workspace's signing material to a device that has been given
// the Workspace Host capability, for it to collect. Only a host that holds that
// material can; the ciphertext is stored, and only the device it was sealed to can open
// it (it needs the private half of the sealing key it presented when it enrolled). The
// material is never in a response to anyone else, and is removed once collected.
func (s *Service) ProvisionHost(ctx context.Context, a Actor, deviceID string) error {
	if err := a.require(domain.PermDevicesManage, "make a device a Workspace Host"); err != nil {
		return err
	}
	net, err := s.needNetwork()
	if err != nil {
		return err
	}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		if dev.Revoked() || !dev.Has(domain.CapabilityWorkspaceHost) {
			return fmt.Errorf("%w: the device must be an unrevoked device that has been given the Workspace Host capability", domain.ErrConflict)
		}
		n, err := tx.DeviceNetwork(ctx, a.Workspace.ID, deviceID)
		if err != nil {
			return err
		}
		if n.SealingKey == "" {
			return fmt.Errorf("%w: the device did not present a sealing key when it enrolled, so secrets cannot be sent to it; enroll it again as a Workspace Host", domain.ErrConflict)
		}
		sealed, err := net.SealSecrets(n.SealingKey, deviceID)
		if err != nil {
			return fmt.Errorf("%w: %v", domain.ErrConflict, err)
		}
		return tx.SetProvision(ctx, a.Workspace.ID, deviceID, sealed, s.stamp())
	})
	s.changed(a.Workspace.ID, err)
	return err
}

// CollectProvision gives a device the secrets sealed to it, if there are any. They are
// ciphertext: anything that is not the device cannot read them.
func (s *Service) CollectProvision(ctx context.Context, a Actor) ([]byte, error) {
	if a.Device == nil {
		return nil, fmt.Errorf("%w: a device's own credential is needed", domain.ErrForbidden)
	}
	var out []byte
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.Provision(ctx, a.Workspace.ID, a.Device.ID); return })
	if err == nil && len(out) == 0 {
		return nil, fmt.Errorf("%w: nothing is waiting for this device", domain.ErrNotFound)
	}
	return out, err
}

// AcknowledgeProvision records that a device has received and stored the secrets: it is
// a Workspace Host now, and what was waiting for it is removed.
func (s *Service) AcknowledgeProvision(ctx context.Context, a Actor) error {
	if a.Device == nil {
		return fmt.Errorf("%w: a device's own credential is needed", domain.ErrForbidden)
	}
	err := s.db.Update(ctx, func(tx store.Tx) error {
		dev, err := tx.Device(ctx, a.Workspace.ID, a.Device.ID)
		if err != nil {
			return err
		}
		if err := tx.ClearProvision(ctx, a.Workspace.ID, dev.ID, s.stamp()); err != nil {
			return err
		}
		if dev.Has(domain.CapabilityWorkspaceHost) {
			dev.HostStatus, dev.UpdatedAt = domain.HostActive, s.stamp()
			return tx.SaveDevice(ctx, dev)
		}
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return err
}

// SetLocalEndpoints records where the host that runs this server says it can be reached,
// from its own configuration, when that differs from what is recorded. It is the host
// speaking for itself, at start-up; it is not reachable over HTTP.
func (s *Service) SetLocalEndpoints(ctx context.Context, workspaceID, deviceID string, bootstrap, network []string) (changed bool, err error) {
	boot, err := domain.CleanEndpoints(bootstrap)
	if err != nil {
		return false, err
	}
	nets, err := domain.CleanEndpoints(network)
	if err != nil {
		return false, err
	}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		n, err := tx.DeviceNetwork(ctx, workspaceID, deviceID)
		if err != nil {
			return err
		}
		if equalStrings(n.BootstrapEndpoints, boot) && equalStrings(n.NetworkEndpoints, nets) {
			return nil
		}
		n.BootstrapEndpoints, n.NetworkEndpoints = boot, nets
		n.Reachability, n.LastCheckAt, n.LastExternalOKAt = classifyUnchecked(nets, boot), nil, nil
		n.UpdatedAt = s.stamp()
		changed = true
		return tx.SaveDeviceNetwork(ctx, n)
	})
	if changed {
		s.changed(workspaceID, err)
	}
	return changed, err
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// LocalBootstrapEndpoints returns the bootstrap endpoints recorded for a host.
func (s *Service) LocalBootstrapEndpoints(ctx context.Context, workspaceID, deviceID string) ([]string, error) {
	var out []string
	err := s.db.View(ctx, func(tx store.Tx) error {
		n, err := tx.DeviceNetwork(ctx, workspaceID, deviceID)
		out = n.BootstrapEndpoints
		return err
	})
	return out, err
}

// PeerInfo is another device on the network, as the workspace's registry knows it.
type PeerInfo struct {
	DeviceID    string
	Name        string
	OverlayAddr string
	LastSeen    time.Time
}

// NetworkPeers lists the workspace's unrevoked devices that have an address on the network, other than one.
// It is what a transport's peer list is made from.
func (s *Service) NetworkPeers(ctx context.Context, workspaceID, exceptDeviceID string) ([]PeerInfo, error) {
	var out []PeerInfo
	err := s.db.View(ctx, func(tx store.Tx) error {
		devs, err := tx.Devices(ctx, workspaceID, "")
		if err != nil {
			return err
		}
		nets, err := tx.DeviceNetworks(ctx, workspaceID)
		if err != nil {
			return err
		}
		addr := map[string]string{}
		for _, n := range nets {
			addr[n.DeviceID] = n.OverlayAddr
		}
		for _, d := range devs {
			a, ok := addr[d.ID]
			if d.Revoked() || !ok || d.ID == exceptDeviceID {
				continue
			}
			p := PeerInfo{DeviceID: d.ID, Name: d.Name, OverlayAddr: a}
			if d.LastSeenAt != nil {
				p.LastSeen = *d.LastSeenAt
			}
			out = append(out, p)
		}
		return nil
	})
	return out, err
}
