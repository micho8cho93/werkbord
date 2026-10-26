package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// A deleted memory's text must not remain readable in the database file after the
// WAL is checkpointed (audit: residual copies). The marker is a single FTS token.
func TestAuditDeletedMemoryLeavesNoReadableResidue(t *testing.T) {
	db, path := openTemp(t)
	ctx := context.Background()
	const marker = "zebracorn7731marker"
	m := memoryRecord(t, "Deploy uses "+marker+" for the staging cluster")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, m) })
	other := memoryRecord(t, "Unrelated note about caching")
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Create(ctx, other) })
	mustUpdate(t, db, func(tx store.Tx) error { return tx.Memory().Delete(ctx, m.Scope, m.ID, m.Version) })
	if _, err := db.pool.Writer.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(marker)) {
		t.Fatalf("deleted memory text is still readable in the database file")
	}
}

// A database written by Phase 1B (schema 18) must upgrade through the knowledge,
// sharing and publication migrations without losing a record, its search index or
// its freshness checkpoint, and must pass SQLite's own integrity checks afterwards.
func TestAuditUpgradeFromPhase1BKeepsMemoryAndIndex(t *testing.T) {
	path := oldDatabase(t, 18)
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		state, verified := "needs_verification", "NULL"
		if i%5 == 0 {
			state, verified = "current", "3"
		}
		if _, err = raw.Exec(`INSERT INTO memories(id,owner_id,project_id,repository,type,authority,source_type,provenance,summary,details,state,fingerprint,schema_version,version,created_at,updated_at,verified_at,record_bytes)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,1,1,1,3,`+verified+`,200)`,
			fmt.Sprintf("mem_%d", i), "individual", "prj_1", "", "decision", "factual_observation", "import", `{"type":"import","reference":"fixture"}`,
			fmt.Sprintf("legacy decision %d about caching", i), "{}", state, fmt.Sprintf("%064d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = raw.Exec(`INSERT INTO memory_freshness(owner_id,project_id,repository,checkpoint,version) VALUES('individual','prj_1','','{}',1)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	db, err := Open(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	scope := domain.MemoryScope{OwnerID: "individual", ProjectID: "prj_1"}
	if err = db.View(ctx, func(tx store.Tx) error {
		got, e := tx.Memory().KnowledgeList(ctx, scope, domain.MemoryQuery{Text: "caching", Limit: 100}, false)
		if e != nil || len(got) != 50 {
			t.Fatalf("legacy records after upgrade: %d %v", len(got), e)
		}
		for _, m := range got {
			if m.Pinned || m.Sensitivity != domain.MemoryPrivate {
				t.Fatalf("upgrade changed defaults: %+v", m)
			}
		}
		if f, e := tx.Memory().Freshness(ctx, scope); e != nil || f == nil {
			t.Fatalf("freshness checkpoint lost: %v", e)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// New-phase features work on upgraded rows: pin one, delete one, search still exact.
	mustUpdate(t, db, func(tx store.Tx) error {
		m, e := tx.Memory().Get(ctx, scope, "mem_1")
		if e != nil {
			return e
		}
		m.Pinned = true
		if e = tx.Memory().Update(ctx, m); e != nil {
			return e
		}
		return tx.Memory().Delete(ctx, scope, "mem_2", 1)
	})
	for _, check := range []string{`PRAGMA integrity_check`, `PRAGMA foreign_key_check`} {
		rows, e := db.pool.Writer.QueryContext(ctx, check)
		if e != nil {
			t.Fatal(e)
		}
		n := 0
		for rows.Next() {
			var first string
			_ = rows.Scan(&first)
			if check == `PRAGMA integrity_check` && first != "ok" || check != `PRAGMA integrity_check` {
				t.Fatalf("%s: %s", check, first)
			}
			n++
		}
		_ = rows.Close()
		if check == `PRAGMA integrity_check` && n != 1 {
			t.Fatalf("%s returned %d rows", check, n)
		}
	}
	if _, err = db.pool.Writer.ExecContext(ctx, `INSERT INTO memories_fts(memories_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("FTS index inconsistent after upgrade: %v", err)
	}
}
