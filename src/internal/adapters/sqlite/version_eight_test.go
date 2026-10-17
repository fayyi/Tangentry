package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestPDFAnchorMigrationPreservesArticleAnchorsAndAllLegacyColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-seven.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version(version INTEGER PRIMARY KEY)`, `INSERT INTO schema_version VALUES (7)`,
		`CREATE TABLE notes(id TEXT PRIMARY KEY,content_id TEXT NOT NULL,body TEXT NOT NULL,position_seconds REAL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,text_anchor TEXT)`,
		`INSERT INTO notes VALUES('old','rss:abc','原有文章 Memo',12.75,123,124,'{"quote":"原文","start":2,"end":4,"prefix":"","suffix":"","sourceHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var before string
	const query = `SELECT json_array(id,content_id,body,position_seconds,created_at,updated_at,text_anchor) FROM notes`
	if err := db.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var after string
		var anchor sql.NullString
		var version int
		if err := s.db.QueryRow(query).Scan(&after); err != nil || after != before {
			t.Fatal("legacy columns changed", after, err)
		}
		if err := s.db.QueryRow(`SELECT pdf_anchor FROM notes`).Scan(&anchor); err != nil || anchor.Valid {
			t.Fatal("migration invented an anchor", err)
		}
		if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version != latestVersion(t) {
			t.Fatal("schema did not upgrade", err)
		}
		s.Close()
	}
}
