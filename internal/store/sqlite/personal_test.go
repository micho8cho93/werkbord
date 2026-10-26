package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func seedPersonal(t *testing.T, db *DB) (*domain.PersonalCollection, *domain.PersonalWorkspace, *domain.PersonalRecord) {
	t.Helper()
	at := now()
	meta := func(prefix string) domain.PersonalMeta {
		return domain.PersonalMeta{ID: domain.NewID(prefix), CreatedAt: at, UpdatedAt: at}
	}
	c := &domain.PersonalCollection{PersonalMeta: meta(domain.PrefixPersonalCollection), PersonalCollectionSpec: domain.PersonalCollectionSpec{Name: "Campaign", SchemaVersion: 1, Fields: []domain.PersonalField{{Key: "title", Name: "Title", Type: "text"}}}}
	w := &domain.PersonalWorkspace{PersonalMeta: meta(domain.PrefixPersonalWorkspace), PersonalWorkspaceSpec: domain.PersonalWorkspaceSpec{Name: "Campaign", Icon: "lucide-kanban", UISchemaVersion: 1, CollectionIDs: []string{c.ID}, Visibility: domain.PersonalVisibility{Individual: true}, Definition: domain.PersonalUI{Layout: domain.PersonalNode{Type: "heading", Text: "Campaign"}}}}
	r := &domain.PersonalRecord{PersonalMeta: meta(domain.PrefixPersonalRecord), CollectionID: c.ID, Values: map[string]json.RawMessage{"title": json.RawMessage(`"Durable"`)}}
	if err := db.Update(context.Background(), func(tx store.Tx) error {
		if e := tx.Personal().CreateCollection(ctx, c); e != nil {
			return e
		}
		if e := tx.Personal().CreateWorkspace(ctx, w); e != nil {
			return e
		}
		return tx.Personal().CreateRecord(ctx, r)
	}); err != nil {
		t.Fatal(err)
	}
	return c, w, r
}
func TestPersonalRestartAndSnapshotRestore(t *testing.T) {
	db, path := openTemp(t)
	c, w, r := seedPersonal(t, db)
	backup := filepath.Join(t.TempDir(), "snapshot.db")
	if _, err := db.writer.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	verify := func(db *DB) {
		t.Helper()
		if err := db.View(ctx, func(tx store.Tx) error {
			got, e := tx.Personal().GetWorkspace(ctx, w.ID)
			if e != nil || got.UISchemaVersion != 1 || got.CollectionIDs[0] != c.ID || got.Revision != 1 || got.CreatedAt != w.CreatedAt {
				t.Fatal(got, e)
			}
			usage, e := tx.Personal().CollectionUsage(ctx, c.ID)
			if e != nil || usage.OwnerWorkspaceID != w.ID || usage.ReferenceCount != 1 || usage.RecordCount != 1 {
				t.Fatal("backup lost lifecycle metadata", usage, e)
			}
			history, e := tx.Personal().WorkspaceHistory(ctx, w.ID)
			if e != nil || len(history) != 1 {
				t.Fatal("backup lost history", history, e)
			}
			collection, e := tx.Personal().GetCollection(ctx, c.ID)
			if e != nil || collection.Fields[0].Key != "title" {
				t.Fatal(collection, e)
			}
			record, e := tx.Personal().GetRecord(ctx, c.ID, r.ID)
			if e != nil || string(record.Values["title"]) != `"Durable"` || record.Revision != 1 {
				t.Fatal(record, e)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	verify(reopened)
	if err := reopened.Update(ctx, func(tx store.Tx) error {
		if e := tx.Personal().DeleteWorkspace(ctx, w.ID, 1); e != nil {
			return e
		}
		if e := tx.Personal().DeleteRecord(ctx, c.ID, r.ID, 1); e != nil {
			return e
		}
		return tx.Personal().DeleteCollection(ctx, c.ID, 1)
	}); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, backup, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	verify(restored)
	// The restored relationship still protects the collection.
	if err = restored.Update(ctx, func(tx store.Tx) error { return tx.Personal().DeleteCollection(ctx, c.ID, 1) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
}
func TestPersonalMigrationFromPreviousSchema(t *testing.T) {
	ms := mustMigrations(t)
	path := oldDatabase(t, len(ms)-1)
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedPersonal(t, db)
	if err = db.View(ctx, func(tx store.Tx) error {
		p, e := tx.Projects().Get(ctx, "prj_1")
		if e != nil || p.Name != "keep me" {
			t.Fatal(p, e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(BackupDir(path))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	info, err := Inspect(ctx, filepath.Join(BackupDir(path), files[0].Name()))
	if err != nil || info.Version != len(ms)-1 || info.Integrity != "ok" {
		t.Fatal(info, err)
	}
	// Restoring that pre-upgrade backup migrates forward and preserves old rows.
	backup, err := Open(ctx, filepath.Join(BackupDir(path), files[0].Name()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if err = backup.View(ctx, func(tx store.Tx) error {
		cs, e := tx.Personal().ListCollections(ctx, domain.PersonalQuery{Limit: 10})
		if e != nil || len(cs) != 0 {
			t.Fatal(cs, e)
		}
		_, e = tx.Projects().Get(ctx, "prj_1")
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPersonalRepositoryCASAndTransactionRollback(t *testing.T) {
	db, _ := openTemp(t)
	c, w, r := seedPersonal(t, db)
	stale := *r
	r.Values = map[string]json.RawMessage{"title": json.RawMessage(`"New"`)}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Personal().UpdateRecord(ctx, r) }); err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Personal().UpdateRecord(ctx, &stale) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Personal().DeleteRecord(ctx, c.ID, r.ID, 1) }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	// Failed reference insertion rolls back both the definition and its revision.
	changed := *w
	changed.Name = "Must roll back"
	changed.CollectionIDs = []string{domain.NewID(domain.PrefixPersonalCollection)}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.Personal().UpdateWorkspace(ctx, &changed) }); err == nil {
		t.Fatal("missing reference accepted")
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		got, e := tx.Personal().GetWorkspace(ctx, w.ID)
		if e != nil || got.Revision != 1 || got.Name != w.Name {
			t.Fatal(got, e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []domain.PersonalQuery{{Limit: -1}, {Limit: 101}} {
		if err := db.View(ctx, func(tx store.Tx) error { _, e := tx.Personal().ListRecords(ctx, c.ID, q); return e }); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	// SQL integrity remains a backstop for invalid IDs, JSON, sizes and references.
	for _, args := range [][]any{{"tsk_abcdefghijklmnop", 1, 1, 1, `{}`}, {domain.NewID("pws"), 0, 1, 1, `{}`}, {domain.NewID("pws"), 1, 1, 1, `[]`}, {domain.NewID("pws"), 1, 1, 1, `not JSON`}} {
		if _, e := db.writer.ExecContext(ctx, `INSERT INTO personal_workspaces(id,revision,created_at,updated_at,document) VALUES(?,?,?,?,?)`, args...); e == nil {
			t.Fatal(args)
		}
	}
}

func TestPersonalLifecycleUpgradeBackfillAndBackup(t *testing.T) {
	path := oldDatabase(t, 23)
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	repo := personalRepo{raw}
	at := now()
	makeCollection := func() *domain.PersonalCollection {
		c := &domain.PersonalCollection{PersonalMeta: domain.PersonalMeta{ID: domain.NewID("pcl"), CreatedAt: at, UpdatedAt: at}, PersonalCollectionSpec: domain.PersonalCollectionSpec{Name: "Retained", SchemaVersion: 1, Fields: []domain.PersonalField{{Key: "title", Name: "Title", Type: "text"}}}}
		if err := repo.CreateCollection(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	makeWorkspace := func(cid string) *domain.PersonalWorkspace {
		w := &domain.PersonalWorkspace{PersonalMeta: domain.PersonalMeta{ID: domain.NewID("pws"), CreatedAt: at, UpdatedAt: at}, PersonalWorkspaceSpec: domain.PersonalWorkspaceSpec{Name: "Saved", Icon: "folder", UISchemaVersion: 1, CollectionIDs: []string{cid}, Visibility: domain.PersonalVisibility{Universal: true}, Definition: domain.PersonalUI{Layout: domain.PersonalNode{Type: "heading", Text: "Saved"}}}}
		if err := repo.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
		return w
	}
	sole := makeCollection()
	owner := makeWorkspace(sole.ID)
	shared := makeCollection()
	makeWorkspace(shared.ID)
	makeWorkspace(shared.ID)
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	verify := func(db *DB) {
		t.Helper()
		if err := db.View(ctx, func(tx store.Tx) error {
			u, err := tx.Personal().CollectionUsage(ctx, sole.ID)
			if err != nil || u.OwnerWorkspaceID != owner.ID || u.ReferenceCount != 1 {
				t.Fatal(u, err)
			}
			u, err = tx.Personal().CollectionUsage(ctx, shared.ID)
			if err != nil || u.OwnerWorkspaceID != "" || u.ReferenceCount != 2 {
				t.Fatal("ambiguous ownership invented", u, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	upgraded, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	verify(upgraded)
	files, err := os.ReadDir(BackupDir(path))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	restored, err := Open(ctx, filepath.Join(BackupDir(path), files[0].Name()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	verify(restored)
}
