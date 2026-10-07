package replicated

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func schemaOf(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		b.WriteString(typ + " " + name + ": " + strings.Join(strings.Fields(sqlComment.ReplaceAllString(sqlText, "")), " ") + "\n")
	}
	return b.String()
}

// a migration's comments are not part of the schema: splitting drops them.
var sqlComment = regexp.MustCompile(`--[^\n]*`)
