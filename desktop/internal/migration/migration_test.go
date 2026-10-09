package migration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyAdoptionPreservesSeparateDatabasesAndSecrets(t *testing.T) {
	for _, scenario := range []string{"individual", "team", "both", "devboard"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			m := &Manager{Dir: filepath.Join(dir, "migration")}
			var dbs []*sql.DB
			for _, kind := range []string{"personal_data", "team_user_data"} {
				if scenario == "individual" && kind == "team_user_data" || scenario == "team" && kind == "personal_data" {
					continue
				}
				if scenario == "devboard" && kind == "personal_data" {
					kind = "devboard_data"
				}
				root := filepath.Join(dir, kind)
				os.MkdirAll(root, 0700)
				m.Roots = append(m.Roots, Installation{Kind: kind, Path: root})
				name := "devboard.db"
				if kind == "team_user_data" {
					name = "team.db"
				}
				db, err := sql.Open("sqlite", filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				dbs = append(dbs, db)
				if _, err = db.Exec("PRAGMA journal_mode=WAL; CREATE TABLE tasks(id TEXT); INSERT INTO tasks VALUES('preserved-task')"); err != nil {
					t.Fatal(err)
				}
				for _, file := range []string{"token", "license.json", "identity.json", "config.json"} {
					if err = os.WriteFile(filepath.Join(root, file), []byte("retained-"+kind), 0600); err != nil {
						t.Fatal(err)
					}
				}
				os.MkdirAll(filepath.Join(root, "worktrees", "repo"), 0700)
				os.WriteFile(filepath.Join(root, "worktrees", "repo", "uncommitted"), []byte("agent work"), 0600)
			}
			st, err := m.Status()
			if err != nil || st.Phase != "detected" {
				t.Fatalf("%+v %v", st, err)
			}
			st, err = m.Prepare(context.Background())
			if err != nil || st.Phase != "prepared" {
				t.Fatalf("prepare: %+v %v", st, err)
			}
			backup := st.Backup
			// A failed verification leaves the transaction retryable and its backup intact.
			if _, err = m.Verify(context.Background(), func(context.Context) error { return errors.New("service unavailable") }); err == nil {
				t.Fatal("failed service verified")
			}
			reopened := &Manager{Dir: m.Dir, Roots: m.Roots}
			st, err = reopened.Prepare(context.Background())
			if err != nil || st.Backup != backup {
				t.Fatalf("retry replaced snapshot: %+v %v", st, err)
			}
			st, err = reopened.Verify(context.Background(), func(context.Context) error { return nil })
			if err != nil || st.Phase != "verified" {
				t.Fatalf("verify: %+v %v", st, err)
			}
			// New work after adoption survives rollback; restoring a stale snapshot would lose it.
			for _, db := range dbs {
				if _, err = db.Exec("INSERT INTO tasks VALUES('new-work')"); err != nil {
					t.Fatal(err)
				}
			}
			st, err = reopened.Rollback()
			if err != nil || st.Phase != "rolled_back" {
				t.Fatalf("rollback: %+v %v", st, err)
			}
			for _, db := range dbs {
				var count int
				if err = db.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil || count != 2 {
					t.Fatalf("data lost: %d %v", count, err)
				}
			}
			matches, _ := filepath.Glob(filepath.Join(m.Dir, backup, "*", "*.db"))
			if len(matches) != len(dbs) {
				t.Fatal("missing separate database snapshots")
			}
			for _, p := range matches {
				db, err := sql.Open("sqlite", p)
				if err != nil {
					t.Fatal(err)
				}
				var n int
				err = db.QueryRow("SELECT count(*) FROM tasks").Scan(&n)
				db.Close()
				if err != nil || n != 1 {
					t.Fatalf("WAL snapshot: %d %v", n, err)
				}
			}
			for _, root := range m.Roots {
				for _, file := range []string{"token", "license.json", "identity.json", "config.json", "worktrees/repo/uncommitted"} {
					if _, err = os.Stat(filepath.Join(root.Path, file)); err != nil {
						t.Fatal("original state removed", err)
					}
				}
			}
			st, err = reopened.Prepare(context.Background())
			if err != nil || st.Backup == backup {
				t.Fatalf("new adoption missing separate snapshot: %+v %v", st, err)
			}
			if _, err = os.Stat(filepath.Join(m.Dir, backup)); err != nil {
				t.Fatal("rollback snapshot was deleted")
			}
		})
	}
}

func TestInterruptedSnapshotAndTamper(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "personal")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "token"), []byte("credential"), 0600)
	m := &Manager{Dir: filepath.Join(dir, "migration"), Roots: []Installation{{Kind: "personal_data", Path: root}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Prepare(ctx); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	// A process crash releases the advisory lock; an orphan lock file cannot block recovery.
	if _, err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	x, err := m.read()
	if err != nil {
		t.Fatal(err)
	}
	for file := range x.Hashes {
		os.WriteFile(filepath.Join(m.Dir, x.Backup, file), []byte("tampered"), 0600)
		break
	}
	if _, err = m.Verify(context.Background(), func(context.Context) error { return nil }); err == nil {
		t.Fatal("changed snapshot accepted")
	}
	if _, err = m.Rollback(); err == nil {
		t.Fatal("changed snapshot rolled back without reporting corruption")
	}
	if err = os.WriteFile(filepath.Join(m.Dir, "migration.json"), []byte(`{"schema":1,"phase":"verified","backup":"../../outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Status(); err == nil {
		t.Fatal("escaping marker accepted")
	}
}

func TestCleanFreeInstallationAndUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Dir: filepath.Join(dir, "migration"), Roots: []Installation{{Kind: "personal_data", Path: filepath.Join(dir, "absent")}}}
	st, err := m.Status()
	if err != nil || st.Phase != "not_needed" {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err = os.Stat(m.Dir); !os.IsNotExist(err) {
		t.Fatal("discovery wrote data")
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	m.Roots = []Installation{{Kind: "personal_data", Path: link}}
	if _, err = m.Status(); err == nil {
		t.Fatal("symlink silently adopted")
	}
}

func TestVerificationMayReadStatusWithoutDeadlocking(t *testing.T) {
	m := &Manager{Dir: filepath.Join(t.TempDir(), "migration")}
	if _, err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Verify(context.Background(), func(context.Context) error { _, err := m.Status(); return err }); err != nil {
		t.Fatal(err)
	}
}
