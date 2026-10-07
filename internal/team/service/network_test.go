package service

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/deviceid/localidentity"
	"devboard/internal/enrollment"
	"devboard/internal/team/domain"
)

// fakeAuthority stands in for the infrastructure that makes certificates, so that
// these tests are about the workspace's rules and not about cryptography (which is
// tested where it is, and end to end in internal/team/server).
type fakeAuthority struct {
	mu      sync.Mutex
	prefix  netip.Prefix
	fp      string
	issued  []CertificateRequest
	n       int
	noSeal  bool
	sealed  map[string][]byte
	invites []enrollment.Invitation
}

func newFake() *fakeAuthority {
	return &fakeAuthority{prefix: netip.MustParsePrefix("10.201.0.0/16"), fp: "aaaa-bbbb-cccc", sealed: map[string][]byte{}}
}

func (f *fakeAuthority) Info() NetworkInfo {
	return NetworkInfo{WorkspaceKey: "KEY", Fingerprint: f.fp, Prefix: f.prefix, CACertificate: "CA-PEM"}
}

func (f *fakeAuthority) SignInvitation(inv enrollment.Invitation) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invites = append(f.invites, inv)
	return "werkbord://join/v1.fake." + inv.ID, nil
}

func (f *fakeAuthority) IssueCertificate(r CertificateRequest, now time.Time) (IssuedCertificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.issued = append(f.issued, r)
	if !strings.Contains(r.NetworkPublicKey, "PUBLIC") {
		return IssuedCertificate{}, errors.New("not a network public key")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", r.DeviceID, f.n)))
	return IssuedCertificate{CertificatePEM: "CERT-" + r.DeviceID, Fingerprint: hex.EncodeToString(sum[:]), NotAfter: now.Add(30 * 24 * time.Hour)}, nil
}

func (f *fakeAuthority) RenderConfig(c domain.NodeConfig) (string, error) {
	return fmt.Sprintf("addr=%s disc=%v relay=%v discoveryHosts=%d relays=%d blocklist=%d", c.OverlayAddr, c.Discovery, c.Relay, len(c.DiscoveryHosts), len(c.RelayAddrs), len(c.Blocklist)), nil
}

func (f *fakeAuthority) SealSecrets(sealingKey, deviceID string) ([]byte, error) {
	if f.noSeal {
		return nil, errors.New("this host does not hold the authority")
	}
	b := []byte("sealed-to:" + sealingKey + ":" + deviceID)
	f.sealed[deviceID] = b
	return b, nil
}

// SealSecretsWithStorage is what a host that holds the data in a cluster seals: the same, and the plan for the new host's node.
func (f *fakeAuthority) SealSecretsWithStorage(sealingKey, deviceID string, p StoragePlan) ([]byte, error) {
	if f.noSeal {
		return nil, errors.New("this host does not hold the authority")
	}
	b := []byte("sealed-to:" + sealingKey + ":" + deviceID + ":storage:" + p.NodeID + ":" + p.RaftAddr + ":join=" + strings.Join(p.JoinRaft, ","))
	f.sealed[deviceID] = b
	return b, nil
}

// ---- helpers ----

// sealKeyFor is a well-formed sealing public key (32 bytes) that stands for the device's: the fake authority does not use it.
func sealKeyFor(deviceID string) string {
	sum := sha256.Sum256([]byte("seal/" + deviceID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const netPubKey = "-----BEGIN NEBULA X25519 PUBLIC KEY-----\nPUBLIC\n-----END NEBULA X25519 PUBLIC KEY-----\n"

type netWorld struct {
	*world
	fake  *fakeAuthority
	owner Actor
	host  *localidentity.Identity
}

// withNetwork starts a workspace on a host that holds a (fake) authority and is its first
// Workspace Host, reachable at a public address.
func withNetwork(t *testing.T) *netWorld {
	t.Helper()
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	f := newFake()
	w.svc.SetNetwork(f)
	host := laptop(t, "ada-server")
	_, err := w.svc.BootstrapNetwork(bg, owner.Workspace.ID, owner.Member.ID, HostBootstrap{Device: host.Public(), Proof: host.ProveRegistration(owner.Workspace.ID, owner.Member.ID),
		NetworkPublicKey: netPubKey, SealingKey: "SEAL", BootstrapEndpoints: []string{"203.0.113.5:7440"}, NetworkEndpoints: []string{"203.0.113.5:4242"}, Connectivity: true})
	if err != nil {
		t.Fatal(err)
	}
	return &netWorld{world: w, fake: f, owner: owner, host: host}
}

// invite makes an invitation as actor.
func (n *netWorld) invite(a Actor, in EnrollInviteInput) EnrollInviteResult {
	n.t.Helper()
	r, err := n.svc.CreateEnrollInvitation(bg, a, in)
	if err != nil {
		n.t.Fatal(err)
	}
	return r
}

// credentialOf is the credential inside an invitation the fake signed: what the person holding the invitation has.
func (n *netWorld) credentialOf(r EnrollInviteResult) string {
	n.t.Helper()
	for _, inv := range n.fake.invites {
		if inv.ID == r.Invitation.ID {
			return inv.Credential
		}
	}
	n.t.Fatal("no such invitation was signed")
	return ""
}

// join is what the shared server hands the authority once it has checked the request's proof.
func (n *netWorld) join(r EnrollInviteResult, dev *localidentity.Identity, member string) (enrollment.Response, error) {
	return n.joinWith(r, n.credentialOf(r), dev, member)
}

func (n *netWorld) joinWith(r EnrollInviteResult, credential string, dev *localidentity.Identity, member string) (enrollment.Response, error) {
	req := enrollment.JoinRequest{InviteID: r.Invitation.ID, Credential: credential, MemberName: member, DeviceID: dev.DeviceID(), DeviceName: dev.Name(),
		DevicePublicKey: base64.RawURLEncoding.EncodeToString(dev.PublicKey()), NetworkPublicKey: netPubKey, SealingPublicKey: sealKeyFor(dev.DeviceID())}
	return n.svc.EnrollAuthority(n.owner.Workspace.ID).Enroll(bg, enrollment.AuthorityJoin{JoinRequest: req, Key: dev.PublicKey(), RemoteAddr: "192.0.2.9:5555"})
}

func (n *netWorld) mustJoin(r EnrollInviteResult, dev *localidentity.Identity, member string) enrollment.Response {
	n.t.Helper()
	resp, err := n.join(r, dev, member)
	if err != nil {
		n.t.Fatal(err)
	}
	if resp.State != enrollment.StateApproved {
		n.t.Fatalf("response = %+v", resp)
	}
	return resp
}

func (n *netWorld) admin(name string) Actor {
	n.t.Helper()
	mt, err := n.svc.AddMember(bg, n.owner, name, "", domain.RoleAdmin)
	if err != nil {
		n.t.Fatal(err)
	}
	return n.signIn(mt.Token)
}

// ---- bootstrapping the first host ----

func TestTheFirstHostIsTheFirstWorkspaceHostAndConnectivityHost(t *testing.T) {
	n := withNetwork(t)
	hostDev, err := n.svc.GetDevice(bg, n.owner, n.host.DeviceID())
	if err != nil {
		t.Fatal(err)
	}
	if !hostDev.Has(domain.CapabilityWorkspaceHost) || !hostDev.Has(domain.CapabilityConnectivityHost) || hostDev.HostStatus != domain.HostActive || hostDev.ConnectivityStatus != domain.HostActive {
		t.Errorf("host = %+v", hostDev)
	}
	st, err := n.svc.NetworkSettings(bg, n.owner)
	if err != nil || st.Fingerprint != n.fake.fp || st.NetworkPrefix != "10.201.0.0/16" || st.EnrollmentApproval != domain.ApprovalAuto {
		t.Fatalf("settings = %+v, %v", st, err)
	}
	cfg, err := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OverlayAddr != "10.201.0.1" || !cfg.Discovery || !cfg.Relay || cfg.ListenPort != 4242 || len(cfg.DiscoveryHosts) != 0 {
		t.Errorf("the first host's configuration = %+v", cfg)
	}
	if got := n.fake.issued[0]; got.DeviceID != n.host.DeviceID() || got.Addr.String() != "10.201.0.1" || len(got.Capabilities) != 2 {
		t.Errorf("the certificate asked for = %+v", got)
	}
	// A second network is refused.
	other := laptop(t, "x")
	_, err = n.svc.BootstrapNetwork(bg, n.owner.Workspace.ID, n.owner.Member.ID, HostBootstrap{Device: other.Public(), Proof: other.ProveRegistration(n.owner.Workspace.ID, n.owner.Member.ID), NetworkPublicKey: netPubKey})
	wantErr(t, err, domain.ErrConflict)
}

func TestAHostThatCannotBeReachedFromOutsideIsNotMadeAConnectivityHost(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	w.svc.SetNetwork(newFake())
	host := laptop(t, "ada-laptop")
	in := HostBootstrap{Device: host.Public(), Proof: host.ProveRegistration(owner.Workspace.ID, owner.Member.ID), NetworkPublicKey: netPubKey, SealingKey: "S",
		BootstrapEndpoints: []string{"192.168.1.10:7440"}, NetworkEndpoints: []string{"192.168.1.10:4242"}, Connectivity: true}
	_, err := w.svc.BootstrapNetwork(bg, owner.Workspace.ID, owner.Member.ID, in)
	wantErr(t, err, domain.ErrInvalid)
	for _, e := range []string{"10.0.0.4:4242", "100.64.3.2:4242", "127.0.0.1:4242", "169.254.1.1:4242", "[fd00::1]:4242"} {
		if ConnectivityPossible([]string{e}) {
			t.Errorf("%s was thought possible to reach from outside", e)
		}
	}
	for _, e := range []string{"203.0.113.5:4242", "team.example.org:4242", "[2001:db8::1]:4242"} {
		if !ConnectivityPossible([]string{e}) {
			t.Errorf("%s was thought impossible to reach from outside", e)
		}
	}
	// As a Workspace Host alone it works, and says it is private.
	in.Connectivity = false
	n, err := w.svc.BootstrapNetwork(bg, owner.Workspace.ID, owner.Member.ID, in)
	if err != nil || n.Reachability != domain.ReachPrivateOnly {
		t.Fatalf("%+v, %v", n, err)
	}
}

func TestOnOneLANTheFirstHostHelpsDevicesFindItAndNothingMore(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	w.svc.SetNetwork(newFake())
	host := laptop(t, "office-server")
	n, err := w.svc.BootstrapNetwork(bg, owner.Workspace.ID, owner.Member.ID, HostBootstrap{Device: host.Public(), Proof: host.ProveRegistration(owner.Workspace.ID, owner.Member.ID),
		NetworkPublicKey: netPubKey, SealingKey: "S", BootstrapEndpoints: []string{"192.168.1.10:7440"}, NetworkEndpoints: []string{"192.168.1.10:4242"}})
	if err != nil {
		t.Fatal(err)
	}
	if !n.Discovery || n.Relay || n.Reachability != domain.ReachPrivateOnly {
		t.Fatalf("host = %+v: a LAN host should help devices find it, relay for nobody, and know it is private", n)
	}
	nw := &netWorld{world: w, fake: w.svc.net.(*fakeAuthority), owner: owner, host: host}
	resp := nw.mustJoin(nw.invite(owner, EnrollInviteInput{}), laptop(t, "desk"), "Dee")
	if len(resp.Network.Discovery) != 1 || resp.Network.Discovery[0].Endpoints[0] != "192.168.1.10:4242" || len(resp.Network.Relays) != 0 {
		t.Errorf("a device on the LAN was told %+v / %v: it must be able to find the host, which is on the LAN", resp.Network.Discovery, resp.Network.Relays)
	}
	h, err := w.svc.NetworkHealth(bg, owner)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(h.Warnings, " | ")
	if h.RemoteAccess != domain.RemoteAccessNotGuaranteed || !strings.Contains(all, "No Connectivity Host") || strings.Contains(all, "No host helps devices find each other") {
		t.Errorf("remote=%s warnings=%s", h.RemoteAccess, all)
	}
}

func TestAWorkspaceHostMayHelpDevicesFindEachOtherButOnlyAConnectivityHostRelays(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	resp := n.mustJoin(r, laptop(t, "second-host"), "")
	a := n.deviceActor(resp.DeviceToken)
	yes := true
	eps := []string{"198.51.100.7:4242"}
	if _, err := n.svc.SetDeviceNetwork(bg, a, a.Device.ID, NetworkPatch{NetworkEndpoints: &eps}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.SetDeviceNetwork(bg, n.owner, a.Device.ID, NetworkPatch{Discovery: &yes}); err != nil {
		t.Errorf("a Workspace Host could not be made a discovery host: %v", err)
	}
	if _, err := n.svc.SetDeviceNetwork(bg, n.owner, a.Device.ID, NetworkPatch{Relay: &yes}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a Workspace Host that is not a Connectivity Host was made a relay: %v", err)
	}
	m := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "member"), "Mia")
	if len(m.Network.Discovery) != 2 || len(m.Network.Relays) != 1 {
		t.Errorf("a new device was told of %d discovery hosts and %d relays, want 2 and 1", len(m.Network.Discovery), len(m.Network.Relays))
	}
}

func TestTheNetworkNeedsAnAuthorityOnThisHost(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	_, err := w.svc.CreateEnrollInvitation(bg, owner, EnrollInviteInput{})
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.NetworkSettings(bg, owner)
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.NetworkHealth(bg, owner)
	wantErr(t, err, domain.ErrConflict)
}

// ---- invitations ----

func TestWhoMayInviteWhom(t *testing.T) {
	n := withNetwork(t)
	admin := n.admin("Ann")
	bo, _ := n.member(n.owner, "Bo")
	cy, _ := n.member(n.owner, "Cy")

	if _, err := n.svc.CreateEnrollInvitation(bg, bo, EnrollInviteInput{}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member invited someone: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, admin, EnrollInviteInput{Role: domain.RoleMember}); err != nil {
		t.Errorf("an admin could not invite a member: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, admin, EnrollInviteInput{Role: domain.RoleAdmin}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("an admin appointed an admin: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, n.owner, EnrollInviteInput{Role: domain.RoleAdmin}); err != nil {
		t.Errorf("the owner could not invite an admin: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, n.owner, EnrollInviteInput{Role: domain.RoleOwner}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an invitation made an owner: %v", err)
	}
	// A member may add a device of their own, and only their own, and not a host.
	if _, err := n.svc.CreateEnrollInvitation(bg, bo, EnrollInviteInput{ForMemberID: bo.Member.ID, Capabilities: []domain.Capability{domain.CapabilityRunner}}); err != nil {
		t.Errorf("a member could not add their own device: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, bo, EnrollInviteInput{ForMemberID: cy.Member.ID}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member added a device to someone else: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, bo, EnrollInviteInput{ForMemberID: bo.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member invited their own device as a Workspace Host: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, admin, EnrollInviteInput{ForMemberID: bo.Member.ID, Capabilities: []domain.Capability{domain.CapabilityConnectivityHost}}); err != nil {
		t.Errorf("an admin could not invite a Connectivity Host: %v", err)
	}
	if _, err := n.svc.CreateEnrollInvitation(bg, n.owner, EnrollInviteInput{ForMemberID: "tmb_nobody"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an invitation for a member who does not exist: %v", err)
	}
	for _, ttl := range []time.Duration{time.Second, 15 * 24 * time.Hour} {
		if _, err := n.svc.CreateEnrollInvitation(bg, n.owner, EnrollInviteInput{TTL: ttl}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("a ttl of %v: %v", ttl, err)
		}
	}
	if _, err := n.svc.ListEnrollInvitations(bg, bo); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member listed invitations: %v", err)
	}
}

func TestAnInvitationNamesTheWorkspacesOwnEndpointsAndKeepsNoCredential(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{Label: "Bo's laptop"})
	signed := n.fake.invites[len(n.fake.invites)-1]
	if len(signed.Endpoints) != 1 || signed.Endpoints[0] != "203.0.113.5:7440" || signed.Role != "member" || signed.Credential == "" || signed.WorkspaceName != "Acme" {
		t.Errorf("signed = %+v", signed)
	}
	if time.Unix(signed.ExpiresAt, 0).Sub(time.Unix(signed.IssuedAt, 0)) != DefaultEnrollTTL {
		t.Errorf("lifetime = %v", time.Unix(signed.ExpiresAt, 0).Sub(time.Unix(signed.IssuedAt, 0)))
	}
	if r.Fingerprint != n.fake.fp || !strings.HasPrefix(r.Link, enrollment.JoinPrefix) {
		t.Errorf("result = %+v", r)
	}
	// The credential is nowhere in what the workspace lists or stores.
	list, _ := n.svc.ListEnrollInvitations(bg, n.owner)
	if len(list) != 1 || list[0].ID != r.Invitation.ID || list[0].State != domain.InvitationOpen {
		t.Fatalf("list = %+v", list)
	}
	if strings.Contains(fmt.Sprintf("%+v", list), signed.Credential) {
		t.Error("the credential is in the listing")
	}
	assertNotInDatabase(t, n.world, signed.Credential)
}

func TestInvitationsCarryEveryKnownEndpointReachableOnesFirst(t *testing.T) {
	n := withNetwork(t)
	// A second Workspace Host, on a LAN, and a third machine reachable from the Internet.
	for _, h := range []struct {
		name string
		boot string
		caps []domain.Capability
		net  []string
	}{
		{"lan-host", "192.168.1.9:7440", []domain.Capability{domain.CapabilityWorkspaceHost}, nil},
		{"edge-host", "198.51.100.2:7440", []domain.Capability{domain.CapabilityConnectivityHost}, []string{"198.51.100.2:4242"}},
	} {
		inv := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: h.caps})
		d := laptop(t, h.name)
		resp := n.mustJoin(inv, d, "")
		a := n.deviceActor(resp.DeviceToken)
		bootstrap := []string{h.boot}
		patch := NetworkPatch{BootstrapEndpoints: &bootstrap}
		if h.net != nil {
			patch.NetworkEndpoints = &h.net
		}
		if _, err := n.svc.SetDeviceNetwork(bg, a, a.Device.ID, patch); err != nil {
			t.Fatal(err)
		}
	}
	n.invite(n.owner, EnrollInviteInput{})
	got := n.fake.invites[len(n.fake.invites)-1].Endpoints
	want := []string{"203.0.113.5:7440", "198.51.100.2:7440", "192.168.1.9:7440"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("endpoints = %v, want %v (publicly addressable first, then the rest)", got, want)
	}
}

func (n *netWorld) deviceActor(token string) Actor {
	n.t.Helper()
	a, err := n.svc.Authenticate(bg, token)
	if err != nil {
		n.t.Fatal(err)
	}
	if a.Device == nil {
		n.t.Fatal("a device credential did not authenticate as a device")
	}
	return a
}

func TestDeviceConnectionInspectionIsLimitedToSelfOrAnAdministrator(t *testing.T) {
	n := withNetwork(t)
	resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "member"), "Mia")
	mia := n.deviceActor(resp.DeviceToken)
	got, err := n.svc.DeviceNetwork(bg, mia, mia.Device.ID)
	if err != nil || got.OverlayAddr != resp.Network.OverlayAddr {
		t.Fatalf("own connection: %+v %v", got, err)
	}
	if _, err := n.svc.DeviceNetwork(bg, mia, n.host.DeviceID()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a member inspected another device: %v", err)
	}
	if _, err := n.svc.DeviceNetwork(bg, n.admin("Bo"), mia.Device.ID); err != nil {
		t.Fatalf("an administrator could not inspect a member's connection: %v", err)
	}
}

func TestAnInvitationsWorkspaceMustHoldTheNetworksKeys(t *testing.T) {
	n := withNetwork(t)
	n.fake.fp = "xxxx-yyyy" // this host's authority is not the one this workspace's network was made with
	_, err := n.svc.CreateEnrollInvitation(bg, n.owner, EnrollInviteInput{})
	wantErr(t, err, domain.ErrConflict)
}

// ---- joining ----

func TestADeviceJoinsAndBecomesAMembersDevice(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{Label: "Bo", Capabilities: []domain.Capability{domain.CapabilityRunner}})
	d := laptop(t, "bo-macbook")
	resp := n.mustJoin(r, d, "Bo")

	if resp.MemberID == "" || resp.DeviceID != d.DeviceID() || resp.Role != "member" || resp.WorkspaceName != "Acme" || resp.WorkspaceKey != "KEY" {
		t.Errorf("response = %+v", resp)
	}
	nb := resp.Network
	if nb == nil || nb.OverlayAddr != "10.201.0.2" || nb.CACertificate != "CA-PEM" || nb.NodeCertificate != "CERT-"+d.DeviceID() || nb.ExpiresAt == 0 {
		t.Fatalf("network = %+v", nb)
	}
	if !strings.Contains(nb.Config, "discoveryHosts=1") || !strings.Contains(nb.Config, "relays=1") { // the first host is both
		t.Errorf("config = %q", nb.Config)
	}
	// What the workspace now knows.
	a := n.deviceActor(resp.DeviceToken)
	if a.Member.Name != "Bo" || a.Member.Role != domain.RoleMember || a.Workspace.ID != n.owner.Workspace.ID || a.Device.ID != d.DeviceID() {
		t.Errorf("actor = %+v", a)
	}
	dev, err := n.svc.GetDevice(bg, n.owner, d.DeviceID())
	if err != nil || !dev.Has(domain.CapabilityRunner) || dev.Has(domain.CapabilityWorkspaceHost) || dev.PublicKey != deviceid.EncodePublicKey(d.PublicKey()) {
		t.Errorf("device = %+v, %v", dev, err)
	}
	if got := n.fake.issued[len(n.fake.issued)-1]; got.Addr.String() != "10.201.0.2" || got.NetworkPublicKey != netPubKey {
		t.Errorf("certificate request = %+v", got)
	}
	// The member has no sign-in token of their own until an administrator issues one.
	if _, err := n.svc.Authenticate(bg, resp.DeviceToken+"x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a wrong token authenticated: %v", err)
	}
	// The device's credential is a hash in the database, not the token.
	assertNotInDatabase(t, n.world, resp.DeviceToken)
	// And it is not an owner's or an admin's: a device reaches only what its member may.
	if _, err := n.svc.CreateEnrollInvitation(bg, a, EnrollInviteInput{}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member's device invited someone: %v", err)
	}
}

func TestADeviceIsAddedToAnExistingMember(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	r := n.invite(bo, EnrollInviteInput{ForMemberID: bo.Member.ID})
	resp := n.mustJoin(r, laptop(t, "bo-phone"), "")
	if resp.MemberID != bo.Member.ID {
		t.Errorf("the device became %s's", resp.MemberID)
	}
	devs, _ := n.svc.ListDevices(bg, bo)
	if len(devs) != 1 {
		t.Errorf("Bo has %d devices", len(devs))
	}
}

func TestEveryDeviceGetsItsOwnAddress(t *testing.T) {
	n := withNetwork(t)
	seen := map[string]bool{"10.201.0.1": true}
	for i := 0; i < 6; i++ {
		resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, fmt.Sprintf("d%d", i)), fmt.Sprintf("Member %d", i))
		if seen[resp.Network.OverlayAddr] {
			t.Fatalf("address %s given twice", resp.Network.OverlayAddr)
		}
		seen[resp.Network.OverlayAddr] = true
		if !netip.MustParsePrefix("10.201.0.0/16").Contains(netip.MustParseAddr(resp.Network.OverlayAddr)) {
			t.Errorf("%s is outside the network", resp.Network.OverlayAddr)
		}
	}
}

func TestAnInvitationWorksOnce(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{})
	n.mustJoin(r, laptop(t, "first"), "Bo")
	for name, dev := range map[string]*localidentity.Identity{"another device": laptop(t, "second"), "a device presenting the same ID": nil} {
		d := dev
		if d == nil {
			d = laptop(t, "again")
		}
		_, err := n.join(r, d, "Cy")
		if !errors.Is(err, enrollment.ErrNotFound) {
			t.Errorf("%s: reuse = %v, want the one refusal", name, err)
		}
	}
	list, _ := n.svc.ListEnrollInvitations(bg, n.owner)
	if list[0].State != domain.InvitationUsed || list[0].UsedAt == nil {
		t.Errorf("invitation = %+v", list[0])
	}
}

func TestEveryWayAnInvitationCanBeUnusableLooksTheSame(t *testing.T) {
	n := withNetwork(t)
	good := func() EnrollInviteResult { return n.invite(n.owner, EnrollInviteInput{}) }

	expired := good()
	n.svc.now = func() time.Time { return time.Now().Add(2 * DefaultEnrollTTL) }
	_, errExpired := n.join(expired, laptop(t, "a"), "A")
	n.svc.now = time.Now

	withdrawn := good()
	if err := n.svc.WithdrawEnrollInvitation(bg, n.owner, withdrawn.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	_, errWithdrawn := n.join(withdrawn, laptop(t, "b"), "B")

	wrongCred := good()
	_, errWrong := n.joinWith(wrongCred, "not-the-credential", laptop(t, "c"), "C")

	_, errUnknown := n.svc.EnrollAuthority(n.owner.Workspace.ID).Enroll(bg, enrollment.AuthorityJoin{JoinRequest: enrollment.JoinRequest{InviteID: enrollment.NewID(), Credential: "x", DeviceID: laptop(t, "d").DeviceID(), DeviceName: "d", NetworkPublicKey: netPubKey}})

	spent := good()
	n.mustJoin(spent, laptop(t, "e"), "E")
	_, errSpent := n.join(spent, laptop(t, "f"), "F")

	for name, err := range map[string]error{"expired": errExpired, "withdrawn": errWithdrawn, "wrong credential": errWrong, "unknown": errUnknown, "spent": errSpent} {
		if !errors.Is(err, enrollment.ErrNotFound) {
			t.Errorf("%s: %v, want enrollment.ErrNotFound (one answer for all of them)", name, err)
		}
	}
	// A refused attempt did not use the invitation up.
	if _, err := n.join(wrongCred, laptop(t, "g"), "G"); err != nil {
		t.Errorf("a wrong guess burned the invitation: %v", err)
	}
}

func TestAnInvitationForAnotherWorkspaceIsNotUsableHere(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{})
	d := laptop(t, "x")
	req := enrollment.JoinRequest{InviteID: r.Invitation.ID, Credential: n.credentialOf(r), MemberName: "X", DeviceID: d.DeviceID(), DeviceName: "x",
		DevicePublicKey: base64.RawURLEncoding.EncodeToString(d.PublicKey()), NetworkPublicKey: netPubKey}
	_, err := n.svc.EnrollAuthority("tws_someoneelse").Enroll(bg, enrollment.AuthorityJoin{JoinRequest: req, Key: d.PublicKey()})
	if !errors.Is(err, enrollment.ErrNotFound) {
		t.Errorf("Enroll through another workspace's authority = %v", err)
	}
}

func TestAChoiceThatCannotBeAcceptedIsReportedAndTheInvitationSurvives(t *testing.T) {
	n := withNetwork(t)
	n.member(n.owner, "Taken")
	r := n.invite(n.owner, EnrollInviteInput{})
	var rej *enrollment.RejectedError
	if _, err := n.join(r, laptop(t, "a"), "Taken"); !errors.As(err, &rej) || !strings.Contains(rej.Reason, "already exists") {
		t.Fatalf("a taken name: %v", err)
	}
	if _, err := n.join(r, laptop(t, "b"), ""); !errors.As(err, &rej) {
		t.Fatalf("no name: %v", err)
	}
	same := laptop(t, "c")
	if _, err := n.join(r, same, "Fresh"); err != nil {
		t.Errorf("the invitation did not survive a rejection: %v", err)
	}
	// A device ID already registered.
	r2 := n.invite(n.owner, EnrollInviteInput{})
	if _, err := n.join(r2, same, "Other"); err == nil {
		t.Error("the same device enrolled twice")
	}
}

func TestAHostMustPresentASealingKey(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	d := laptop(t, "second-host")
	req := enrollment.JoinRequest{InviteID: r.Invitation.ID, Credential: n.credentialOf(r), DeviceID: d.DeviceID(), DeviceName: "h", DevicePublicKey: base64.RawURLEncoding.EncodeToString(d.PublicKey()), NetworkPublicKey: netPubKey}
	var rej *enrollment.RejectedError
	if _, err := n.svc.EnrollAuthority(n.owner.Workspace.ID).Enroll(bg, enrollment.AuthorityJoin{JoinRequest: req, Key: d.PublicKey()}); !errors.As(err, &rej) {
		t.Errorf("a Workspace Host without a sealing key: %v", err)
	}
}

func TestADeviceCanBeLimitedToTheDevicesItMayHold(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	for i := 0; i < domain.MaxDevicesPerMember; i++ {
		n.mustJoin(n.invite(n.owner, EnrollInviteInput{ForMemberID: bo.Member.ID}), laptop(t, fmt.Sprintf("d%d", i)), "")
	}
	var rej *enrollment.RejectedError
	if _, err := n.join(n.invite(n.owner, EnrollInviteInput{ForMemberID: bo.Member.ID}), laptop(t, "one-too-many"), ""); !errors.As(err, &rej) {
		t.Errorf("a %dth device: %v", domain.MaxDevicesPerMember+1, err)
	}
}

// ---- approval ----

func TestAWorkspaceThatRequiresApprovalHoldsDevicesUntilAnAdministratorDecides(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	if err := n.svc.SetEnrollmentApproval(bg, bo, domain.ApprovalAdmin); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a member changed the policy: %v", err)
	}
	if err := n.svc.SetEnrollmentApproval(bg, n.owner, domain.ApprovalAdmin); err != nil {
		t.Fatal(err)
	}
	r := n.invite(n.owner, EnrollInviteInput{})
	if !r.Invitation.RequireApproval {
		t.Error("the invitation does not require approval under a workspace that does")
	}
	d := laptop(t, "bo-laptop")
	resp, err := n.join(r, d, "Bea")
	if err != nil || resp.State != enrollment.StatePending || resp.EnrollmentID == "" || resp.DeviceToken != "" || resp.Network != nil {
		t.Fatalf("response = %+v, %v: a pending device must be given nothing", resp, err)
	}
	// Nothing exists yet: no member, no device, no address.
	if _, err := n.svc.GetDevice(bg, n.owner, d.DeviceID()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a pending device is already registered: %v", err)
	}
	auth := n.svc.EnrollAuthority(n.owner.Workspace.ID)
	poll := func(device string) (enrollment.Response, error) {
		return auth.Poll(bg, enrollment.AuthorityPoll{PollRequest: enrollment.PollRequest{EnrollmentID: resp.EnrollmentID, DeviceID: device}})
	}
	if p, err := poll(d.DeviceID()); err != nil || p.State != enrollment.StatePending {
		t.Errorf("poll while pending = %+v, %v", p, err)
	}
	if _, err := poll(laptop(t, "someone-else").DeviceID()); !errors.Is(err, enrollment.ErrNotFound) {
		t.Errorf("another device polled this enrollment: %v", err)
	}
	// The administrators see it, with a fingerprint to compare.
	pending, err := n.svc.ListEnrollments(bg, n.owner, domain.EnrollmentPending)
	if err != nil || len(pending) != 1 || pending[0].DeviceName != "bo-laptop" || pending[0].Fingerprint != deviceid.Fingerprint(d.PublicKey()) || pending[0].MemberName != "Bea" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if _, err := n.svc.ListEnrollments(bg, bo, domain.EnrollmentPending); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member listed requests to join: %v", err)
	}
	if _, err := n.svc.ApproveEnrollment(bg, bo, resp.EnrollmentID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member approved a device: %v", err)
	}
	if _, err := n.svc.ApproveEnrollment(bg, n.owner, resp.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.ApproveEnrollment(bg, n.owner, resp.EnrollmentID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("approving twice: %v", err)
	}
	// The device collects what it was issued, once.
	got, err := poll(d.DeviceID())
	if err != nil || got.State != enrollment.StateApproved || got.DeviceToken == "" || got.Network == nil || got.Network.OverlayAddr == "" {
		t.Fatalf("poll after approval = %+v, %v", got, err)
	}
	if _, err := poll(d.DeviceID()); !errors.Is(err, enrollment.ErrNotFound) {
		t.Errorf("collected twice: %v", err)
	}
	if a := n.deviceActor(got.DeviceToken); a.Member.Name != "Bea" {
		t.Errorf("actor = %+v", a.Member)
	}
	assertNotInDatabase(t, n.world, got.DeviceToken)
}

func TestADeniedDeviceGetsNothingAndTheInvitationIsSpent(t *testing.T) {
	n := withNetwork(t)
	n.svc.SetEnrollmentApproval(bg, n.owner, domain.ApprovalAdmin)
	r := n.invite(n.owner, EnrollInviteInput{})
	d := laptop(t, "stranger")
	resp, _ := n.join(r, d, "Mallory")
	if _, err := n.svc.DenyEnrollment(bg, n.owner, resp.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	p, err := n.svc.EnrollAuthority(n.owner.Workspace.ID).Poll(bg, enrollment.AuthorityPoll{PollRequest: enrollment.PollRequest{EnrollmentID: resp.EnrollmentID, DeviceID: d.DeviceID()}})
	if err != nil || p.State != enrollment.StateDenied || p.DeviceToken != "" || p.Network != nil {
		t.Errorf("poll after denial = %+v, %v", p, err)
	}
	if _, err := n.join(r, laptop(t, "retry"), "Mallory"); !errors.Is(err, enrollment.ErrNotFound) {
		t.Errorf("a denied invitation was used again: %v", err)
	}
	if _, err := n.svc.ApproveEnrollment(bg, n.owner, resp.EnrollmentID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a denied request was approved afterwards: %v", err)
	}
	if _, err := n.svc.GetDevice(bg, n.owner, d.DeviceID()); !errors.Is(err, domain.ErrNotFound) {
		t.Error("a denied device was registered")
	}
}

func TestApprovingSomethingTheApproverMayNotGrantIsRefused(t *testing.T) {
	n := withNetwork(t)
	n.svc.SetEnrollmentApproval(bg, n.owner, domain.ApprovalAdmin)
	// An invitation for an admin, made by the owner, cannot be approved by an admin.
	admin := n.admin("Ann")
	r := n.invite(n.owner, EnrollInviteInput{Role: domain.RoleAdmin})
	resp, _ := n.join(r, laptop(t, "new-admin"), "Newadmin")
	if _, err := n.svc.ApproveEnrollment(bg, admin, resp.EnrollmentID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("an admin approved an admin: %v", err)
	}
	if _, err := n.svc.ApproveEnrollment(bg, n.owner, resp.EnrollmentID); err != nil {
		t.Errorf("the owner could not: %v", err)
	}
}

func TestApprovalCatchesAChoiceThatBecameUnacceptable(t *testing.T) {
	n := withNetwork(t)
	n.svc.SetEnrollmentApproval(bg, n.owner, domain.ApprovalAdmin)
	r := n.invite(n.owner, EnrollInviteInput{})
	resp, _ := n.join(r, laptop(t, "x"), "Sam")
	n.member(n.owner, "Sam") // someone else took the name while it waited
	if _, err := n.svc.ApproveEnrollment(bg, n.owner, resp.EnrollmentID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("approving a name that is now taken: %v", err)
	}
}

// ---- revocation: two layers ----

func TestRevokingADeviceEndsItInBothLayersAtOnce(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: bo.Member.ID, Capabilities: []domain.Capability{domain.CapabilityRunner}})
	d := laptop(t, "bo-laptop")
	resp := n.mustJoin(r, d, "")
	a := n.deviceActor(resp.DeviceToken)
	fingerprint := n.fake.issued[len(n.fake.issued)-1]
	_ = fingerprint

	// Before: it authenticates, and the network does not refuse it.
	before, _ := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID())
	if len(before.Blocklist) != 0 {
		t.Fatalf("blocklist before = %v", before.Blocklist)
	}

	// The device's owner revokes it (a lost laptop).
	if _, err := n.svc.RevokeDevice(bg, bo, d.DeviceID()); err != nil {
		t.Fatal(err)
	}

	// Application layer: authoritative, immediate. Its credential no longer authenticates, on
	// the very next request, whatever the network still lets through.
	if _, err := n.svc.Authenticate(bg, resp.DeviceToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a revoked device's credential authenticated: %v", err)
	}
	if _, err := n.svc.RenewCertificate(bg, a); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("a revoked device renewed its certificate with a credential it held before: %v", err)
	}
	if _, err := n.svc.DeviceNetworkConfig(bg, a); err == nil {
		t.Error("a revoked device fetched the network configuration")
	}
	dir := n.svc.DeviceDirectory()
	if ed, err := dir.Device(bg, n.owner.Workspace.ID, d.DeviceID()); err != nil || !ed.Revoked {
		t.Errorf("the registry does not say it is revoked: %+v %v", ed, err)
	}

	// Network layer: defence in depth. Its certificate is on the blocklist every host gives its
	// network program, so the network stops accepting it too.
	after, err := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID())
	if err != nil || len(after.Blocklist) != 1 {
		t.Fatalf("blocklist after = %v, %v", after.Blocklist, err)
	}
	if _, err := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, d.DeviceID()); err == nil {
		t.Error("a revoked device was given a node configuration")
	}
	// Revoking again changes nothing.
	if _, err := n.svc.RevokeDevice(bg, n.owner, d.DeviceID()); err != nil {
		t.Errorf("revoking a revoked device: %v", err)
	}
	if again, _ := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID()); len(again.Blocklist) != 1 {
		t.Errorf("revoking twice changed the blocklist: %v", again.Blocklist)
	}
}

func TestEveryCertificateAMemberHadIsRefusedWhenTheyAreRemoved(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	var tokens []string
	for i := 0; i < 2; i++ {
		resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{ForMemberID: bo.Member.ID}), laptop(t, fmt.Sprintf("bo-%d", i)), "")
		tokens = append(tokens, resp.DeviceToken)
		// A renewal gives a second certificate: both must be refused.
		if _, err := n.svc.RenewCertificate(bg, n.deviceActor(resp.DeviceToken)); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.svc.RemoveMember(bg, n.owner, bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if _, err := n.svc.Authenticate(bg, tok); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("a removed member's device authenticated: %v", err)
		}
	}
	cfg, err := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID())
	if err != nil || len(cfg.Blocklist) != 4 {
		t.Errorf("blocklist = %d entries, want 4 (two devices, two certificates each, kept after the registry rows were deleted): %v %v", len(cfg.Blocklist), cfg.Blocklist, err)
	}
}

func TestBlocklistEntriesGoWhenTheCertificateWouldHaveExpiredAnyway(t *testing.T) {
	n := withNetwork(t)
	resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "x"), "Xavier")
	if _, err := n.svc.RevokeDevice(bg, n.owner, resp.DeviceID); err != nil {
		t.Fatal(err)
	}
	n.svc.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	cfg, _ := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, n.host.DeviceID())
	if len(cfg.Blocklist) != 0 {
		t.Errorf("an expired certificate is still on the blocklist: %v", cfg.Blocklist)
	}
}

func TestRenewalKeepsTheDevicesKeyAndAddress(t *testing.T) {
	n := withNetwork(t)
	resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "x"), "Xavier")
	a := n.deviceActor(resp.DeviceToken)
	b, err := n.svc.RenewCertificate(bg, a)
	if err != nil {
		t.Fatal(err)
	}
	if b.OverlayAddr != resp.Network.OverlayAddr || b.Certificate == "" || b.CACertificate != "CA-PEM" {
		t.Errorf("renewal = %+v", b)
	}
	if last := n.fake.issued[len(n.fake.issued)-1]; last.NetworkPublicKey != netPubKey || last.Addr.String() != resp.Network.OverlayAddr {
		t.Errorf("renewal asked for %+v", last)
	}
	// A member's own token is not a device's credential.
	if _, err := n.svc.RenewCertificate(bg, n.owner); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member token renewed a device's certificate: %v", err)
	}
}

// ---- configuration: several discovery hosts, relays ----

func TestEveryDeviceIsToldOfEveryDiscoveryHostAndRelay(t *testing.T) {
	n := withNetwork(t)
	// A second Connectivity Host, reachable.
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityConnectivityHost}})
	resp := n.mustJoin(r, laptop(t, "edge"), "")
	edge := n.deviceActor(resp.DeviceToken)
	eps := []string{"198.51.100.2:4242"}
	yes := true
	if _, err := n.svc.SetDeviceNetwork(bg, edge, edge.Device.ID, NetworkPatch{NetworkEndpoints: &eps}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.SetDeviceNetwork(bg, n.owner, edge.Device.ID, NetworkPatch{Discovery: &yes, Relay: &yes}); err != nil {
		t.Fatal(err)
	}

	// A member's device now learns of both.
	m := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "member"), "Mia")
	if len(m.Network.Discovery) != 2 || len(m.Network.Relays) != 2 {
		t.Fatalf("the new device was told of %d discovery hosts and %d relays, want 2 and 2: %+v", len(m.Network.Discovery), len(m.Network.Relays), m.Network)
	}
	got := map[string]bool{}
	for _, p := range m.Network.Discovery {
		got[p.OverlayAddr] = len(p.Endpoints) > 0
	}
	if !got["10.201.0.1"] || !got["10.201.0.2"] {
		t.Errorf("discovery hosts = %v", got)
	}
	// A host does not list itself.
	cfg, _ := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, edge.Device.ID)
	if len(cfg.DiscoveryHosts) != 1 || cfg.DiscoveryHosts[0].Addr != "10.201.0.1" || cfg.ListenPort != 4242 || len(cfg.RelayAddrs) != 1 {
		t.Errorf("edge's configuration = %+v", cfg)
	}
	// Neither is "authoritative": revoking the first leaves the second.
	if _, err := n.svc.RevokeDevice(bg, n.owner, n.host.DeviceID()); err != nil {
		t.Fatal(err)
	}
	cfg2, err := n.svc.NodeConfigOf(bg, n.owner.Workspace.ID, edge.Device.ID)
	if err != nil || len(cfg2.DiscoveryHosts) != 0 || len(cfg2.RelayAddrs) != 0 {
		t.Errorf("a revoked host is still listed: %+v %v", cfg2, err)
	}
	mia := n.deviceActor(m.DeviceToken)
	b, err := n.svc.DeviceNetworkConfig(bg, mia)
	if err != nil || len(b.Discovery) != 1 || b.Discovery[0].OverlayAddr != "10.201.0.2" {
		t.Errorf("a connected device's view after one host went: %+v %v", b.Discovery, err)
	}
}

func TestOnlyAConnectivityHostCanBeDiscoveryOrRelay(t *testing.T) {
	n := withNetwork(t)
	resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "plain"), "Pat")
	yes := true
	_, err := n.svc.SetDeviceNetwork(bg, n.owner, resp.DeviceID, NetworkPatch{Discovery: &yes})
	wantErr(t, err, domain.ErrInvalid)
	// A device may say where it is, but not make itself a discovery host.
	a := n.deviceActor(resp.DeviceToken)
	_, err = n.svc.SetDeviceNetwork(bg, a, a.Device.ID, NetworkPatch{Discovery: &yes})
	wantErr(t, err, domain.ErrForbidden)
	_, err = n.svc.SetDeviceNetwork(bg, a, n.host.DeviceID(), NetworkPatch{NetworkEndpoints: &[]string{"6.6.6.6:4242"}})
	wantErr(t, err, domain.ErrForbidden)
	bad := []string{"not an endpoint"}
	_, err = n.svc.SetDeviceNetwork(bg, a, a.Device.ID, NetworkPatch{NetworkEndpoints: &bad})
	wantErr(t, err, domain.ErrInvalid)
}

// ---- reachability and health ----

func TestReachabilityIsOnlyEverAsStrongAsWhatWasChecked(t *testing.T) {
	n := withNetwork(t)
	hostID := n.host.DeviceID()
	// From the host itself: it proves the address answers, not that anyone else can reach it.
	self := Actor{Member: n.owner.Member, Workspace: n.owner.Workspace, Device: &domain.Device{ID: hostID}}
	if err := n.svc.RecordReachability(bg, self, hostID, ReachReport{Endpoint: "203.0.113.5:7440", OK: true}); err != nil {
		t.Fatal(err)
	}
	h, _ := n.svc.NetworkHealth(bg, n.owner)
	if h.Hosts[0].LastExternalOKAt != nil || h.RemoteAccess != domain.RemoteAccessNotGuaranteed {
		t.Errorf("a host's own check counted as an external one: %+v", h.Hosts[0])
	}
	// From another device of the workspace.
	resp := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "elsewhere"), "Eve")
	other := n.deviceActor(resp.DeviceToken)
	if err := n.svc.RecordReachability(bg, other, hostID, ReachReport{Endpoint: "203.0.113.5:7440", OK: true}); err != nil {
		t.Fatal(err)
	}
	h, _ = n.svc.NetworkHealth(bg, n.owner)
	if h.Hosts[0].LastExternalOKAt == nil || h.Hosts[0].Reachability != domain.ReachPublic || h.RemoteAccess != domain.RemoteAccessAvailable {
		t.Errorf("a check from another device did not count: %+v / %s", h.Hosts[0], h.RemoteAccess)
	}
	// It stops answering.
	if err := n.svc.RecordReachability(bg, other, hostID, ReachReport{Endpoint: "203.0.113.5:7440", OK: false}); err != nil {
		t.Fatal(err)
	}
	h, _ = n.svc.NetworkHealth(bg, n.owner)
	if h.Hosts[0].Reachability != domain.ReachUnreachable {
		t.Errorf("reachability = %s", h.Hosts[0].Reachability)
	}
	// An address the host does not advertise cannot be reported on; a member cannot report.
	if err := n.svc.RecordReachability(bg, other, hostID, ReachReport{Endpoint: "6.6.6.6:1", OK: true}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a report on an unknown address: %v", err)
	}
	bo, _ := n.member(n.owner, "Bo")
	if err := n.svc.RecordReachability(bg, bo, hostID, ReachReport{Endpoint: "203.0.113.5:7440", OK: true}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member (not a device) reported: %v", err)
	}
	// A private address can never be public, whatever answers.
	private := []string{"192.168.1.5:4242"}
	n.svc.SetDeviceNetwork(bg, n.owner, hostID, NetworkPatch{NetworkEndpoints: &private, BootstrapEndpoints: &private})
	n.svc.RecordReachability(bg, other, hostID, ReachReport{Endpoint: "192.168.1.5:4242", OK: true})
	h, _ = n.svc.NetworkHealth(bg, n.owner)
	if h.Hosts[0].Reachability != domain.ReachPrivateOnly || h.Hosts[0].LastExternalOKAt != nil {
		t.Errorf("a private address was counted as reachable from outside: %+v", h.Hosts[0])
	}
}

func TestTheHealthReportDoesNotPretendNATTraversalIsGuaranteed(t *testing.T) {
	now := time.Now()
	host := func(name string, caps domain.Capability, reach domain.Reachability, ok bool, disc, relay bool) domain.HostNetworkStatus {
		h := domain.HostNetworkStatus{DeviceID: name, Name: name, Reachability: reach, Discovery: disc, Relay: relay}
		if caps == domain.CapabilityWorkspaceHost {
			h.WorkspaceHost = true
		} else {
			h.ConnectivityHost = true
		}
		if ok {
			h.LastExternalOKAt = &now
		}
		return h
	}
	cases := []struct {
		name       string
		hosts      []domain.HostNetworkStatus
		remote     string
		mustWarn   []string
		mustNotSay []string
	}{
		{"no Connectivity Host at all", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachPrivateOnly, false, false, false)},
			domain.RemoteAccessNotGuaranteed, []string{"No Connectivity Host", "cannot be guaranteed"}, []string{"available"}},
		{"a Connectivity Host nobody has reached", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachUnknown, false, false, false), host("c", domain.CapabilityConnectivityHost, domain.ReachUnknown, false, true, true)},
			domain.RemoteAccessNotGuaranteed, []string{"confirmed reachable", "cannot be guaranteed", "carrier-grade NAT"}, nil},
		{"a Connectivity Host that did not answer", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachUnknown, false, false, false), host("c", domain.CapabilityConnectivityHost, domain.ReachUnreachable, false, true, true)},
			domain.RemoteAccessNotGuaranteed, []string{"cannot be guaranteed"}, nil},
		{"one reachable Connectivity Host", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachUnknown, false, false, false), host("c", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, true)},
			domain.RemoteAccessAvailable, []string{"Only one Connectivity Host", "Only one host helps"}, nil},
		{"two reachable Connectivity Hosts", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachUnknown, false, false, false), host("c", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, true), host("d", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, true)},
			domain.RemoteAccessAvailable, nil, []string{"Only one", "cannot be guaranteed", "No Connectivity"}},
		{"reachable but not relaying", []domain.HostNetworkStatus{host("a", domain.CapabilityWorkspaceHost, domain.ReachUnknown, false, false, false), host("c", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, false), host("d", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, false)},
			domain.RemoteAccessAvailable, []string{"relays traffic"}, nil},
		{"no Workspace Host", []domain.HostNetworkStatus{host("c", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, true), host("d", domain.CapabilityConnectivityHost, domain.ReachPublic, true, true, true)},
			domain.RemoteAccessAvailable, []string{"No active Workspace Host"}, nil},
	}
	for _, c := range cases {
		remote, warnings := assess(c.hosts)
		all := strings.Join(warnings, " | ")
		if remote != c.remote {
			t.Errorf("%s: remote access = %s, want %s", c.name, remote, c.remote)
		}
		for _, w := range c.mustWarn {
			if !strings.Contains(all, w) {
				t.Errorf("%s: the warnings lack %q:\n%s", c.name, w, all)
			}
		}
		for _, w := range c.mustNotSay {
			if strings.Contains(all, w) {
				t.Errorf("%s: the warnings say %q:\n%s", c.name, w, all)
			}
		}
	}
}

func TestOnlyThoseWhoMayViewDevicesSeeTheNetworksHealth(t *testing.T) {
	n := withNetwork(t)
	bo, _ := n.member(n.owner, "Bo")
	if _, err := n.svc.NetworkHealth(bg, bo); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member saw the health report: %v", err)
	}
	h, err := n.svc.NetworkHealth(bg, n.owner)
	if err != nil || len(h.Hosts) != 1 || h.Hosts[0].DeviceID != n.host.DeviceID() || h.Settings.Fingerprint != n.fake.fp {
		t.Errorf("health = %+v, %v", h, err)
	}
	if len(h.Warnings) == 0 {
		t.Error("a network with one host and no confirmed external reachability has no warnings")
	}
	if len(h.Hosts[0].Endpoints) != 2 || h.Hosts[0].Endpoints[0].Kind != domain.AddrPublic {
		t.Errorf("endpoints = %+v", h.Hosts[0].Endpoints)
	}
}

// ---- handing the authority on ----

func TestTheAuthorityIsSealedToTheHostItIsForAndCollectedOnce(t *testing.T) {
	n := withNetwork(t)
	admin := n.admin("Ann")
	r := n.invite(admin, EnrollInviteInput{ForMemberID: admin.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	d := laptop(t, "second-host")
	resp := n.mustJoin(r, d, "")
	a := n.deviceActor(resp.DeviceToken)
	// Until it is handed over, nothing is waiting, and the device is only joining.
	if _, err := n.svc.CollectProvision(bg, a); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("collected before being provisioned: %v", err)
	}
	dev, _ := n.svc.GetDevice(bg, admin, d.DeviceID())
	if dev.HostStatus != domain.HostJoining {
		t.Errorf("host status = %s", dev.HostStatus)
	}
	// Only someone who manages devices provisions.
	bo, _ := n.member(n.owner, "Bo")
	if err := n.svc.ProvisionHost(bg, bo, d.DeviceID()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member provisioned a host: %v", err)
	}
	if err := n.svc.ProvisionHost(bg, admin, d.DeviceID()); err != nil {
		t.Fatal(err)
	}
	blob, err := n.svc.CollectProvision(bg, a)
	if err != nil || !strings.Contains(string(blob), "sealed-to:"+sealKeyFor(d.DeviceID())) {
		t.Fatalf("collect = %q, %v", blob, err)
	}
	// Another device, even another host's, gets nothing: the blob is addressed by device, never by request.
	if _, err := n.svc.CollectProvision(bg, n.deviceActor(n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "snoop"), "Snoop").DeviceToken)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another device collected: %v", err)
	}
	if _, err := n.svc.CollectProvision(bg, n.owner); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a member token collected: %v", err)
	}
	if err := n.svc.AcknowledgeProvision(bg, a); err != nil {
		t.Fatal(err)
	}
	if _, err := n.svc.CollectProvision(bg, a); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("collected twice: %v", err)
	}
	dev, _ = n.svc.GetDevice(bg, admin, d.DeviceID())
	if dev.HostStatus != domain.HostActive {
		t.Errorf("host status after acknowledging = %s", dev.HostStatus)
	}
}

func TestOnlyAHostWithTheAuthorityCanHandItOn(t *testing.T) {
	n := withNetwork(t)
	r := n.invite(n.owner, EnrollInviteInput{ForMemberID: n.owner.Member.ID, Capabilities: []domain.Capability{domain.CapabilityWorkspaceHost}})
	resp := n.mustJoin(r, laptop(t, "h2"), "")
	n.fake.noSeal = true
	if err := n.svc.ProvisionHost(bg, n.owner, resp.DeviceID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a host without the authority provisioned: %v", err)
	}
	// And a device that is not a Workspace Host cannot be given it.
	n.fake.noSeal = false
	plain := n.mustJoin(n.invite(n.owner, EnrollInviteInput{}), laptop(t, "plain"), "Pat")
	if err := n.svc.ProvisionHost(bg, n.owner, plain.DeviceID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a plain device was given the authority: %v", err)
	}
	// Revoking a device before it collects removes what was waiting.
	n.svc.ProvisionHost(bg, n.owner, resp.DeviceID)
	if _, err := n.svc.RevokeDevice(bg, n.owner, resp.DeviceID); err != nil {
		t.Fatal(err)
	}
	if err := n.svc.ProvisionHost(bg, n.owner, resp.DeviceID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a revoked device was provisioned: %v", err)
	}
}

// ---- the schema holds nothing secret ----

func assertNotInDatabase(t *testing.T, w *world, secret string) {
	t.Helper()
	rows := dumpDatabase(t, w)
	if strings.Contains(rows, secret) {
		t.Errorf("a secret (%.8s…) is stored in the database in clear", secret)
	}
}
