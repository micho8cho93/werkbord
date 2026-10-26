package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func memoryRecord(t *testing.T, summary string) *domain.Memory {
	t.Helper()
	c, err := (domain.MemoryContent{Type: "context", Authority: domain.MemoryObservation, Source: domain.MemoryProvenance{Type: domain.MemorySourceImport, Reference: "fixture:1"}, Summary: summary}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	at := now()
	return &domain.Memory{ID: domain.NewID("mem"), Scope: domain.MemoryScope{OwnerID: "owner", Repository: "github.com/acme/repo"}, MemoryContent: c, Fingerprint: domain.MemoryFingerprint(c), SchemaVersion: 1, Version: 1, CreatedAt: at, UpdatedAt: at}
}
func TestMemoryStoreCASAndRollback(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	m := memoryRecord(t, "SQLite original")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, m) })
	stale := *m
	m.Summary = "SQLite changed"
	m.Fingerprint = domain.MemoryFingerprint(m.MemoryContent)
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Update(ctx, m) })
	if m.Version != 2 {
		t.Fatal(m.Version)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Update(ctx, &stale) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	wrong := stale
	wrong.Scope.ProjectID = "prj_other"
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Update(ctx, &wrong) }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Delete(ctx, m.Scope, m.ID, 1) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	boom := errors.New("rollback")
	temporary := memoryRecord(t, "rollback invisible")
	if err := db.Update(ctx, func(tx store.Tx) error {
		if err := tx.Memory().Create(ctx, temporary); err != nil {
			return err
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		u, err := tx.Memory().Usage(ctx, "")
		if err != nil || u.Records != 1 {
			t.Fatalf("usage after rollback %+v %v", u, err)
		}
		page, err := tx.Memory().List(ctx, m.Scope, domain.MemoryQuery{Text: "rollback"})
		if err != nil || len(page.Records) != 0 {
			t.Fatalf("FTS after rollback %+v %v", page, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMemoryDatabaseIntegrityConstraints(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	target := memoryRecord(t, "target")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, target) })
	dup := *target
	dup.ID = domain.NewID("mem")
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Create(ctx, &dup) }); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatal(err)
	}
	linked := memoryRecord(t, "duplicate evidence")
	linked.DuplicateOf = target.ID
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, linked) })
	bad := memoryRecord(t, "wrong repository link")
	bad.Scope.Repository = "github.com/acme/other"
	bad.DuplicateOf = target.ID
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Create(ctx, bad) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	bad = memoryRecord(t, "unknown state")
	bad.State = "unknown"
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Create(ctx, bad) }); err == nil {
		t.Fatal("unknown state accepted")
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Delete(ctx, target.Scope, target.ID, target.Version) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	// Global hard ceilings are also enforced by SQL, independently of services.
	if _, err := db.writer.ExecContext(ctx, `UPDATE memory_usage SET records=100000 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Create(ctx, memoryRecord(t, "overflow")) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `UPDATE memory_usage SET records=2,bytes=268435456 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Create(ctx, memoryRecord(t, "byte overflow")) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	linked.Summary = "larger duplicate evidence than before"
	linked.Fingerprint = domain.MemoryFingerprint(linked.MemoryContent)
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Memory().Update(ctx, linked) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
}
func TestMemoryMigrationAndPreUpgradeRestore(t *testing.T) {
	// Upgrade the actual preceding schema with existing application data.
	path := oldDatabase(t, 16)
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := memoryRecord(t, "post upgrade memory")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, m) })
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(BackupDir(path), "werkbord-v16-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatal(backups, err)
	}
	info, err := Inspect(ctx, backups[0])
	if err != nil || info.Version != 16 || info.Integrity != "ok" {
		t.Fatal(info, err)
	}
	// Restore a pre-memory backup to a separate offline path, then upgrade it.
	raw, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restoredPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, restoredPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.View(ctx, func(tx store.Tx) error {
		p, err := tx.Projects().Get(ctx, "prj_1")
		if err != nil || p.Name != "keep me" {
			t.Fatalf("restored project %+v %v", p, err)
		}
		u, err := tx.Memory().Usage(ctx, "")
		if err != nil || u.Records != 0 {
			t.Fatalf("restored memory %+v %v", u, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Current databases reopen without reapplying the migration or dropping memory.
	reopened, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.View(ctx, func(tx store.Tx) error {
		got, err := tx.Memory().Get(ctx, m.Scope, m.ID)
		if err != nil || got.Summary != m.Summary {
			t.Fatalf("reopened memory %+v %v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMemorySnapshotRestoreIncludesFTSAndLinks(t *testing.T) {
	db, _ := openTemp(t)
	ctx := context.Background()
	// A deleted early row leaves a gap. VACUUM INTO must not renumber content
	// rows away from the FTS row IDs in a consistent backup.
	discarded := memoryRecord(t, "discarded early row")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, discarded) })
	target := memoryRecord(t, "SQLite durable memory")
	older := memoryRecord(t, "obsolete SQLite memory")
	older.State = domain.MemorySuperseded
	older.SupersededBy = target.ID
	mustUpdate(t, db, func(tx store.Tx) error {
		if err := tx.Memory().Create(ctx, target); err != nil {
			return err
		}
		return tx.Memory().Create(ctx, older)
	})
	mustUpdate(t, db, func(tx store.Tx) error {
		return tx.Memory().Delete(ctx, discarded.Scope, discarded.ID, discarded.Version)
	})
	backup := filepath.Join(t.TempDir(), "snapshot.db")
	if _, err := db.writer.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
		t.Fatal(err)
	}
	// Deleting from the live database must not affect the snapshot.
	mustUpdate(t, db, func(tx store.Tx) error {
		if err := tx.Memory().Delete(ctx, older.Scope, older.ID, older.Version); err != nil {
			return err
		}
		return tx.Memory().Delete(ctx, target.Scope, target.ID, target.Version)
	})
	restored, err := Open(ctx, backup, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.View(ctx, func(tx store.Tx) error {
		page, err := tx.Memory().List(ctx, target.Scope, domain.MemoryQuery{Text: "sqlite"})
		if err != nil || len(page.Records) != 2 {
			t.Fatalf("restored search %+v %v", page, err)
		}
		got, err := tx.Memory().Get(ctx, older.Scope, older.ID)
		if err != nil || got.SupersededBy != target.ID {
			t.Fatalf("restored link %+v %v", got, err)
		}
		u, err := tx.Memory().Usage(ctx, "")
		if err != nil || u.Records != 2 || u.Bytes <= 0 {
			t.Fatalf("restored usage %+v %v", u, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.writer.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts,rank) VALUES('integrity-check',1)`); err != nil {
		t.Fatal(err)
	}
	// Triggers, reference checks and cleanup still work after restore.
	if err := restored.Update(ctx, func(tx store.Tx) error { return tx.Memory().Delete(ctx, target.Scope, target.ID, target.Version) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	var n int
	if err := restored.Update(ctx, func(tx store.Tx) error {
		var err error
		n, err = tx.Memory().Prune(ctx, "owner", time.Now().Add(time.Hour), 10)
		return err
	}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := restored.Update(ctx, func(tx store.Tx) error { return tx.Memory().Delete(ctx, target.Scope, target.ID, target.Version) }); err != nil {
		t.Fatal(err)
	}
}
