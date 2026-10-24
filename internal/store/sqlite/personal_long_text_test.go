package sqlite

import (
	"encoding/json"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// A database from before migration 0028 bounds a record at 32 KiB. Opening it widens the bound to 64 KiB and keeps every
// record, its index and the triggers that count a collection's changes.
func TestPersonalRecordBoundWidensWithoutLosingRecords(t *testing.T) {
	db, path := openTemp(t)
	collection, _, record := seedPersonal(t, db)
	for _, statement := range []string{
		`DROP TABLE personal_records`,
		`CREATE TABLE personal_records (
 id TEXT PRIMARY KEY CHECK (length(id) = 21 AND substr(id, 1, 5) = 'prec_' AND substr(id, 6) NOT GLOB '*[^a-z2-7]*'),
 collection_id TEXT NOT NULL REFERENCES personal_collections(id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL CHECK (revision > 0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK (updated_at >= created_at),
 document TEXT NOT NULL CHECK (json_valid(document) AND json_type(document) = 'object' AND length(CAST(document AS BLOB)) <= 32768)
) STRICT`,
		`CREATE INDEX personal_records_collection ON personal_records(collection_id, id)`,
		`DELETE FROM schema_migrations WHERE version=28`,
	} {
		if _, err := db.writer.ExecContext(ctx, statement); err != nil {
			t.Fatal(statement, err)
		}
	}
	if _, err := db.writer.ExecContext(ctx, `INSERT INTO personal_records(id, collection_id, revision, created_at, updated_at, document) VALUES(?, ?, 1, 1, 1, ?)`, record.ID, collection.ID, `{"title":"Durable"}`); err != nil {
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
	long := &domain.PersonalRecord{PersonalMeta: domain.PersonalMeta{ID: domain.NewID(domain.PrefixPersonalRecord), CreatedAt: now(), UpdatedAt: now()}, CollectionID: collection.ID, Values: map[string]json.RawMessage{"title": json.RawMessage(`"` + strings.Repeat("x", 50000) + `"`)}}
	var generation int64
	if err := upgraded.Update(ctx, func(tx store.Tx) error {
		before, err := tx.Personal().CollectionUsage(ctx, collection.ID)
		if err != nil {
			return err
		}
		generation = before.Generation
		return tx.Personal().CreateRecord(ctx, long)
	}); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.View(ctx, func(tx store.Tx) error {
		if got, err := tx.Personal().GetRecord(ctx, collection.ID, record.ID); err != nil || string(got.Values["title"]) != `"Durable"` {
			t.Fatal("the saved record was lost", got, err)
		}
		if _, err := tx.Personal().GetRecord(ctx, collection.ID, long.ID); err != nil {
			t.Fatal("the long record was not saved", err)
		}
		after, err := tx.Personal().CollectionUsage(ctx, collection.ID)
		if err != nil || after.Generation != generation+1 || after.RecordCount != 2 {
			t.Fatalf("the change trigger did not count the new record: %+v %v", after, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var indexes int
	if err := upgraded.writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='personal_records_collection'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatal("the collection index is missing", indexes, err)
	}
	over := &domain.PersonalRecord{PersonalMeta: domain.PersonalMeta{ID: domain.NewID(domain.PrefixPersonalRecord), CreatedAt: now(), UpdatedAt: now()}, CollectionID: collection.ID, Values: map[string]json.RawMessage{"title": json.RawMessage(`"` + strings.Repeat("x", 70000) + `"`)}}
	if err := upgraded.Update(ctx, func(tx store.Tx) error { return tx.Personal().CreateRecord(ctx, over) }); err == nil {
		t.Fatal("a record beyond 64 KiB was stored")
	}
}
