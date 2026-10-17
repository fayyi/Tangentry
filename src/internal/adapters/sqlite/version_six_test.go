package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestVersionSixPreservesEveryVersionFiveTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-five.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_version(version INTEGER PRIMARY KEY)`, `INSERT INTO schema_version VALUES(1),(2),(3),(4),(5)`,
		`CREATE TABLE saved_content(id TEXT PRIMARY KEY,payload TEXT NOT NULL,saved_at INTEGER NOT NULL)`,
		`CREATE TABLE downloads(id TEXT PRIMARY KEY,payload TEXT NOT NULL,created_at INTEGER NOT NULL)`,
		`CREATE TABLE notes(id TEXT PRIMARY KEY,content_id TEXT NOT NULL,body TEXT NOT NULL,position_seconds REAL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`CREATE TABLE subscriptions(id TEXT PRIMARY KEY,creator_id TEXT NOT NULL UNIQUE,name TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL DEFAULT 0,snapshot TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE reading_progress(content_id TEXT PRIMARY KEY,position_seconds REAL NOT NULL,duration_seconds REAL NOT NULL,completed INTEGER NOT NULL,revision INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`INSERT INTO saved_content VALUES('bilibili:BV17x411w7KC','{"title":"existing collection"}',123)`,
		`INSERT INTO downloads VALUES('legacy-job','{"state":"completed"}',124)`,
		`INSERT INTO notes VALUES('legacy-note','bilibili:BV17x411w7KC','existing memo',12.3,125,126)`,
		`INSERT INTO subscriptions VALUES('bilibili:creator:12345','12345','existing source',127,128,'{"items":[]}')`,
		`INSERT INTO reading_progress VALUES('bilibili:BV17x411w7KC',12.3,99.5,0,7,129)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	queries := []string{
		`SELECT json_array(id,payload,saved_at) FROM saved_content`,
		`SELECT json_array(id,payload,created_at) FROM downloads`,
		`SELECT json_array(id,content_id,body,position_seconds,created_at,updated_at) FROM notes`,
		`SELECT json_array(id,creator_id,name,created_at,updated_at,snapshot) FROM subscriptions`,
		`SELECT json_array(content_id,position_seconds,duration_seconds,completed,revision,updated_at) FROM reading_progress`,
	}
	before := make([]string, len(queries))
	for i, q := range queries {
		if err := db.QueryRow(q).Scan(&before[i]); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, q := range queries {
		var after string
		if err := store.db.QueryRow(q).Scan(&after); err != nil || after != before[i] {
			t.Fatalf("existing table changed: %s %v", q, err)
		}
	}
	var version, count int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version != latestVersion(t) {
		t.Fatal("latest schema missing")
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM saved_subtitles`).Scan(&count); err != nil || count != 0 {
		t.Fatal("new subtitle table invalid")
	}
}
