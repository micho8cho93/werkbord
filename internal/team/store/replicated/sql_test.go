package replicated

import (
	"reflect"
	"strings"
	"testing"

	"devboard/internal/team/store"
)

func TestSplittingAScript(t *testing.T) {
	cases := map[string][]string{
		"two statements":          {"CREATE TABLE a (x INTEGER); INSERT INTO a VALUES (1);", "CREATE TABLE a (x INTEGER)", "INSERT INTO a VALUES (1)"},
		"no final semicolon":      {"SELECT 1", "SELECT 1"},
		"a semicolon in a string": {"INSERT INTO a VALUES ('a;b'); SELECT 2;", "INSERT INTO a VALUES ('a;b')", "SELECT 2"},
		"a doubled quote":         {"INSERT INTO a VALUES ('it''s;'); SELECT 3;", "INSERT INTO a VALUES ('it''s;')", "SELECT 3"},
		"comments are dropped":    {"-- a note; with a semicolon\nSELECT 1; /* and; another */ SELECT 2;", "SELECT 1", "SELECT 2"},
		"a trigger": {"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET n = n + 1; UPDATE c SET m = 1; END; SELECT 9;",
			"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET n = n + 1; UPDATE c SET m = 1; END", "SELECT 9"},
		"a trigger with a CASE": {"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET n = CASE WHEN x THEN 1 ELSE 2 END; END; SELECT 1;",
			"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET n = CASE WHEN x THEN 1 ELSE 2 END; END", "SELECT 1"},
		"BEGIN in a name is not a body": {"CREATE TABLE begin_x (a INTEGER); SELECT 1;", "CREATE TABLE begin_x (a INTEGER)", "SELECT 1"},
		"empty":                         {"  \n -- nothing\n"},
	}
	for name, c := range cases {
		script := c[0]
		want := c[1:]
		got := splitScript(script)
		for i := range got {
			got[i] = strings.Join(strings.Fields(got[i]), " ")
		}
		for i := range want {
			want[i] = strings.Join(strings.Fields(want[i]), " ")
		}
		if len(want) == 0 {
			want = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

// Every one of Team's own migrations splits into statements that SQLite accepts one at a time, in order,
// and leave the schema the migration's own text would.
func TestEveryTeamMigrationSplitsAndAppliesStatementByStatement(t *testing.T) {
	ms, err := store.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	whole, split := memDB(t), memDB(t)
	for _, m := range ms {
		if strings.Contains(m.SQL, "migrate:foreign-keys-off") {
			t.Fatalf("migration %d needs foreign keys off, which a replicated migration cannot do", m.Version)
		}
		if _, err := whole.Exec(m.SQL); err != nil {
			t.Fatalf("whole %d: %v", m.Version, err)
		}
		stmts := splitScript(m.SQL)
		if len(stmts) == 0 {
			t.Fatalf("migration %d has no statements", m.Version)
		}
		for _, s := range stmts {
			if classify(s) != kindWrite {
				t.Fatalf("migration %d: %q is not a write", m.Version, s)
			}
			if _, err := split.Exec(s); err != nil {
				t.Fatalf("migration %d: %q: %v", m.Version, s, err)
			}
		}
	}
	if a, b := schemaOf(t, whole), schemaOf(t, split); a != b {
		la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
		inB := map[string]bool{}
		for _, l := range lb {
			inB[l] = true
		}
		for _, l := range la {
			if !inB[l] {
				t.Errorf("only in the whole: %s", l)
			}
		}
		inA := map[string]bool{}
		for _, l := range la {
			inA[l] = true
		}
		for _, l := range lb {
			if !inA[l] {
				t.Errorf("only in the split: %s", l)
			}
		}
		t.Fatal("splitting changed the schema")
	}
}

func TestClassifyingStatements(t *testing.T) {
	for sql, want := range map[string]kind{
		"SELECT 1": kindRead, "  select 1": kindRead, "-- hi\nSELECT 1": kindRead, "/* x */ select 1": kindRead, "(SELECT 1)": kindRead,
		"INSERT INTO a VALUES (1)": kindWrite, "update a set x=1": kindWrite, "DELETE FROM a": kindWrite, "REPLACE INTO a VALUES (1)": kindWrite,
		"CREATE TABLE a (x)": kindWrite, "DROP TABLE a": kindWrite, "ALTER TABLE a ADD COLUMN y": kindWrite,
		"WITH x AS (SELECT 1) SELECT * FROM x": kindRead, "WITH x AS (SELECT 1) DELETE FROM a WHERE id IN x": kindWrite,
		"PRAGMA foreign_keys = OFF": kindForbidden, "BEGIN": kindForbidden, "COMMIT": kindForbidden, "ATTACH DATABASE 'x' AS y": kindForbidden, "VACUUM": kindForbidden, "": kindForbidden,
	} {
		if got := classify(sql); got != want {
			t.Errorf("%q: %d, want %d", sql, got, want)
		}
	}
}

func TestArgumentsAreCarriedExactly(t *testing.T) {
	stmts := []Stmt{{SQL: "INSERT INTO a VALUES (?, ?, ?, ?, ?)", Args: []any{nil, int64(1700000000000), "text; with 'quotes'", []byte{0, 1, 2, 255}, int64(-5)}}, {SQL: "DELETE FROM a"}}
	enc, err := encodeStmts(stmts)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := decodeStmts(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dec, stmts) {
		t.Fatalf("%+v != %+v", dec, stmts)
	}
	// Arguments are normalised the way database/sql does it, and what cannot be sent exactly is refused.
	got, err := normalize([]any{true, false, 7, "s", []byte("b"), nil, int32(3), float64(4)})
	if err != nil || !reflect.DeepEqual(got, []any{int64(1), int64(0), int64(7), "s", []byte("b"), nil, int64(3), int64(4)}) {
		t.Fatalf("%v %v", got, err)
	}
	for name, bad := range map[string]any{"a huge integer": int64(1 << 60), "a fraction": 1.5, "a struct": struct{}{}} {
		if _, err := normalize([]any{bad}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A BLOB goes to rqlite as an array of bytes, which is what makes it a blob there.
	w := Stmt{SQL: "x", Args: []any{[]byte{104, 105}, "s"}}.wire()
	if !reflect.DeepEqual(w, []any{"x", []int{104, 105}, "s"}) {
		t.Fatalf("%v", w)
	}
}
