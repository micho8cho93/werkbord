package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"werkbord/internal/service"
	"werkbord/internal/store"
)

func TestGeneratedWorkspaceCleanupPreservesDeckAndCoreData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "before-removal.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate(ctx, raw, ms[:31]); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO projects (id,name,repo_path,created_at,updated_at) VALUES ('prj_keep','Keep','/repos/keep',1,1)`,
		`INSERT INTO personal_workspaces VALUES ('pws_abcdefghijklmnop',1,1,1,'{}')`,
		`INSERT INTO personal_collections VALUES ('pcl_abcdefghijklmnop',1,1,1,'{}')`,
		`INSERT INTO personal_workspace_collections VALUES ('pws_abcdefghijklmnop','pcl_abcdefghijklmnop')`,
		`INSERT INTO personal_records VALUES ('prec_abcdefghijklmnop','pcl_abcdefghijklmnop',1,1,1,'{}')`,
		`INSERT INTO settings VALUES ('phone-deck.test','{"version":7,"slots":[{"id":"work","button":"mywork"},{"id":"old","button":"pws_abcdefghijklmnop"},null,{"id":"scan","button":"find-bugs","target":"ask"},null,null,null,null,null]}',1)`,
		`INSERT INTO settings VALUES ('keep-memory-settings','{"enabled":true}',1)`,
	} {
		if _, err := raw.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.reader.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name LIKE 'personal_%'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("obsolete schema objects remain: %d, %v", tables, err)
	}
	if err := db.View(ctx, func(tx store.Tx) error {
		project, err := tx.Projects().Get(ctx, "prj_keep")
		if err != nil {
			return err
		}
		if project.Name != "Keep" {
			t.Fatal("existing project changed during cleanup")
		}
		var deck service.DeckLayout
		if err := tx.Settings().Get(ctx, "phone-deck.test", &deck); err != nil {
			return err
		}
		if deck.Version != 8 || deck.Slots[1] != nil || deck.Slots[0].Button != "mywork" || deck.Slots[3].Button != "find-bugs" || deck.Slots[3].Target != "ask" {
			t.Fatalf("retained layout changed: %+v", deck)
		}
		var kept map[string]bool
		if err := tx.Settings().Get(ctx, "keep-memory-settings", &kept); err != nil {
			return err
		}
		if !kept["enabled"] {
			t.Fatal("unrelated settings were changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seedProject(t, db)
}
