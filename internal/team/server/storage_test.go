package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/config"
	"devboard/internal/team/domain"
	"devboard/internal/team/infra/pki"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/service"
	"devboard/internal/team/store/replicated"
)

// These tests run a Workspace Host's storage as Team ships it: the pinned database program, supervised, with the replicated
// store on it. They skip when the program has not been fetched (scripts/fetch-rqlite.sh); CI requires it.

func testLog() *slog.Logger {
	if os.Getenv("WERKBORD_TEST_VERBOSE") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return nil
}

func storageCfg(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DatabaseDirs = []string{rqlitetest.BinaryDir(t)}
	cfg.StoragePort, cfg.StorageRaftPort = rqlitetest.FreePort(t), rqlitetest.FreePort(t)
	cfg.Storage = config.StorageReplicated
	return cfg
}

func TestAWorkspaceIsCreatedInAClusterOfOneAndComesBackAfterARestart(t *testing.T) {
	cfg := storageCfg(t)
	w, err := CreateStorage(bg, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := w.Service.CreateWorkspace(bg, "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	if !w.Service.StorageReplicated() {
		t.Fatal("the service does not know its data is in a cluster")
	}
	st, err := w.Service.StorageStatus(bg, mustAuth(t, w.Service, created.Token))
	if err != nil {
		t.Fatal(err)
	}
	if st.State != domain.StorageReplicated || st.Topology.Voters != 1 || st.Topology.Level != domain.TopologySingle || st.Topology.HighAvailability || !st.Writable {
		t.Fatalf("%+v", st)
	}
	if !strings.Contains(st.Topology.Summary, "no high availability") {
		t.Errorf("a cluster of one is not described honestly: %q", st.Topology.Summary)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Where the data is was recorded, with nothing secret in it; the credentials are sealed in the vault.
	raw, err := os.ReadFile(cfg.StorageMarkerPath())
	if err != nil {
		t.Fatal(err)
	}
	m, ok, err := readMarker(cfg)
	if err != nil || !ok || m.Kind != config.StorageReplicated || m.Role != RoleVoter || m.Fresh {
		t.Fatalf("%+v %v %v", m, ok, err)
	}
	v, _ := (&Storage{cfg: cfg}).vault()
	secrets, err := v.Storage()
	if err != nil {
		t.Fatal(err)
	}
	for _, pw := range []string{secrets.AppPassword, secrets.AdminPassword, secrets.NodePassword} {
		if strings.Contains(string(raw), pw) {
			t.Error("a database password is in storage.json")
		}
		if b, err := os.ReadFile(filepath.Join(cfg.PKIDir(), "storage.sealed")); err != nil || strings.Contains(string(b), pw) {
			t.Error("a database password is in the sealed file in clear")
		}
	}
	// A restart finds the workspace where it was.
	w2, err := OpenWorkspace(bg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	a, err := w2.Service.Authenticate(bg, created.Token)
	if err != nil || a.Workspace.Name != "Acme" {
		t.Fatalf("%+v %v", a, err)
	}
	// A second creation in the same directory is refused.
	if _, err := CreateStorage(bg, cfg, nil, ""); err == nil {
		t.Error("a second workspace was created over the first")
	}
}

func mustAuth(t *testing.T, svc *service.Service, token string) service.Actor {
	t.Helper()
	a, err := svc.Authenticate(bg, token)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestWithoutTheDatabaseProgramAReplicatedWorkspaceIsNotStartedAndNothingIsLeftBehind(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.StoragePort, cfg.StorageRaftPort = 41111, 41112
	cfg.DatabaseDirs = nil
	cfg.Storage = config.StorageReplicated
	// The test binary's own directory has no rqlited, and neither does the PATH count.
	t.Setenv("PATH", t.TempDir())
	_, err := CreateStorage(bg, cfg, nil, "")
	if err == nil || !errors.Is(err, ErrNoDatabaseProgram) && !strings.Contains(err.Error(), "database program") {
		t.Fatalf("%v", err)
	}
	if _, serr := os.Stat(cfg.StorageMarkerPath()); serr == nil {
		t.Error("a marker was left behind")
	}
	if _, serr := os.Stat(cfg.StorageDir()); serr == nil {
		t.Error("the storage directory was left behind")
	}
}

// ---- moving a workspace out of a single file ----

func legacyWorkspace(t *testing.T, cfg config.Config) (token string, digest replicated.Digest) {
	t.Helper()
	single := cfg
	single.Storage = config.StorageSingleFile
	w, err := CreateStorage(bg, single, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.Service.CreateWorkspace(bg, "Acme", "Ada", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	a := mustAuth(t, w.Service, c.Token)
	p, err := w.Service.CreateProject(bg, a, service.ProjectInput{Name: "App"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := w.Service.CreateTicket(bg, a, p.ID, service.TicketInput{Title: "Ticket", Status: domain.TicketAvailable}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// What the workspace is, as bytes, so that the original can be shown to be unchanged.
	db, err := sql.Open("sqlite", "file:"+cfg.DBPath()+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d, err := replicated.DigestOfFile(bg, db)
	if err != nil {
		t.Fatal(err)
	}
	// A marker that says "one file" is removed, so that this is what a workspace from before replication looks like.
	_ = os.Remove(cfg.StorageMarkerPath())
	return c.Token, d
}

func TestAWorkspaceInOneFileMovesIntoAClusterWithEverythingCheckedAndTheOriginalKept(t *testing.T) {
	cfg := storageCfg(t)
	token, original := legacyWorkspace(t, cfg)

	// A dry run changes nothing.
	rep, err := MigrateStorage(bg, cfg, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || rep.Tables["tickets"] != 4 {
		t.Fatalf("%+v", rep)
	}
	if _, err := os.Stat(cfg.StorageMarkerPath()); err == nil {
		t.Fatal("a dry run switched the workspace")
	}
	if entries, _ := filepath.Glob(filepath.Join(cfg.DataDir, "migration-*")); len(entries) != 0 {
		t.Errorf("a dry run left %v", entries)
	}

	rep, err = MigrateStorage(bg, cfg, nil, false)
	if err != nil {
		t.Fatalf("%v\n%v", err, rep.Steps)
	}
	if rep.RollbackCopy == "" || len(rep.Steps) < 6 {
		t.Fatalf("%+v", rep)
	}
	// The rollback copy is a database, and the original is as it was.
	for name, path := range map[string]string{"the rollback copy": rep.RollbackCopy, "the original": cfg.DBPath()} {
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		d, err := replicated.DigestOfFile(bg, db)
		db.Close()
		if err != nil || original.Diff(d) != "" {
			t.Errorf("%s is not the original: %v %s", name, err, original.Diff(d))
		}
	}
	m, ok, err := readMarker(cfg)
	if err != nil || !ok || m.Kind != config.StorageReplicated || m.MigratedFrom != cfg.DBPath() || m.RollbackCopy != rep.RollbackCopy || m.Fresh {
		t.Fatalf("%+v %v %v", m, ok, err)
	}

	// The workspace opens in the cluster with its data, and writes.
	w, err := OpenWorkspace(bg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.Service.Authenticate(bg, token)
	if err != nil || a.Workspace.Name != "Acme" || a.Member.Name != "Ada" {
		t.Fatalf("%+v %v", a, err)
	}
	projects, err := w.Service.ListProjects(bg, a)
	if err != nil || len(projects) != 1 {
		t.Fatalf("%v %v", projects, err)
	}
	board, err := w.Service.Board(bg, a, projects[0].ID)
	if err != nil || len(board.Tickets) != 4 {
		t.Fatalf("%d tickets: %v", len(board.Tickets), err)
	}
	k, err := w.Service.CreateTicket(bg, a, projects[0].ID, service.TicketInput{Title: "After the move", Status: domain.TicketAvailable})
	if err != nil || k.Number != 5 {
		t.Fatalf("the numbering did not continue: %+v %v", k, err)
	}
	if _, _, err := w.Storage.ReplicatedStore().Verify(bg); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// And a workspace that is in a cluster is not moved again.
	if _, err := MigrateStorage(bg, cfg, nil, false); err == nil {
		t.Error("a workspace that is in a cluster was moved again")
	}
}

func TestAMigrationThatCannotBeTrustedIsRefusedOrUndoneAndLeavesTheOriginalAsItWas(t *testing.T) {
	t.Run("the server is still running", func(t *testing.T) {
		cfg := storageCfg(t)
		_, original := legacyWorkspace(t, cfg)
		db, err := sql.Open("sqlite", "file:"+cfg.DBPath())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		conn, err := db.Conn(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(bg, `BEGIN IMMEDIATE`); err != nil {
			t.Fatal(err)
		}
		rep, err := MigrateStorage(bg, cfg, nil, false)
		if err == nil || !strings.Contains(err.Error(), "in use") {
			t.Fatalf("%v %v", err, rep.Steps)
		}
		_, _ = conn.ExecContext(bg, `ROLLBACK`)
		assertUntouched(t, cfg, original)
	})
	t.Run("the file is not a database", func(t *testing.T) {
		cfg := storageCfg(t)
		if err := os.WriteFile(cfg.DBPath(), []byte("this is not a database, though it is long enough to look like one at a glance"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := MigrateStorage(bg, cfg, nil, false); err == nil {
			t.Fatal("garbage was moved")
		}
		assertNoSwitch(t, cfg)
	})
	t.Run("nothing to move", func(t *testing.T) {
		cfg := storageCfg(t)
		if _, err := MigrateStorage(bg, cfg, nil, false); err == nil {
			t.Fatal("an empty directory was migrated")
		}
	})
	t.Run("the load is found not to be the original", func(t *testing.T) {
		cfg := storageCfg(t)
		_, original := legacyWorkspace(t, cfg)
		migrateFault = func(stage string) error {
			if stage == "loaded" {
				return errors.New("an injected failure after the load")
			}
			return nil
		}
		defer func() { migrateFault = nil }()
		rep, err := MigrateStorage(bg, cfg, nil, false)
		if err == nil || !strings.Contains(err.Error(), "injected") {
			t.Fatalf("%v %v", err, rep.Steps)
		}
		assertUntouched(t, cfg, original)
		// What was made is kept aside, not removed.
		if left, _ := filepath.Glob(cfg.StorageDir() + ".failed-*"); len(left) != 1 {
			t.Errorf("the failed attempt's files are %v", left)
		}
		if kept, _ := filepath.Glob(filepath.Join(cfg.DataDir, "backups", "before-replication-*")); len(kept) != 1 {
			t.Errorf("the rollback copy is %v", kept)
		}
		// And the workspace still opens, from its file, as it did.
		w, err := OpenWorkspace(bg, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Close()
		if w.Storage.Replicated() {
			t.Error("the workspace is in a cluster after a migration that failed")
		}
		// A later attempt, with the fault gone, succeeds.
		_ = w.Close()
		migrateFault = nil
		if _, err := MigrateStorage(bg, cfg, nil, false); err != nil {
			t.Fatalf("the second attempt: %v", err)
		}
	})
}

func assertNoSwitch(t *testing.T, cfg config.Config) {
	t.Helper()
	if _, err := os.Stat(cfg.StorageMarkerPath()); err == nil {
		t.Error("the workspace was switched to a cluster")
	}
}

func assertUntouched(t *testing.T, cfg config.Config, original replicated.Digest) {
	t.Helper()
	assertNoSwitch(t, cfg)
	db, err := sql.Open("sqlite", "file:"+cfg.DBPath()+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d, err := replicated.DigestOfFile(bg, db)
	if err != nil || original.Diff(d) != "" {
		t.Errorf("the original changed: %v %s", err, original.Diff(d))
	}
}

func TestAWorkspaceInAClusterOfOneMovesBackIntoOneFileWithWhatItHoldsNow(t *testing.T) {
	cfg := storageCfg(t)
	token, _ := legacyWorkspace(t, cfg)
	if _, err := MigrateStorage(bg, cfg, nil, false); err != nil {
		t.Fatal(err)
	}
	w, err := OpenWorkspace(bg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := mustAuth(t, w.Service, token)
	ps, _ := w.Service.ListProjects(bg, a)
	if _, err := w.Service.CreateTicket(bg, a, ps[0].ID, service.TicketInput{Title: "Written in the cluster", Status: domain.TicketAvailable}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rep, err := RollbackStorage(bg, cfg, nil)
	if err != nil {
		t.Fatalf("%v %v", err, rep.Steps)
	}
	if rep.Kept == "" || rep.Replaced == "" {
		t.Fatalf("%+v", rep)
	}
	// It is in one file again, with what was written in the cluster, and the old file and the cluster's files are kept.
	w2, err := OpenWorkspace(bg, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if w2.Storage.Replicated() {
		t.Fatal("still in a cluster")
	}
	a = mustAuth(t, w2.Service, token)
	ps, _ = w2.Service.ListProjects(bg, a)
	b, err := w2.Service.Board(bg, a, ps[0].ID)
	if err != nil || len(b.Tickets) != 5 {
		t.Fatalf("%d tickets, want the 4 it had and the one written in the cluster: %v", len(b.Tickets), err)
	}
	for _, p := range []string{rep.Kept, rep.Replaced} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was not kept: %v", p, err)
		}
	}
}

// ---- adding and removing Workspace Hosts ----

type hostEnv struct {
	cfg config.Config
	st  *Storage
	w   *Workspace
}

// addHost makes a second Workspace Host the way the product does: the first host plans it (what its node needs to join),
// the plan is sealed to the new host and stored in its vault when it collects it, and its first start joins.
func addHost(t *testing.T, first *hostEnv, nodeID string) *hostEnv {
	t.Helper()
	plan, err := first.st.PlanHost(bg, service.HostToAdd{NodeID: nodeID, OverlayAddr: "127.0.0.1", ExistingHosts: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatalf("planning %s: %v", nodeID, err)
	}
	cfg := storageCfg(t)
	sealer, err := SealerFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v, err := pki.OpenVault(cfg.PKIDir(), sealer)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SaveStorage(pki.StorageSecrets{ClusterID: plan.ClusterID, AppPassword: plan.AppPassword, AdminPassword: plan.AdminPassword, NodePassword: plan.NodePassword,
		NodeID: plan.NodeID, HTTPAddr: plan.HTTPAddr, RaftAddr: plan.RaftAddr, JoinRaft: plan.JoinRaft}); err != nil {
		t.Fatal(err)
	}
	st, err := OpenStorage(bg, cfg, testLog())
	if err != nil {
		t.Fatal(err)
	}
	if st.marker.Role != RoleReplica || st.marker.NodeID != nodeID {
		t.Fatalf("a host that collected its credentials did not start as a replica: %+v", st.marker)
	}
	svc := service.New(st.DB())
	st.Attach(svc, nil)
	st.Start(bg)
	t.Cleanup(func() { _ = st.Close(bg) })
	return &hostEnv{cfg: cfg, st: st, w: &Workspace{Storage: st, Service: svc}}
}

func firstHost(t *testing.T) *hostEnv {
	t.Helper()
	cfg := storageCfg(t)
	w, err := CreateStorage(bg, cfg, testLog(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Service.CreateWorkspace(bg, "Acme", "Ada", ""); err != nil {
		t.Fatal(err)
	}
	h := &hostEnv{cfg: cfg, st: w.Storage, w: w}
	ports := map[netip.Addr]int{}
	_ = ports
	h.st.planPorts = func(netip.Addr) (int, int) { return rqlitetest.FreePort(t), rqlitetest.FreePort(t) }
	h.st.Attach(w.Service, func(context.Context) (netip.Addr, error) { return netip.MustParseAddr("127.0.0.1"), nil })
	return h
}

func waitUntil(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestAHostIsAddedJoinsAsAReplicaIsCheckedAndOnlyThenVotesAndIsThenRemoved(t *testing.T) {
	first := firstHost(t)
	b := addHost(t, first, "host-b")
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("first node tail:\n%s\nsecond node tail:\n%s", strings.Join(first.st.sup.Status().Tail, "\n"), strings.Join(b.st.sup.Status().Tail, "\n"))
		}
	})
	// It joins without a vote, and stays that way until it has caught up and been checked against the cluster's data.
	waitUntil(t, 3*time.Minute, "the second host to vote", func() bool {
		return first.st.Status(bg).Topology.Voters == 2
	})
	// The cluster lists it as a voter a moment before the host has recorded that it is one.
	waitUntil(t, 3*time.Minute, "the second host to record that it votes", func() bool {
		b.st.mu.Lock()
		defer b.st.mu.Unlock()
		return b.st.marker.Role == RoleVoter
	})
	st := first.st.Status(bg)
	if st.Topology.Level != domain.TopologyTwo || st.Topology.HighAvailability {
		t.Fatalf("two hosts are not described honestly: %+v", st.Topology)
	}
	// Its copy is the workspace's whole data.
	waitUntil(t, 30*time.Second, "its copy of the workspace", func() bool {
		_, err := b.w.Service.Authenticate(bg, "wbt_nothing")
		return errors.Is(err, domain.ErrUnauthenticated)
	})

	c := addHost(t, first, "host-c")
	waitUntil(t, 3*time.Minute, "three voters", func() bool { return first.st.Status(bg).Topology.Voters == 3 })
	if lvl := first.st.Status(bg).Topology.Level; lvl != domain.TopologyRecommended {
		t.Fatalf("%v", lvl)
	}
	_ = c

	// A removal that would leave no quorum is refused: with one of the other two down, taking out the third would leave
	// two voters of which one answers.
	if err := b.st.sup.Stop(bg); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, "the cluster to see that host-b is down", func() bool {
		for _, h := range first.st.Status(bg).Hosts {
			if h.NodeID == "host-b" && !h.Reachable {
				return true
			}
		}
		return false
	})
	if _, err := first.st.RemoveHost(bg, "host-c"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a removal that leaves no quorum was not refused: %v", err)
	}
	// Removing the one that is down is safe, and the cluster says what is left.
	warn, err := first.st.RemoveHost(bg, "host-b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn, "tolerates the loss of 0") {
		t.Errorf("the warning for going from three to two: %q", warn)
	}
	waitUntil(t, 30*time.Second, "two voters", func() bool { return first.st.Status(bg).Topology.Voters == 2 })
	// Removing a host that is not there is not an error.
	if _, err := first.st.RemoveHost(bg, "host-b"); err != nil {
		t.Fatal(err)
	}
}

func TestAHostThatIsRemovedStopsItsNodeAndKeepsItsFiles(t *testing.T) {
	first := firstHost(t)
	b := addHost(t, first, "host-b")
	waitUntil(t, 3*time.Minute, "two voters", func() bool { return first.st.Status(bg).Topology.Voters == 2 })
	if _, err := first.st.RemoveHost(bg, "host-b"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 60*time.Second, "the removed host to notice and put its files aside", func() bool {
		left, _ := filepath.Glob(filepath.Join(b.cfg.StorageDir(), "node.removed-*"))
		return len(left) == 1
	})
	b.st.mu.Lock()
	role := b.st.marker.Role
	b.st.mu.Unlock()
	if role != RoleRemoved {
		t.Errorf("the removed host's role is %s", role)
	}
	if st := b.st.sup.Status(); st.State != rqlite.StateStopped {
		t.Errorf("the removed host's node is %v", st.State)
	}
	if left, _ := filepath.Glob(filepath.Join(b.cfg.StorageDir(), "node.removed-*")); len(left) != 1 {
		t.Errorf("its files were not kept aside: %v", left)
	}
	// It does not start again as a host.
	_ = b.st.Close(bg)
	if _, err := OpenStorage(bg, b.cfg, nil); err == nil || !strings.Contains(err.Error(), "taken out") {
		t.Fatalf("a removed host started: %v", err)
	}
	// The cluster is a cluster of one again, still working (the status follows the cluster within a moment).
	waitUntil(t, 30*time.Second, "the cluster to be one voter and writable", func() bool {
		st := first.st.Status(bg)
		return st.Topology.Voters == 1 && st.Writable
	})
}
