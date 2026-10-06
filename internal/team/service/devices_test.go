package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/deviceid/localidentity"
	"devboard/internal/envelope"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// laptop makes a device identity, as the device would on its own computer.
func laptop(t *testing.T, name string) *localidentity.Identity {
	t.Helper()
	i, err := localidentity.New(name, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// register registers a device as actor, with a proof the device makes.
func (w *world) register(a Actor, i *localidentity.Identity, caps ...domain.Capability) (domain.Device, error) {
	return w.svc.RegisterDevice(bg, a, DeviceInput{Device: i.Public(), Proof: i.ProveRegistration(a.Workspace.ID, a.Member.ID), Capabilities: caps})
}

func (w *world) mustRegister(a Actor, i *localidentity.Identity, caps ...domain.Capability) domain.Device {
	w.t.Helper()
	d, err := w.register(a, i, caps...)
	if err != nil {
		w.t.Fatal(err)
	}
	return d
}

func TestADeviceIsRegisteredWithOnlyPublicFacts(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	i := laptop(t, "Bo's MacBook")
	d := w.mustRegister(bo, i, domain.CapabilityRunner)

	if d.ID != string(i.ID()) || d.MemberID != bo.Member.ID || d.WorkspaceID != bo.Workspace.ID || d.Name != "Bo's MacBook" || d.PublicKey != deviceid.EncodePublicKey(i.PublicKey()) {
		t.Fatalf("%+v", d)
	}
	if !d.Has(domain.CapabilityRunner) || d.Has(domain.CapabilityWorkspaceHost) || d.HostStatus != domain.HostNone || d.ConnectivityStatus != domain.HostNone || d.Revoked() {
		t.Fatalf("%+v", d)
	}
	got, err := w.svc.GetDevice(bg, bo, d.ID)
	if err != nil || got.ID != d.ID || len(got.Capabilities) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationNeedsProofOfTheKey(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	cy, _ := w.member(owner, "Cy")
	mine, theirs := laptop(t, "mine"), laptop(t, "theirs")

	// Registering someone else's public key, with a proof signed by a key one does hold.
	_, err := w.svc.RegisterDevice(bg, bo, DeviceInput{Device: theirs.Public(), Proof: mine.ProveRegistration(bo.Workspace.ID, bo.Member.ID)})
	wantErr(t, err, domain.ErrInvalid)
	// No proof.
	_, err = w.svc.RegisterDevice(bg, bo, DeviceInput{Device: mine.Public()})
	wantErr(t, err, domain.ErrInvalid)
	// A proof made for another member cannot be replayed to enrol the device under this one.
	_, err = w.svc.RegisterDevice(bg, cy, DeviceInput{Device: mine.Public(), Proof: mine.ProveRegistration(bo.Workspace.ID, bo.Member.ID)})
	wantErr(t, err, domain.ErrInvalid)
	// A malformed record.
	bad := mine.Public()
	bad.PublicKey = "nope"
	_, err = w.svc.RegisterDevice(bg, bo, DeviceInput{Device: bad, Proof: mine.ProveRegistration(bo.Workspace.ID, bo.Member.ID)})
	wantErr(t, err, domain.ErrInvalid)

	// The same device or key cannot be registered twice, by anyone.
	w.mustRegister(bo, mine)
	_, err = w.register(bo, mine)
	wantErr(t, err, domain.ErrConflict)
	_, err = w.register(cy, mine)
	wantErr(t, err, domain.ErrConflict)
}

// What a device may do for the workspace is a fact about the device, decided by
// who may manage devices; it has nothing to do with its owner's role.
func TestCapabilitiesAreIndependentOfRoles(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admin := w.admin(owner, "Ann")
	member, _ := w.member(owner, "Bo")

	// A plain member owns a Workspace Host and a Connectivity Host (the admin put them there)...
	host := w.mustRegister(member, laptop(t, "bo-nas"))
	host, err := w.svc.SetDeviceCapabilities(bg, admin, host.ID, []domain.Capability{domain.CapabilityWorkspaceHost, domain.CapabilityConnectivityHost})
	if err != nil {
		t.Fatalf("an admin could not make a member's device a host: %v", err)
	}
	if !host.Has(domain.CapabilityWorkspaceHost) || !host.Has(domain.CapabilityConnectivityHost) || host.HostStatus != domain.HostJoining || host.ConnectivityStatus != domain.HostJoining {
		t.Fatalf("%+v", host)
	}
	// ...which does not make the member an admin, or give them any permission they lacked.
	again := w.signIn(mustTokenFor(t, w, owner, member))
	if again.Member.Role != domain.RoleMember {
		t.Fatalf("owning a host changed a role to %s", again.Member.Role)
	}
	for _, p := range []domain.Permission{domain.PermMembersManage, domain.PermProjectsCreate, domain.PermDevicesManage, domain.PermDevicesViewAll} {
		if again.Member.Can(p) {
			t.Errorf("the owner of a host can %s", p)
		}
	}
	if _, err := w.svc.CreateProject(bg, again, ProjectInput{Name: "x"}); err == nil {
		t.Fatal("the owner of a host created a project")
	}

	// And an admin with no devices at all is no host, while the admin's own runner is just a runner.
	if got, _ := w.svc.DevicesWithCapability(bg, admin, domain.CapabilityWorkspaceHost); len(got) != 1 || got[0].ID != host.ID {
		t.Fatalf("hosts = %+v", got)
	}
	adminRunner := w.mustRegister(admin, laptop(t, "ann-mac"), domain.CapabilityRunner)
	if adminRunner.Has(domain.CapabilityWorkspaceHost) || adminRunner.Has(domain.CapabilityConnectivityHost) {
		t.Fatal("an admin's device is a host because its owner is an admin")
	}
	ownerDev := w.mustRegister(owner, laptop(t, "ada-mac"))
	if len(ownerDev.Capabilities) != 0 {
		t.Fatalf("the owner's device has capabilities it was not given: %+v", ownerDev)
	}
	if got, _ := w.svc.DevicesWithCapability(bg, admin, domain.CapabilityWorkspaceHost); len(got) != 1 {
		t.Fatalf("hosts = %+v", got)
	}
	if got, _ := w.svc.DevicesWithCapability(bg, admin, domain.CapabilityRunner); len(got) != 1 || got[0].ID != adminRunner.ID {
		t.Fatalf("runners = %+v", got)
	}
}

func mustTokenFor(t *testing.T, w *world, by Actor, m Actor) string {
	t.Helper()
	mt, err := w.svc.ReissueToken(bg, by, m.Member.ID)
	if err != nil {
		t.Fatal(err)
	}
	return mt.Token
}

func TestOnlyDeviceManagersGrantInfrastructureCapabilities(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	member, _ := w.member(owner, "Bo")

	// A member may not register their own device as a host...
	for _, c := range []domain.Capability{domain.CapabilityWorkspaceHost, domain.CapabilityConnectivityHost} {
		_, err := w.register(member, laptop(t, "x"), c)
		wantErr(t, err, domain.ErrForbidden)
	}
	// ...but may declare it a runner, and may withdraw that.
	d := w.mustRegister(member, laptop(t, "bo-mac"))
	d, err := w.svc.SetDeviceCapabilities(bg, member, d.ID, []domain.Capability{domain.CapabilityRunner})
	if err != nil || !d.Has(domain.CapabilityRunner) {
		t.Fatalf("%+v %v", d, err)
	}
	// They may not promote it to a host later, either.
	_, err = w.svc.SetDeviceCapabilities(bg, member, d.ID, []domain.Capability{domain.CapabilityRunner, domain.CapabilityWorkspaceHost})
	wantErr(t, err, domain.ErrForbidden)
	d, err = w.svc.SetDeviceCapabilities(bg, member, d.ID, nil)
	if err != nil || len(d.Capabilities) != 0 {
		t.Fatalf("%+v %v", d, err)
	}

	// Once granted by the owner, the member cannot take it away from themselves
	// (a host leaving is a decision for whoever runs the workspace).
	d, err = w.svc.SetDeviceCapabilities(bg, owner, d.ID, []domain.Capability{domain.CapabilityConnectivityHost})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.svc.SetDeviceCapabilities(bg, member, d.ID, nil)
	wantErr(t, err, domain.ErrForbidden)
	// Taking it away resets the role.
	d, err = w.svc.SetDeviceCapabilities(bg, owner, d.ID, []domain.Capability{domain.CapabilityRunner})
	if err != nil || d.ConnectivityStatus != domain.HostNone || d.Has(domain.CapabilityConnectivityHost) {
		t.Fatalf("%+v %v", d, err)
	}
	// Unknown capabilities are refused.
	_, err = w.svc.SetDeviceCapabilities(bg, owner, d.ID, []domain.Capability{"root"})
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.register(member, laptop(t, "y"), "root")
	wantErr(t, err, domain.ErrInvalid)
}

func TestHostStatusFollowsTheRoleItBelongsTo(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	member, _ := w.member(owner, "Bo")
	d := w.mustRegister(member, laptop(t, "nas"))
	d, _ = w.svc.SetDeviceCapabilities(bg, owner, d.ID, []domain.Capability{domain.CapabilityWorkspaceHost})

	d, err := w.svc.SetDeviceRoleStatus(bg, owner, d.ID, domain.CapabilityWorkspaceHost, domain.HostActive)
	if err != nil || d.HostStatus != domain.HostActive {
		t.Fatalf("%+v %v", d, err)
	}
	// A role the device does not hold has no status.
	_, err = w.svc.SetDeviceRoleStatus(bg, owner, d.ID, domain.CapabilityConnectivityHost, domain.HostActive)
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.SetDeviceRoleStatus(bg, owner, d.ID, domain.CapabilityRunner, domain.HostActive)
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.SetDeviceRoleStatus(bg, owner, d.ID, domain.CapabilityWorkspaceHost, "great")
	wantErr(t, err, domain.ErrInvalid)
	// A member cannot report a host's status as they please.
	_, err = w.svc.SetDeviceRoleStatus(bg, member, d.ID, domain.CapabilityWorkspaceHost, domain.HostActive)
	wantErr(t, err, domain.ErrForbidden)
	// Keeping the role keeps its status across other changes.
	d, _ = w.svc.SetDeviceCapabilities(bg, owner, d.ID, []domain.Capability{domain.CapabilityWorkspaceHost, domain.CapabilityRunner})
	if d.HostStatus != domain.HostActive {
		t.Fatalf("status lost: %+v", d)
	}
}

func TestWhoSeesWhichDevices(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admin := w.admin(owner, "Ann")
	bo, _ := w.member(owner, "Bo")
	cy, _ := w.member(owner, "Cy")
	boDev := w.mustRegister(bo, laptop(t, "bo"))
	cyDev := w.mustRegister(cy, laptop(t, "cy"))

	if list, err := w.svc.ListDevices(bg, bo); err != nil || len(list) != 1 || list[0].ID != boDev.ID {
		t.Fatalf("a member sees %+v, %v", list, err)
	}
	if list, _ := w.svc.ListDevices(bg, admin); len(list) != 2 {
		t.Fatalf("an admin sees %d", len(list))
	}
	if list, _ := w.svc.ListDevices(bg, owner); len(list) != 2 {
		t.Fatalf("the owner sees %d", len(list))
	}
	// Another member's device is not found, as another member's project is not.
	_, err := w.svc.GetDevice(bg, bo, cyDev.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = w.svc.RevokeDevice(bg, bo, cyDev.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = w.svc.RenameDevice(bg, bo, cyDev.ID, "mine now")
	wantErr(t, err, domain.ErrNotFound)
	// Only managers list by capability.
	_, err = w.svc.DevicesWithCapability(bg, bo, domain.CapabilityRunner)
	wantErr(t, err, domain.ErrForbidden)
	if _, err := w.svc.GetDevice(bg, bo, "dev_aaaaaaaaaaaaaaaa"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

// Another workspace's devices are invisible, and its members cannot act on ours.
func TestDevicesAreScopedToTheirWorkspace(t *testing.T) {
	w := newWorld(t)
	a, _ := w.workspace("Acme", "Ada")
	z, _ := w.workspace("Zed", "Zoe")
	d := w.mustRegister(a, laptop(t, "ada"))
	_, err := w.svc.GetDevice(bg, z, d.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = w.svc.RevokeDevice(bg, z, d.ID)
	wantErr(t, err, domain.ErrNotFound)
	if list, _ := w.svc.ListDevices(bg, z); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
	// The same key may be registered in another workspace (it is a different workspace's device record)...
	// ...but never the same device ID: an ID names one device.
	i := laptop(t, "shared")
	w.mustRegister(a, i)
	if _, err := w.register(z, i); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("one device ID in two workspaces: %v", err)
	}
}

func TestRevokingADevice(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admin := w.admin(owner, "Ann")
	bo, _ := w.member(owner, "Bo")
	dev := w.mustRegister(bo, laptop(t, "bo"), domain.CapabilityRunner)
	host := w.mustRegister(bo, laptop(t, "nas"))
	host, _ = w.svc.SetDeviceCapabilities(bg, owner, host.ID, []domain.Capability{domain.CapabilityWorkspaceHost})
	host, _ = w.svc.SetDeviceRoleStatus(bg, owner, host.ID, domain.CapabilityWorkspaceHost, domain.HostActive)

	// An owner revokes their own lost laptop.
	got, err := w.svc.RevokeDevice(bg, bo, dev.ID)
	if err != nil || !got.Revoked() || got.Has(domain.CapabilityRunner) {
		t.Fatalf("%+v %v", got, err)
	}
	// An admin revokes a member's host, which stops being one at once.
	got, err = w.svc.RevokeDevice(bg, admin, host.ID)
	if err != nil || !got.Revoked() || got.HostStatus != domain.HostNone || got.Has(domain.CapabilityWorkspaceHost) {
		t.Fatalf("%+v %v", got, err)
	}
	if hosts, _ := w.svc.DevicesWithCapability(bg, owner, domain.CapabilityWorkspaceHost); len(hosts) != 0 {
		t.Fatalf("a revoked device is still a host: %+v", hosts)
	}
	// It stays in the registry as history, listed as revoked.
	if list, _ := w.svc.ListDevices(bg, bo); len(list) != 2 || !list[0].Revoked() {
		t.Fatalf("%+v", list)
	}
	// Revoking again is not an error and does not move the time.
	again, err := w.svc.RevokeDevice(bg, bo, dev.ID)
	if err != nil || !again.RevokedAt.Equal(*got0(t, w, bo, dev.ID).RevokedAt) {
		t.Fatalf("%+v %v", again, err)
	}
	// Revocation is permanent: nothing changes a revoked device.
	_, err = w.svc.SetDeviceCapabilities(bg, owner, dev.ID, []domain.Capability{domain.CapabilityRunner})
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.RenameDevice(bg, owner, dev.ID, "again")
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.SetDeviceRoleStatus(bg, owner, host.ID, domain.CapabilityWorkspaceHost, domain.HostActive)
	wantErr(t, err, domain.ErrConflict)
	if d := got0(t, w, bo, dev.ID); !d.Revoked() || d.Name != "bo" {
		t.Fatalf("%+v", d)
	}
	// A revoked device is never online, and the key stays unavailable for another registration.
	if err := w.svc.RecordDeviceSeen(bg, bo, dev.ID); err != nil {
		t.Fatal(err)
	}
	if d := got0(t, w, bo, dev.ID); d.OnlineAt(time.Now()) {
		t.Fatal("a revoked device is online")
	}
}

func got0(t *testing.T, w *world, a Actor, id string) domain.Device {
	t.Helper()
	d, err := w.svc.GetDevice(bg, a, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLastSeenAndOnline(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	d := w.mustRegister(bo, laptop(t, "bo"))
	if d.LastSeenAt != nil || d.OnlineAt(time.Now()) {
		t.Fatal("a device never seen is online")
	}
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	w.svc.now = func() time.Time { return clock }
	if err := w.svc.RecordDeviceSeen(bg, bo, d.ID); err != nil {
		t.Fatal(err)
	}
	d = got0(t, w, bo, d.ID)
	if d.LastSeenAt == nil || !d.LastSeenAt.Equal(clock) || !d.OnlineAt(clock.Add(domain.DeviceOnlineWindow)) || d.OnlineAt(clock.Add(domain.DeviceOnlineWindow+time.Second)) {
		t.Fatalf("%+v", d)
	}
	// The time only moves forward: a stale report cannot make a device look older.
	w.svc.now = func() time.Time { return clock.Add(-time.Hour) }
	if err := w.svc.RecordDeviceSeen(bg, bo, d.ID); err != nil {
		t.Fatal(err)
	}
	if d = got0(t, w, bo, d.ID); !d.LastSeenAt.Equal(clock) {
		t.Fatalf("last seen moved back to %v", d.LastSeenAt)
	}
	// Someone else cannot report for a device.
	cy, _ := w.member(owner, "Cy")
	wantErr(t, w.svc.RecordDeviceSeen(bg, cy, d.ID), domain.ErrNotFound)
}

func TestAMemberHasABoundedNumberOfDevices(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	var first domain.Device
	for i := 0; i < domain.MaxDevicesPerMember; i++ {
		d := w.mustRegister(bo, laptop(t, "d"))
		if i == 0 {
			first = d
		}
	}
	_, err := w.register(bo, laptop(t, "one too many"))
	wantErr(t, err, domain.ErrConflict)
	// Revoking one makes room.
	if _, err := w.svc.RevokeDevice(bg, bo, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.register(bo, laptop(t, "fits now")); err != nil {
		t.Fatal(err)
	}
}

func TestRemovingAMemberRemovesTheirDevices(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	i := laptop(t, "bo")
	d := w.mustRegister(bo, i)
	if err := w.svc.RemoveMember(bg, owner, bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := w.svc.ListDevices(bg, owner); len(list) != 0 {
		t.Fatalf("a removed member's device remains: %+v", list)
	}
	// So nothing it signs verifies any more.
	_, err := w.svc.DeviceDirectory().Device(bg, owner.Workspace.ID, d.ID)
	if !errors.Is(err, envelope.ErrUnknownKey) {
		t.Fatalf("%v", err)
	}
}

// The registry feeds the envelope verifier: a device registered here can sign
// envelopes that verify, one revoked here cannot, and nothing outside the
// workspace can.
func TestTheRegistryIsWhatVerifiesEnvelopes(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	cy, _ := w.member(owner, "Cy")
	boID, runnerID := laptop(t, "bo-laptop"), laptop(t, "bo-runner")
	w.mustRegister(bo, boID)
	w.mustRegister(bo, runnerID, domain.CapabilityRunner)
	cyID := laptop(t, "cy")
	w.mustRegister(cy, cyID)

	verifier, err := envelope.NewVerifier(w.svc.DeviceDirectory(), &envelope.MemoryReplayCache{}, string(runnerID.ID()))
	if err != nil {
		t.Fatal(err)
	}
	req := func() envelope.Request {
		return envelope.Request{WorkspaceID: bo.Workspace.ID, UserID: bo.Member.ID, TargetDeviceID: string(runnerID.ID()), Action: envelope.ActionFetchRunnerStatus, Payload: envelope.FetchRunnerStatus{}}
	}
	e, err := envelope.Sign(boID, req(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(bg, e); err != nil {
		t.Fatalf("a registered device's envelope: %v", err)
	}
	// Cy's device signing as Bo.
	forged, _ := envelope.Sign(cyID, req(), time.Now())
	if _, err := verifier.Verify(bg, forged); !errors.Is(err, envelope.ErrWrongOwner) {
		t.Fatalf("Cy signing as Bo: %v", err)
	}
	// Revoked devices stop verifying immediately.
	if _, err := w.svc.RevokeDevice(bg, bo, string(boID.ID())); err != nil {
		t.Fatal(err)
	}
	e2, _ := envelope.Sign(boID, req(), time.Now())
	if _, err := verifier.Verify(bg, e2); !errors.Is(err, envelope.ErrRevoked) {
		t.Fatalf("after revocation: %v", err)
	}
	// A device from another workspace is not known here.
	z, _ := w.workspace("Zed", "Zoe")
	zd := laptop(t, "zoe")
	w.mustRegister(z, zd)
	zreq := req()
	zreq.UserID = z.Member.ID
	e3, _ := envelope.Sign(zd, zreq, time.Now())
	if _, err := verifier.Verify(bg, e3); !errors.Is(err, envelope.ErrUnknownKey) {
		t.Fatalf("another workspace's device claiming ours: %v", err)
	}
}

// Everything Team returns about a device is public: no field a private key,
// credential, path or environment could be in.
func TestADeviceRecordHasNoRoomForASecret(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	d := w.mustRegister(owner, laptop(t, "ada"), domain.CapabilityRunner)
	raw := jsonOf(t, d)
	var keys []string
	collectKeys(raw, &keys)
	allowed := map[string]bool{"publicKey": true}
	for _, key := range keys {
		low := strings.ToLower(key)
		for _, banned := range []string{"token", "secret", "password", "credential", "apikey", "api_key", "env", "environ", "path", "dir", "shell", "command", "cwd", "home", "hash", "ssh", "private", "seed", "key"} {
			if strings.Contains(low, banned) && !allowed[key] {
				t.Errorf("a device exposes a field named %q", key)
			}
		}
	}
	for _, secret := range []string{"BEGIN PRIVATE", "wbt_", "ghp_", "sk-"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("a device record contains %q", secret)
		}
	}
}

// The service is written against store.Store and store.Tx, not SQL: it runs,
// unchanged, on a storage that is only those interfaces. This one wraps the real
// one and counts what the service asks of it.
type countingStore struct {
	store.Store
	mu      sync.Mutex
	views   int
	updates int
	queries int
}

type countingTx struct {
	store.Tx
	s *countingStore
}

func (c *countingStore) View(ctx context.Context, fn func(store.Tx) error) error {
	c.mu.Lock()
	c.views++
	c.mu.Unlock()
	return c.Store.View(ctx, func(tx store.Tx) error { return fn(countingTx{tx, c}) })
}

func (c *countingStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	c.mu.Lock()
	c.updates++
	c.mu.Unlock()
	return c.Store.Update(ctx, func(tx store.Tx) error { return fn(countingTx{tx, c}) })
}

func (c countingTx) Device(ctx context.Context, ws, id string) (domain.Device, error) {
	c.s.mu.Lock()
	c.s.queries++
	c.s.mu.Unlock()
	return c.Tx.Device(ctx, ws, id)
}

func TestTheServiceOnlyNeedsTheStoreInterfaces(t *testing.T) {
	db, _ := newDB(t)
	cs := &countingStore{Store: db}
	var _ store.Store = cs
	w := &world{t: t, svc: New(cs), db: cs}

	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	p := w.project(owner, "Shop")
	if err := w.svc.AddProjectMember(bg, owner, p.ID, bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	k, err := w.svc.CreateTicket(bg, owner, p.ID, TicketInput{Title: "A", Description: "d", Requirements: "r", Status: domain.TicketAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.ClaimTicket(bg, bo, p.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	d := w.mustRegister(bo, laptop(t, "bo"))
	if _, err := w.svc.GetDevice(bg, bo, d.ID); err != nil {
		t.Fatal(err)
	}
	if cs.updates == 0 || cs.views == 0 || cs.queries == 0 {
		t.Fatalf("the service did not go through the store: %d updates, %d views, %d device queries", cs.updates, cs.views, cs.queries)
	}
	board, err := w.svc.Board(bg, bo, p.ID)
	if err != nil || board.Project.ID != p.ID {
		t.Fatalf("%v", err)
	}
}

func jsonOf(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
