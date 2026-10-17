package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"tangentry/internal/content"
)

func latestVersion(t *testing.T) int {
	t.Helper()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return migrations[len(migrations)-1].version
}

// testdata/v22.sql is a dump of a database written by the release that last
// used schema version 22, with representative rows.
func TestVersion22DatabaseUpgradesWithoutLoss(t *testing.T) {
	dump, err := os.ReadFile(filepath.Join("testdata", "v22.sql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "v22.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(dump)); err != nil {
		t.Fatal(err)
	}
	const notes = `SELECT json_group_array(json_array(id,content_id,body,position_seconds,created_at,updated_at,text_anchor,pdf_anchor,tags,pinned,subtitle_references,frame)) FROM notes`
	snapshots := map[string]string{
		"saved":    `SELECT json_group_array(json_array(id,payload,saved_at)) FROM saved_content`,
		"notes":    notes,
		"frames":   `SELECT json_group_array(json_array(note_id,frame_id,hex(data))) FROM note_frames`,
		"progress": `SELECT json_group_array(json_array(content_id,position_seconds,duration_seconds,completed,revision,updated_at)) FROM reading_progress`,
		"read":     `SELECT json_group_array(json_array(content_id,is_read,updated_at)) FROM read_state`,
		"articles": `SELECT json_group_array(json_array(content_id,payload,revision)) FROM article_documents`,
	}
	before := map[string]string{}
	for name, query := range snapshots {
		var value string
		if err := db.QueryRow(query).Scan(&value); err != nil {
			t.Fatal(name, err)
		}
		before[name] = value
	}
	db.Close()
	for open := 0; open < 2; open++ {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for name, query := range snapshots {
			var value string
			if err := store.db.QueryRow(query).Scan(&value); err != nil || value != before[name] {
				t.Fatalf("%s changed: %v\n%s\n%s", name, err, before[name], value)
			}
		}
		var version, recorded int
		if err := store.db.QueryRow(`SELECT MAX(version), count(*) FROM schema_version`).Scan(&version, &recorded); err != nil || version != latestVersion(t) {
			t.Fatal("schema did not reach latest version", version, err)
		}
		// Version 23 consolidated the legacy recommendation account.
		var personal int
		if err := store.db.QueryRow(`SELECT count(*) FROM recommendation_queue WHERE account='0000000000000000000000000000000000000000000000000000000000000000'`).Scan(&personal); err != nil || personal != 1 {
			t.Fatal("recommendation queue not consolidated", personal, err)
		}
		// Existing saved items are searchable after the upgrade.
		page, err := store.Page(context.Background(), content.Query{Q: "Existing"})
		if err != nil || page.Total != 1 || page.Items[0].ID != "bilibili:BV17x411w7KC" {
			t.Fatalf("existing library not indexed: %+v %v", page, err)
		}
		for _, table := range []string{"ai_result", "viewing_history", "search_history"} {
			var count int
			if err := store.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
				t.Fatal("new table missing", table, err)
			}
		}
		store.Close()
	}
}

func TestMigrationFilesAreOrderedAndUnique(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version <= migrations[i-1].version {
			t.Fatal("migrations out of order", migrations[i].name)
		}
	}
}

func TestFreshDatabaseRecordsEveryMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	for open := 0; open < 2; open++ {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		migrations, _ := loadMigrations()
		var count int
		if err := store.db.QueryRow(`SELECT count(*) FROM schema_version`).Scan(&count); err != nil || count != len(migrations) {
			t.Fatal("unexpected recorded versions", count, err)
		}
		store.Close()
	}
}
