package replicated

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/infra/rqlite"
	"devboard/internal/team/infra/rqlite/rqlitetest"
	"devboard/internal/team/store"
)

func TestABackupIsTakenCheckedKeptAndRestorable(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	a, b := open(t, c, 0), open(t, c, 1)
	k := seed(t, a)
	dest := DirDestination{Dir: filepath.Join(t.TempDir(), "backups")}

	info, err := a.Backup(bg, dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Position < int64(ms(t)) || info.SchemaVersion != ms(t) || info.Tables["members"] != 1 || info.Tables["tickets"] != 1 || info.SHA256 == "" || info.FromLocalCopy {
		t.Fatalf("%+v", info)
	}
	if info.ClusterID == "" {
		t.Fatal("a backup does not say whose database it is")
	}
	// It is a file only its owner can read, with a record beside it.
	fi, err := os.Stat(filepath.Join(dest.Dir, info.Name))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	list, err := ListBackups(bg, dest)
	if err != nil || len(list) != 1 || list[0].Name != info.Name || list[0].Position != info.Position {
		t.Fatalf("%+v %v", list, err)
	}
	// It verifies, and verifies again from another host's point of view.
	if _, err := VerifyBackup(bg, dest, info.Name, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	// Change the workspace, then restore: what was written after the backup is gone, on every host.
	if _, err := addMemberNamed(a, k, "Grace"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "b to have Grace", func() bool { return memberCount(b, k) == 2 })
	safety := DirDestination{Dir: filepath.Join(t.TempDir(), "before-restore")}
	restored, kept, err := a.Restore(bg, dest, info.Name, RestoreOptions{SafetyCopy: safety})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != info.Name || kept.Tables["members"] != 2 {
		t.Fatalf("restored %+v, kept %+v: the copy of what the restore replaced must have both members", restored, kept)
	}
	eventually(t, 20*time.Second, "both hosts to have the restored data", func() bool { return memberCount(a, k) == 1 && memberCount(b, k) == 1 })
	// The hosts agree with the cluster, and the workspace is writable again.
	for i, s := range []*Store{a, b} {
		if _, _, err := s.Verify(bg); err != nil {
			t.Errorf("host %d: %v", i, err)
		}
	}
	if _, err := addMemberNamed(b, k, "Hedy"); err != nil {
		t.Fatalf("a write after a restore: %v", err)
	}
	eventually(t, 10*time.Second, "a to have Hedy", func() bool { return memberCount(a, k) == 2 })
	// The safety copy is itself a backup that can be restored.
	if _, err := VerifyBackup(bg, safety, kept.Name, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func addMemberNamed(s store.Store, k seeded, name string) (string, error) {
	id := domain.NewID(domain.PrefixMember)
	return id, s.Update(bg, func(tx store.Tx) error {
		return tx.InsertMember(bg, domain.Member{ID: id, WorkspaceID: k.ws, Name: name, Role: domain.RoleMember, CreatedAt: time.Now()}, "h-"+id)
	})
}

func memberCount(s store.Store, k seeded) (n int) {
	_ = s.View(bg, func(tx store.Tx) error {
		m, err := tx.Members(bg, k.ws)
		n = len(m)
		return err
	})
	return n
}

func TestARestoreRefusesWhatIsNotSafe(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	s := open(t, c, 0)
	seed(t, s)
	dest := DirDestination{Dir: t.TempDir()}
	info, err := s.Backup(bg, dest)
	if err != nil {
		t.Fatal(err)
	}
	safety := DirDestination{Dir: t.TempDir()}
	// No place for the copy of what it replaces: no restore.
	if _, _, err := s.Restore(bg, dest, info.Name, RestoreOptions{}); err == nil {
		t.Error("a restore with nowhere to keep what it replaces went ahead")
	}
	// A backup that was altered is refused, and nothing is changed.
	p := filepath.Join(dest.Dir, info.Name)
	raw, _ := os.ReadFile(p)
	raw[len(raw)/2] ^= 0xff
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	before := position(t, s)
	if _, _, err := s.Restore(bg, dest, info.Name, RestoreOptions{SafetyCopy: safety}); err == nil {
		t.Fatal("a damaged backup was restored")
	}
	if position(t, s) != before {
		t.Error("a refused restore changed the cluster")
	}
	if names, _ := safety.List(bg); len(names) != 0 {
		t.Errorf("a refused restore left %v", names)
	}
	// A file that is not a workspace database is not a backup.
	bad := DirDestination{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(bad.Dir, "workspace-x.sqlite"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(bg, bad, "workspace-x.sqlite", t.TempDir()); err == nil {
		t.Error("garbage verified")
	}
	// Names with a path in them are not names.
	for _, name := range []string{"../x", "a/b", "", ".hidden", ".."} {
		if _, err := dest.Open(bg, name); err == nil {
			t.Errorf("%q was opened", name)
		}
	}
}

func TestRetentionKeepsTheNewestAndTheYoungAndNeverTheLastOne(t *testing.T) {
	dest := DirDestination{Dir: t.TempDir()}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	mk := func(daysAgo, pos int) string {
		at := now.AddDate(0, 0, -daysAgo)
		name := backupName(at, int64(pos))
		if err := dest.Put(bg, name, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
		if err := dest.Put(bg, name+".json", strings.NewReader(`{"createdAt":"`+at.Format(time.RFC3339)+`"}`)); err != nil {
			t.Fatal(err)
		}
		return name
	}
	var names []string
	for d := 0; d < 10; d++ {
		names = append(names, mk(d, 100-d))
	}
	removed, err := Retention{KeepLast: 3, KeepFor: 5 * 24 * time.Hour}.Apply(bg, dest, now)
	if err != nil {
		t.Fatal(err)
	}
	// Kept: the newest three, and everything under five days old (days 0..4); removed: days 5..9.
	if len(removed) != 5 {
		t.Fatalf("removed %v", removed)
	}
	left, _ := ListBackups(bg, dest)
	if len(left) != 5 || left[0].Name != names[0] {
		t.Fatalf("%v", left)
	}
	for _, r := range removed {
		if _, err := os.Stat(filepath.Join(dest.Dir, r+".json")); err == nil {
			t.Errorf("the record of %s was kept", r)
		}
	}
	// With no age limit, only the newest KeepLast stay; and the last one always does.
	if _, err := (Retention{KeepLast: 2}).Apply(bg, dest, now); err != nil {
		t.Fatal(err)
	}
	if left, _ := ListBackups(bg, dest); len(left) != 2 {
		t.Fatalf("%v", left)
	}
	if _, err := (Retention{}).Apply(bg, dest, now); err != nil {
		t.Fatal(err)
	}
	if left, _ := ListBackups(bg, dest); len(left) != 1 || left[0].Name != names[0] {
		t.Fatalf("%v", left)
	}
}

func TestStatusIsHonestAboutWhatTheClusterIs(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	s := open(t, c, 0)
	st := s.Status(bg)
	if st.State != domain.StorageReplicated || !st.Writable || st.ReadOnly || st.Topology.Voters != 3 || st.Topology.Level != domain.TopologyRecommended || len(st.Hosts) != 3 || st.Leader == "" {
		t.Fatalf("%+v", st)
	}
	if st.SchemaVersion != ms(t) || st.Local.Position < int64(ms(t)) {
		t.Fatalf("%+v", st)
	}
	// A cluster of one is valid and says it has no high availability.
	one := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	st1 := open(t, one, 0).Status(bg)
	if st1.Topology.Level != domain.TopologySingle || st1.Topology.HighAvailability || !st1.Writable {
		t.Fatalf("%+v", st1.Topology)
	}
	_ = rqlite.Version
	_ = context.Background
}
