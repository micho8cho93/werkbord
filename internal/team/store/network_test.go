package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/domain"
)

func seedNetwork(t *testing.T, db *DB) (ada, bo string) {
	t.Helper()
	ctx := context.Background()
	ada, bo = seedWorkspace(t, db, "tws_1")
	err := db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertNetworkSettings(ctx, domain.NetworkSettings{WorkspaceID: "tws_1", WorkspaceKey: "K", Fingerprint: "fp", NetworkPrefix: "10.200.0.0/16", CACertificate: "CA", EnrollmentApproval: domain.ApprovalAuto, CreatedAt: t0, UpdatedAt: t0}); err != nil {
			return err
		}
		return tx.InsertDevice(ctx, device("tws_1", bo, devA, 1, domain.CapabilityRunner))
	})
	if err != nil {
		t.Fatal(err)
	}
	return ada, bo
}

func devNet(id, addr string) domain.DeviceNetwork {
	return domain.DeviceNetwork{DeviceID: id, WorkspaceID: "tws_1", OverlayAddr: addr, NetworkPublicKey: "PUB", Groups: []string{"member"}, BootstrapEndpoints: []string{}, NetworkEndpoints: []string{}, Reachability: domain.ReachUnknown, CreatedAt: t0, UpdatedAt: t0}
}

func TestTheNetworkTablesHaveNoColumnForASecret(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	cols := map[string][]string{}
	tables := []string{"network_settings", "device_network", "network_certificates", "device_credentials", "enrollment_invitations", "enrollments"}
	err := db.pool.View(ctx, func(tx *sql.Tx) error {
		for _, table := range tables {
			rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
			if err != nil {
				return err
			}
			for rows.Next() {
				var n string
				_ = rows.Scan(&n)
				cols[table] = append(cols[table], n)
			}
			rows.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"network_settings":       "workspace_id workspace_key fingerprint network_prefix ca_certificate enrollment_approval created_at updated_at",
		"device_network":         "device_id workspace_id overlay_addr network_public_key sealing_key groups discovery relay bootstrap_endpoints network_endpoints reachability last_check_at last_external_ok_at provision_sealed created_at updated_at",
		"network_certificates":   "fingerprint workspace_id device_id issued_at not_after revoked_at",
		"device_credentials":     "device_id workspace_id token_hash created_at",
		"enrollment_invitations": "id workspace_id credential_hash created_by label for_member_id role capabilities require_approval state created_at expires_at used_at",
		"enrollments":            "id workspace_id invitation_id member_id member_name device_id device_name device_key network_public_key sealing_key capabilities state delivered_at decided_by decided_at remote_addr created_at",
	}
	for table, w := range want {
		if got := strings.Join(cols[table], " "); got != w {
			t.Errorf("%s columns = %s\nwant %s", table, got, w)
		}
	}
	// Whatever is called a key, a credential or sealed is public, a hash or ciphertext; none is a private key,
	// a token, a seed or a passphrase. Adding a column means saying here why it is none of those.
	isNotASecret := map[string]string{
		"workspace_key": "the workspace's PUBLIC key", "network_public_key": "a device's PUBLIC network key", "device_key": "a device's PUBLIC application key",
		"sealing_key": "a device's PUBLIC sealing key", "ca_certificate": "the authority's certificate: public",
		"credential_hash": "a hash", "token_hash": "a hash", "provision_sealed": "ciphertext, openable only by the device it is sealed to",
	}
	for table, cs := range cols {
		for _, c := range cs {
			for _, banned := range []string{"private", "secret", "token", "password", "passphrase", "seed", "credential", "key", "sealed", "ca_key", "signing", "pem"} {
				if strings.Contains(c, banned) {
					if _, ok := isNotASecret[c]; !ok {
						t.Errorf("%s.%s could hold %s: say why it cannot", table, c, banned)
					}
				}
			}
		}
	}
	// The columns that hold ciphertext or a hash are the only ones that could be mistaken for a secret, and each is typed to say what it is.
	for _, c := range []string{"credential_hash", "token_hash"} {
		if _, ok := isNotASecret[c]; !ok {
			t.Errorf("%s", c)
		}
	}
}

func TestAnAddressOnTheNetworkBelongsToOneDevice(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedNetwork(t, db)
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, device("tws_1", bo, devB, 2)) }); err != nil {
		t.Fatal(err)
	}
	err := db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertDeviceNetwork(ctx, devNet(devA, "10.200.0.1")); err != nil {
			return err
		}
		return tx.InsertDeviceNetwork(ctx, devNet(devB, "10.200.0.1"))
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("two devices at one address: %v", err)
	}
	// The failed transaction was rolled back whole.
	if err := db.View(ctx, func(tx Tx) error {
		l, err := tx.DeviceNetworks(ctx, "tws_1")
		if len(l) != 0 {
			t.Errorf("%d devices were left on the network", len(l))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnInvitationIsUsedOnceAndNeverAfterItExpiresOrIsWithdrawn(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	ada, _ := seedNetwork(t, db)
	mk := func(id string, ttl time.Duration) {
		t.Helper()
		err := db.Update(ctx, func(tx Tx) error {
			return tx.InsertEnrollInvitation(ctx, domain.EnrollInvitation{ID: id, WorkspaceID: "tws_1", Role: domain.RoleMember, Capabilities: []domain.Capability{domain.CapabilityRunner},
				CreatedBy: ada, CreatedAt: t0, ExpiresAt: t0.Add(ttl), State: domain.InvitationOpen}, []byte("hash-"+id))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("winv_one", time.Hour)
	mk("winv_old", time.Minute)
	mk("winv_gone", time.Hour)
	use := func(id string, at time.Time) bool {
		var ok bool
		if err := db.Update(ctx, func(tx Tx) (err error) { ok, err = tx.UseEnrollInvitation(ctx, "tws_1", id, at); return }); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !use("winv_one", t0.Add(time.Second)) || use("winv_one", t0.Add(2*time.Second)) {
		t.Error("an invitation was not usable exactly once")
	}
	if use("winv_old", t0.Add(2*time.Minute)) {
		t.Error("an expired invitation was used")
	}
	if err := db.Update(ctx, func(tx Tx) error { _, err := tx.WithdrawEnrollInvitation(ctx, "tws_1", "winv_gone"); return err }); err != nil {
		t.Fatal(err)
	}
	if use("winv_gone", t0.Add(time.Second)) {
		t.Error("a withdrawn invitation was used")
	}
	// The credential's hash comes back for the check, and is a hash.
	if err := db.View(ctx, func(tx Tx) error {
		inv, hash, err := tx.EnrollInvitationByID(ctx, "winv_one")
		if err != nil || string(hash) != "hash-winv_one" || inv.State != domain.InvitationUsed || inv.UsedAt == nil || len(inv.Capabilities) != 1 {
			t.Errorf("invitation = %+v %q %v", inv, hash, err)
		}
		if _, _, err := tx.EnrollInvitationByID(ctx, "winv_nothing"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("an invitation that does not exist: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnInvitationHasOneEnrollmentAndItIsDecidedAndCollectedOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	ada, _ := seedNetwork(t, db)
	_ = db.Update(ctx, func(tx Tx) error {
		return tx.InsertEnrollInvitation(ctx, domain.EnrollInvitation{ID: "winv_a", WorkspaceID: "tws_1", Role: domain.RoleMember, CreatedBy: ada, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), State: domain.InvitationOpen}, []byte("h"))
	})
	e := domain.Enrollment{ID: "tenr_1", WorkspaceID: "tws_1", InvitationID: "winv_a", DeviceID: devC, DeviceName: "c", DeviceKey: "K", NetworkPublicKey: "P", State: domain.EnrollmentPending, CreatedAt: t0, Capabilities: []domain.Capability{domain.CapabilityRunner}}
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertEnrollment(ctx, e) }); err != nil {
		t.Fatal(err)
	}
	e2 := e
	e2.ID = "tenr_2"
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertEnrollment(ctx, e2) }); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a second enrollment for one invitation: %v", err)
	}
	dec := func(state domain.EnrollmentState) bool {
		var ok bool
		_ = db.Update(ctx, func(tx Tx) (err error) {
			ok, err = tx.DecideEnrollment(ctx, "tws_1", "tenr_1", state, "", ada, t0.Add(time.Minute))
			return
		})
		return ok
	}
	if dec(domain.EnrollmentApproved) == false || dec(domain.EnrollmentDenied) {
		t.Error("an enrollment was not decided exactly once")
	}
	deliver := func() bool {
		var ok bool
		_ = db.Update(ctx, func(tx Tx) (err error) { ok, err = tx.MarkEnrollmentDelivered(ctx, "tws_1", "tenr_1", t0); return })
		return ok
	}
	if !deliver() || deliver() {
		t.Error("an enrollment was not collected exactly once")
	}
	// A denied one can never be delivered.
	_ = db.Update(ctx, func(tx Tx) error {
		_ = tx.InsertEnrollInvitation(ctx, domain.EnrollInvitation{ID: "winv_b", WorkspaceID: "tws_1", Role: domain.RoleMember, CreatedBy: ada, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), State: domain.InvitationOpen}, []byte("h"))
		e3 := e
		e3.ID, e3.InvitationID, e3.DeviceID = "tenr_3", "winv_b", devB
		return tx.InsertEnrollment(ctx, e3)
	})
	_ = db.Update(ctx, func(tx Tx) error {
		_, _ = tx.DecideEnrollment(ctx, "tws_1", "tenr_3", domain.EnrollmentDenied, "", ada, t0)
		ok, _ := tx.MarkEnrollmentDelivered(ctx, "tws_1", "tenr_3", t0)
		if ok {
			t.Error("a denied enrollment was delivered")
		}
		return nil
	})
}

func TestRevokedCertificatesStayOnTheBlocklistWhenTheirDeviceIsDeletedAndLeaveItWhenTheyExpire(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedNetwork(t, db)
	err := db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertDeviceNetwork(ctx, devNet(devA, "10.200.0.2")); err != nil {
			return err
		}
		for i, fp := range []string{"fp-1", "fp-2"} {
			if err := tx.InsertCertificate(ctx, "tws_1", domain.NetworkCertificate{Fingerprint: fp, DeviceID: devA, IssuedAt: t0, NotAfter: t0.Add(time.Duration(i+1) * 24 * time.Hour)}); err != nil {
				return err
			}
		}
		// Recording the same certificate again is the same certificate.
		if err := tx.InsertCertificate(ctx, "tws_1", domain.NetworkCertificate{Fingerprint: "fp-1", DeviceID: devA, IssuedAt: t0, NotAfter: t0.Add(24 * time.Hour)}); err != nil {
			return err
		}
		n, err := tx.RevokeCertificates(ctx, "tws_1", devA, t0.Add(time.Hour))
		if n != 2 {
			t.Errorf("revoked %d certificates", n)
		}
		if again, _ := tx.RevokeCertificates(ctx, "tws_1", devA, t0.Add(2*time.Hour)); again != 0 {
			t.Errorf("revoking twice revoked %d more", again)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := func(at time.Time) []string {
		var l []string
		_ = db.View(ctx, func(tx Tx) (err error) { l, err = tx.Blocklist(ctx, "tws_1", at); return })
		return l
	}
	if l := blocked(t0.Add(2 * time.Hour)); len(l) != 2 {
		t.Fatalf("blocklist = %v", l)
	}
	// The device and its member are deleted: the refusal stays.
	if err := db.Update(ctx, func(tx Tx) error { return tx.DeleteMember(ctx, "tws_1", bo) }); err != nil {
		t.Fatal(err)
	}
	if l := blocked(t0.Add(2 * time.Hour)); len(l) != 2 {
		t.Errorf("the blocklist lost its entries when the device was deleted: %v", l)
	}
	// An expired certificate is refused anyway, and leaves the list.
	if l := blocked(t0.Add(36 * time.Hour)); len(l) != 1 || l[0] != "fp-2" {
		t.Errorf("blocklist after the first expired = %v", l)
	}
	if l := blocked(t0.Add(72 * time.Hour)); len(l) != 0 {
		t.Errorf("blocklist after both expired = %v", l)
	}
}

func TestADeviceCredentialIdentifiesOneDeviceAndGoesWhenDeleted(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedNetwork(t, db)
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, device("tws_1", bo, devB, 2)) }); err != nil {
		t.Fatal(err)
	}
	set := func(dev, hash string) error {
		return db.Update(ctx, func(tx Tx) error { return tx.SetDeviceCredential(ctx, "tws_1", dev, hash, t0) })
	}
	if err := set(devA, "h1"); err != nil {
		t.Fatal(err)
	}
	if err := set(devB, "h1"); err == nil {
		t.Error("two devices share a credential")
	}
	if err := set(devA, "h2"); err != nil { // reissued
		t.Fatal(err)
	}
	find := func(h string) (domain.Device, error) {
		var d domain.Device
		err := db.View(ctx, func(tx Tx) (err error) { d, err = tx.DeviceByCredentialHash(ctx, h); return })
		return d, err
	}
	if d, err := find("h2"); err != nil || d.ID != devA {
		t.Errorf("find h2 = %+v %v", d, err)
	}
	if _, err := find("h1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the replaced credential still works: %v", err)
	}
	if err := db.Update(ctx, func(tx Tx) error { return tx.DeleteDeviceCredential(ctx, "tws_1", devA) }); err != nil {
		t.Fatal(err)
	}
	if _, err := find("h2"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a deleted credential still works: %v", err)
	}
}

func TestSealedSecretsAreKeptForOneDeviceAndRemovedOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	seedNetwork(t, db)
	if err := db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertDeviceNetwork(ctx, devNet(devA, "10.200.0.3")); err != nil {
			return err
		}
		return tx.SetProvision(ctx, "tws_1", devA, []byte{1, 2, 3}, t0)
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.View(ctx, func(tx Tx) error {
		b, err := tx.Provision(ctx, "tws_1", devA)
		if err != nil || len(b) != 3 {
			t.Errorf("provision = %v %v", b, err)
		}
		n, _ := tx.DeviceNetwork(ctx, "tws_1", devA)
		if !n.ProvisionPending {
			t.Error("a device with secrets waiting does not say so")
		}
		return nil
	})
	_ = db.Update(ctx, func(tx Tx) error { return tx.ClearProvision(ctx, "tws_1", devA, t0) })
	_ = db.View(ctx, func(tx Tx) error {
		if b, _ := tx.Provision(ctx, "tws_1", devA); len(b) != 0 {
			t.Error("collected secrets are still stored")
		}
		return nil
	})
}
