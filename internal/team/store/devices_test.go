package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/team/domain"
)

var t0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// workspace creates a workspace with two members, and returns their IDs.
func seedWorkspace(t *testing.T, db *DB, ws string) (ada, bo string) {
	t.Helper()
	ctx := context.Background()
	ada, bo = "tmb_ada_"+ws, "tmb_bo_"+ws
	err := db.Update(ctx, func(tx Tx) error {
		if err := tx.InsertWorkspace(ctx, domain.Workspace{ID: ws, Name: ws, CreatedAt: t0}); err != nil {
			return err
		}
		if err := tx.InsertMember(ctx, domain.Member{ID: ada, WorkspaceID: ws, Name: "Ada", Role: domain.RoleOwner, CreatedAt: t0}, "h_"+ada); err != nil {
			return err
		}
		return tx.InsertMember(ctx, domain.Member{ID: bo, WorkspaceID: ws, Name: "Bo", Role: domain.RoleMember, CreatedAt: t0}, "h_"+bo)
	})
	if err != nil {
		t.Fatal(err)
	}
	return ada, bo
}

func openDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func device(ws, member, id string, keyByte byte, caps ...domain.Capability) domain.Device {
	key := make([]byte, 32)
	key[0] = keyByte
	d := domain.Device{ID: id, WorkspaceID: ws, MemberID: member, Name: "dev " + id, PublicKey: deviceid.EncodePublicKey(key),
		Capabilities: caps, HostStatus: domain.HostNone, ConnectivityStatus: domain.HostNone, CreatedAt: t0, UpdatedAt: t0}
	if d.Capabilities == nil {
		d.Capabilities = []domain.Capability{}
	}
	return d
}

const (
	devA = "dev_aaaaaaaaaaaaaaaa"
	devB = "dev_bbbbbbbbbbbbbbbb"
	devC = "dev_cccccccccccccccc"
)

func TestDevicesRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedWorkspace(t, db, "tws_1")
	d := device("tws_1", bo, devA, 1, domain.CapabilityWorkspaceHost, domain.CapabilityRunner)
	d.HostStatus = domain.HostJoining
	seen := t0.Add(time.Minute)
	d.LastSeenAt = &seen

	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, d) }); err != nil {
		t.Fatal(err)
	}
	var got domain.Device
	if err := db.View(ctx, func(tx Tx) (err error) { got, err = tx.Device(ctx, "tws_1", devA); return }); err != nil {
		t.Fatal(err)
	}
	if got.ID != devA || got.MemberID != bo || got.PublicKey != d.PublicKey || got.HostStatus != domain.HostJoining || got.ConnectivityStatus != domain.HostNone ||
		!got.CreatedAt.Equal(t0) || got.LastSeenAt == nil || !got.LastSeenAt.Equal(seen) || got.Revoked() {
		t.Fatalf("%+v", got)
	}
	// Capabilities come back sorted, every time.
	if len(got.Capabilities) != 2 || got.Capabilities[0] != domain.CapabilityRunner || got.Capabilities[1] != domain.CapabilityWorkspaceHost {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
}

func TestADeviceOrKeyIsRegisteredOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedWorkspace(t, db, "tws_1")
	seedWorkspace(t, db, "tws_2")
	insert := func(d domain.Device) error {
		return db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, d) })
	}
	if err := insert(device("tws_1", bo, devA, 1)); err != nil {
		t.Fatal(err)
	}
	if err := insert(device("tws_1", bo, devA, 2)); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("the same ID twice: %v", err)
	}
	if err := insert(device("tws_1", bo, devB, 1)); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("the same key twice in a workspace: %v", err)
	}
	// Another workspace may not take the ID, but its own key is its own business.
	if err := insert(device("tws_2", "tmb_bo_tws_2", devA, 3)); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("an ID taken in another workspace: %v", err)
	}
	if err := insert(device("tws_2", "tmb_bo_tws_2", devC, 1)); err != nil {
		t.Errorf("the same key in another workspace: %v", err)
	}
	// A device cannot belong to a member of another workspace: the foreign key says so.
	if err := insert(device("tws_1", "tmb_bo_tws_2", devB, 9)); err == nil {
		t.Error("a device was registered for another workspace's member")
	}
}

func TestDevicesAreScopedAndListed(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	ada, bo := seedWorkspace(t, db, "tws_1")
	_, bo2 := seedWorkspace(t, db, "tws_2")
	for _, d := range []domain.Device{
		device("tws_1", ada, devA, 1, domain.CapabilityRunner),
		device("tws_1", bo, devB, 2, domain.CapabilityWorkspaceHost, domain.CapabilityConnectivityHost),
		device("tws_2", bo2, devC, 3, domain.CapabilityWorkspaceHost),
	} {
		d.CreatedAt = d.CreatedAt.Add(time.Duration(d.PublicKey[0]) * time.Second)
		if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, d) }); err != nil {
			t.Fatal(err)
		}
	}
	err := db.View(ctx, func(tx Tx) error {
		all, _ := tx.Devices(ctx, "tws_1", "")
		if len(all) != 2 {
			t.Errorf("all = %d", len(all))
		}
		mine, _ := tx.Devices(ctx, "tws_1", bo)
		if len(mine) != 1 || mine[0].ID != devB || len(mine[0].Capabilities) != 2 {
			t.Errorf("bo's = %+v", mine)
		}
		none, err := tx.Devices(ctx, "tws_9", "")
		if err != nil || none == nil || len(none) != 0 {
			t.Errorf("an empty workspace: %#v %v", none, err)
		}
		hosts, _ := tx.DevicesWithCapability(ctx, "tws_1", domain.CapabilityWorkspaceHost)
		if len(hosts) != 1 || hosts[0].ID != devB {
			t.Errorf("hosts = %+v", hosts)
		}
		conn, _ := tx.DevicesWithCapability(ctx, "tws_1", domain.CapabilityConnectivityHost)
		runners, _ := tx.DevicesWithCapability(ctx, "tws_1", domain.CapabilityRunner)
		if len(conn) != 1 || len(runners) != 1 || runners[0].ID != devA {
			t.Errorf("conn %+v runners %+v", conn, runners)
		}
		// Another workspace's device is not found through this one.
		if _, err := tx.Device(ctx, "tws_1", devC); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("another workspace's device: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRevocationIsPermanentAndEndsHostRoles(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedWorkspace(t, db, "tws_1")
	d := device("tws_1", bo, devA, 1, domain.CapabilityWorkspaceHost)
	d.HostStatus = domain.HostActive
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, d) }); err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Hour)
	var first, second bool
	if err := db.Update(ctx, func(tx Tx) (err error) {
		first, err = tx.RevokeDevice(ctx, "tws_1", devA, at)
		if err != nil {
			return err
		}
		second, err = tx.RevokeDevice(ctx, "tws_1", devA, at.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("revoked: first %v, second %v", first, second)
	}
	_ = db.View(ctx, func(tx Tx) error {
		got, _ := tx.Device(ctx, "tws_1", devA)
		if got.RevokedAt == nil || !got.RevokedAt.Equal(at) || got.HostStatus != domain.HostNone {
			t.Errorf("%+v", got)
		}
		if hosts, _ := tx.DevicesWithCapability(ctx, "tws_1", domain.CapabilityWorkspaceHost); len(hosts) != 0 {
			t.Errorf("a revoked device is listed as a host: %+v", hosts)
		}
		return nil
	})
	// Nothing saves a revoked device, so a writer that read it before it was revoked cannot undo that.
	stale := d
	stale.Name = "stale write"
	err := db.Update(ctx, func(tx Tx) error { return tx.SaveDevice(ctx, stale) })
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("SaveDevice on a revoked device: %v", err)
	}
	if err := db.Update(ctx, func(tx Tx) error { return tx.RecordDeviceSeen(ctx, "tws_1", devA, at) }); err != nil {
		t.Fatal(err)
	}
	_ = db.View(ctx, func(tx Tx) error {
		got, _ := tx.Device(ctx, "tws_1", devA)
		if got.Name == "stale write" || got.RevokedAt == nil || got.LastSeenAt != nil {
			t.Errorf("a revoked device changed: %+v", got)
		}
		return nil
	})
	// Unknown devices are not found.
	err = db.Update(ctx, func(tx Tx) error { _, err := tx.RevokeDevice(ctx, "tws_1", devB, at); return err })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	err = db.Update(ctx, func(tx Tx) error { return tx.SaveDevice(ctx, device("tws_1", bo, devB, 4)) })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestSaveDeviceReplacesCapabilitiesAndLastSeenOnlyMovesForward(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedWorkspace(t, db, "tws_1")
	d := device("tws_1", bo, devA, 1, domain.CapabilityRunner)
	if err := db.Update(ctx, func(tx Tx) error { return tx.InsertDevice(ctx, d) }); err != nil {
		t.Fatal(err)
	}
	d.Capabilities = []domain.Capability{domain.CapabilityConnectivityHost}
	d.ConnectivityStatus = domain.HostJoining
	d.Name = "renamed"
	d.UpdatedAt = t0.Add(time.Minute)
	if err := db.Update(ctx, func(tx Tx) error { return tx.SaveDevice(ctx, d) }); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{t0.Add(10 * time.Minute), t0.Add(5 * time.Minute), t0.Add(10 * time.Minute)} {
		if err := db.Update(ctx, func(tx Tx) error { return tx.RecordDeviceSeen(ctx, "tws_1", devA, at) }); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.View(ctx, func(tx Tx) error {
		got, _ := tx.Device(ctx, "tws_1", devA)
		if got.Name != "renamed" || len(got.Capabilities) != 1 || got.Capabilities[0] != domain.CapabilityConnectivityHost || got.ConnectivityStatus != domain.HostJoining ||
			got.LastSeenAt == nil || !got.LastSeenAt.Equal(t0.Add(10*time.Minute)) || !got.UpdatedAt.Equal(t0.Add(time.Minute)) {
			t.Errorf("%+v", got)
		}
		return nil
	})
	// The public key and the owner are not editable: SaveDevice does not write them.
	d.PublicKey, d.MemberID = deviceid.EncodePublicKey(make([]byte, 32)), "someone-else"
	_ = db.Update(ctx, func(tx Tx) error { return tx.SaveDevice(ctx, d) })
	_ = db.View(ctx, func(tx Tx) error {
		got, _ := tx.Device(ctx, "tws_1", devA)
		if got.MemberID != bo || got.PublicKey == d.PublicKey {
			t.Errorf("SaveDevice changed the owner or the key: %+v", got)
		}
		return nil
	})
}

func TestDeletingAMemberDeletesTheirDevices(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	_, bo := seedWorkspace(t, db, "tws_1")
	if err := db.Update(ctx, func(tx Tx) error {
		return tx.InsertDevice(ctx, device("tws_1", bo, devA, 1, domain.CapabilityRunner))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx Tx) error { return tx.DeleteMember(ctx, "tws_1", bo) }); err != nil {
		t.Fatal(err)
	}
	_ = db.View(ctx, func(tx Tx) error {
		if _, err := tx.Device(ctx, "tws_1", devA); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%v", err)
		}
		return nil
	})
	var n int
	_ = db.pool.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT COUNT(*) FROM device_capabilities`).Scan(&n)
	})
	if n != 0 {
		t.Errorf("%d capability rows left", n)
	}
}

// The schema has nowhere to put what a registry must never hold. If a column is
// added to these tables, this test lists it and fails until it is shown to be
// something other workspaces' members and devices may know.
func TestTheDeviceTablesHaveNoColumnForASecret(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	cols := map[string][]string{}
	err := db.pool.View(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"devices", "device_capabilities"} {
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
		"devices":             "id workspace_id member_id name public_key host_status connectivity_status last_seen_at revoked_at created_at updated_at",
		"device_capabilities": "device_id workspace_id capability",
	}
	for table, w := range want {
		if got := strings.Join(cols[table], " "); got != w {
			t.Errorf("%s columns = %s\nwant %s", table, got, w)
		}
	}
	for table, cs := range cols {
		for _, c := range cs {
			for _, banned := range []string{"private", "secret", "token", "password", "credential", "api", "env", "path", "dir", "git", "seed", "cwd", "home", "ssh"} {
				if strings.Contains(c, banned) {
					t.Errorf("%s.%s could hold %s", table, c, banned)
				}
			}
		}
	}
}

// The profile table says what kind of machine a device is and nothing that could identify or reach it.
func TestTheProfileTableHasNoColumnForASecretOrAnAddress(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	var cols []string
	err := db.pool.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name FROM pragma_table_info('device_profiles')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			cols = append(cols, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cols, " "), "device_id workspace_id platform form sleeps sleep_events version reported_at"; got != want {
		t.Fatalf("columns = %s\nwant %s", got, want)
	}
	for _, c := range cols {
		for _, banned := range []string{"private", "secret", "token", "password", "credential", "api", "env", "path", "dir", "git", "seed", "cwd", "home", "ssh", "host", "addr", "ip"} {
			if strings.Contains(c, banned) {
				t.Errorf("device_profiles.%s could hold %s", c, banned)
			}
		}
	}
}

// The mailbox holds what a sender signed and what the device it was for said back, and nothing that could run or reach anything.
func TestTheMailboxHasNoColumnThatCouldHoldACommandOrASecret(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	var cols []string
	err := db.pool.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT name FROM pragma_table_info('device_messages')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			cols = append(cols, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cols, " "), "id workspace_id from_device_id to_device_id member_id action envelope state result created_at expires_at decided_at"; got != want {
		t.Fatalf("columns = %s\nwant %s", got, want)
	}
	for _, c := range cols {
		for _, banned := range []string{"private", "secret", "token", "password", "credential", "api", "env_", "path", "dir", "git", "seed", "cwd", "home", "ssh", "command", "script", "shell", "exec", "arg"} {
			if strings.Contains(c, banned) {
				t.Errorf("device_messages.%s could hold %s", c, banned)
			}
		}
	}
}
