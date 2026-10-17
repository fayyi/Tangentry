package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestTextAnchorMigrationIsIdempotentAndPreservesLegacyNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-six.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE schema_version(version INTEGER PRIMARY KEY)`, `INSERT INTO schema_version VALUES (6)`,
		`CREATE TABLE notes(id TEXT PRIMARY KEY,content_id TEXT NOT NULL,body TEXT NOT NULL,position_seconds REAL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`INSERT INTO notes VALUES('legacy','bilibili:BV17x411w7KC','原有 Memo',12.75,123,124)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var row string
		err = s.db.QueryRow(`SELECT json_array(id,content_id,body,position_seconds,created_at,updated_at,text_anchor) FROM notes`).Scan(&row)
		if err != nil || row != `["legacy","bilibili:BV17x411w7KC","原有 Memo",12.75,123,124,null]` {
			t.Fatal("legacy note changed", row, err)
		}
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_version WHERE version=7`).Scan(&count); err != nil || count != 1 {
			t.Fatal("migration version missing", err)
		}
		s.Close()
	}
}
