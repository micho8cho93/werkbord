package sqlite

import (
	"encoding/json"
	"reflect"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func TestPersonalLibraryIconUpgradePreservesRecordsAndHistory(t *testing.T) {
	db, path := openTemp(t)
	collection, workspace, record := seedPersonal(t, db)
	var stateBefore *domain.PersonalCollectionUsage
	// Simulate an existing v26 database with an old pixel identity.
	if err := db.Update(ctx, func(tx store.Tx) error {
		w, err := tx.Personal().GetWorkspace(ctx, workspace.ID)
		if err != nil {
			return err
		}
		w.Icon = "workspace-1297741"
		if err := tx.Personal().UpdateWorkspace(ctx, w); err != nil {
			return err
		}
		workspace = w
		// Reserve the first semantic match with a different, hidden workspace.
		reserved := &domain.PersonalWorkspace{PersonalMeta: domain.PersonalMeta{ID: domain.NewID(domain.PrefixPersonalWorkspace), CreatedAt: now(), UpdatedAt: now()}, PersonalWorkspaceSpec: domain.PersonalWorkspaceSpec{Name: "Other", Icon: domain.PersonalIconCandidates(w.Name, "")[0], UISchemaVersion: 1, Definition: domain.PersonalUI{Layout: domain.PersonalNode{Type: "text", Text: "Keep this"}}}}
		if err := tx.Personal().CreateWorkspace(ctx, reserved); err != nil {
			return err
		}
		stateBefore, err = tx.Personal().CollectionUsage(ctx, collection.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `DROP INDEX personal_workspace_library_icon_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `ALTER TABLE assistant_sessions DROP COLUMN selection_mode`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `ALTER TABLE assistant_sessions DROP COLUMN selection_json`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `DROP TABLE device_operations`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"product_map_jobs", "product_map_snapshots"} {
		if _, err := db.writer.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.writer.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version>=27`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var saved *domain.PersonalWorkspace
	if err := upgraded.View(ctx, func(tx store.Tx) error {
		var err error
		saved, err = tx.Personal().GetWorkspace(ctx, workspace.ID)
		if err != nil {
			return err
		}
		if !domain.LibraryPersonalIcon(saved.Icon) || saved.Icon == domain.PersonalIconCandidates(workspace.Name, "")[0] || saved.Revision != workspace.Revision+1 {
			t.Fatalf("upgrade didn't allocate a fresh icon: %+v", saved)
		}
		original := workspace.PersonalWorkspaceSpec
		original.Icon = saved.Icon
		if !reflect.DeepEqual(saved.PersonalWorkspaceSpec, original) {
			t.Fatal("upgrade changed workspace contents")
		}
		usage, err := tx.Personal().CollectionUsage(ctx, collection.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(usage, stateBefore) {
			t.Fatal("upgrade changed ownership, references or collection generation")
		}
		got, err := tx.Personal().GetRecord(ctx, collection.ID, record.ID)
		if err != nil {
			return err
		}
		beforeJSON, _ := json.Marshal(record)
		afterJSON, _ := json.Marshal(got)
		if string(beforeJSON) != string(afterJSON) {
			t.Fatal("upgrade altered personal records")
		}
		history, err := tx.Personal().WorkspaceHistory(ctx, workspace.ID)
		if err != nil {
			return err
		}
		if len(history) != 3 {
			t.Fatalf("expected original, pixel and library revisions; got %d", len(history))
		}
		old, err := tx.Personal().WorkspaceRevision(ctx, workspace.ID, workspace.Revision)
		if err != nil {
			return err
		}
		if old.Icon != "workspace-1297741" {
			t.Fatal("upgrade replaced old definition history")
		}
		// The SQL index is the last line of defence against bypassing services.
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.upgradePersonalIcons(ctx); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.View(ctx, func(tx store.Tx) error {
		w, err := tx.Personal().GetWorkspace(ctx, workspace.ID)
		if err == nil && (w.Icon != saved.Icon || w.Revision != saved.Revision) {
			t.Fatal("second upgrade changed a saved library choice")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Update(ctx, func(tx store.Tx) error {
		clone := *saved
		clone.ID = domain.NewID(domain.PrefixPersonalWorkspace)
		return tx.Personal().CreateWorkspace(ctx, &clone)
	}); err == nil {
		t.Fatal("unique library icon index allowed a duplicate")
	}
}
