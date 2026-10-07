package replicated

import (
	"context"
	"database/sql"
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

// legacyDB makes a Team database the way it was kept before replication, with a workspace in it.
func legacyDB(t *testing.T, upTo int) (string, seeded) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team.db")
	db, err := store.Open(bg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	k := seed(t, db)
	addTickets(t, db, k, 5)
	addMember(t, db, k, "Grace")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if upTo > 0 {
		// An older database: roll the recorded version back to what a database from an older Team would say, with
		// the later migrations' changes undone by the tests that need them. Here only the version row is used.
		_ = upTo
	}
	return path, k
}

func TestALegacyDatabaseMovesIntoTheClusterAndIsVerified(t *testing.T) {
	path, k := legacyDB(t, 0)
	before, _ := os.ReadFile(path)
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	client, err := NewClient(c.Addrs(), Auth{User: rqlite.UserApp, Pass: c.Creds.App})
	if err != nil {
		t.Fatal(err)
	}
	info, err := PrepareImport(bg, path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != ms(t) || info.Legacy.Tables["tickets"] != 5+1 || info.Legacy.Tables["members"] != 2 {
		t.Fatalf("%+v", info)
	}
	if err := client.Load(bg, mustRead(t, info.File)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyImport(bg, client, info); err != nil {
		t.Fatal(err)
	}
	// The original was only read.
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		// A WAL checkpoint may touch it; its contents, not its bytes, are what must not change.
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if d, err := DigestOfFile(bg, db); err != nil || info.Legacy.Diff(d) != "" {
			t.Fatalf("the original database changed: %v %v", err, info.Legacy.Diff(d))
		}
	}
	// The store opens on what was loaded: it is at the original's position of history (the import made none),
	// holds the data, and continues the numbering of everything that counts up.
	s := open(t, c, 0)
	if v, _ := s.SchemaVersion(bg); v != ms(t) {
		t.Fatalf("schema %d", v)
	}
	if got := ticketOf(t, s, k); got.Title != "First" {
		t.Fatalf("%+v", got)
	}
	var nextNumber int
	_ = s.View(bg, func(tx store.Tx) (err error) { nextNumber, err = tx.NextTicketNumber(bg, k.ws); return })
	if nextNumber != 7 {
		t.Fatalf("the next ticket number is %d, want 7", nextNumber)
	}
	// And writing continues.
	addMember(t, s, k, "Hedy")
	if _, _, err := s.Verify(bg); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An import that does not match the original is caught: a row changed after the load, a row missing.
func TestVerifyImportCatchesWhatIsNotTheOriginal(t *testing.T) {
	path, k := legacyDB(t, 0)
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 1})
	client, _ := NewClient(c.Addrs(), Auth{User: rqlite.UserApp, Pass: c.Creds.App})
	info, err := PrepareImport(bg, path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Load(bg, mustRead(t, info.File)); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, out, err := client.Execute(bg, []Stmt{{SQL: q, Args: args}}); err != nil || out != OutcomeCommitted {
			t.Fatal(out, err)
		}
	}
	exec(`UPDATE members SET name = 'Mallory' WHERE id = ?`, k.owner)
	if err := VerifyImport(bg, client, info); err == nil || !strings.Contains(err.Error(), "members") {
		t.Fatalf("a changed row was not noticed: %v", err)
	}
	exec(`UPDATE members SET name = 'Ada' WHERE id = ?`, k.owner)
	if err := VerifyImport(bg, client, info); err != nil {
		t.Fatalf("after putting it back: %v", err)
	}
	exec(`DELETE FROM tickets WHERE number = 3`)
	if err := VerifyImport(bg, client, info); err == nil || !strings.Contains(err.Error(), "tickets") {
		t.Fatalf("a missing row was not noticed: %v", err)
	}
}

func TestOnlyATeamDatabaseCanBePrepared(t *testing.T) {
	dir := t.TempDir()
	if _, err := PrepareImport(bg, filepath.Join(dir, "nothing.db"), t.TempDir()); err == nil {
		t.Error("a file that is not there was prepared")
	}
	other := filepath.Join(dir, "other.db")
	db, err := sql.Open("sqlite", other)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`CREATE TABLE x (a INTEGER)`)
	db.Close()
	if _, err := PrepareImport(bg, other, t.TempDir()); err == nil {
		t.Error("another program's database was prepared")
	}
	garbage := filepath.Join(dir, "garbage.db")
	_ = os.WriteFile(garbage, []byte("this is not sqlite at all, but it is long enough to be mistaken for a header"), 0o600)
	if _, err := PrepareImport(bg, garbage, t.TempDir()); err == nil {
		t.Error("garbage was prepared")
	}
	// A database from a newer Team is refused, by name, and not touched.
	path, _ := legacyDB(t, 0)
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, 'future', 0)`, ms(t)+1); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := PrepareImport(bg, path, t.TempDir()); err == nil || !strings.Contains(err.Error(), "upgrade werkbord-team") {
		t.Errorf("a newer database: %v", err)
	}
	_ = domain.ErrConflict
	_ = context.Background
	_ = time.Now
}
