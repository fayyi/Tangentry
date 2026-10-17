package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/notes"
)

func TestNotesUpgradePreservesExistingLibraryAndDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`,
		`INSERT INTO schema_version VALUES (1), (2)`,
		`CREATE TABLE saved_content (id TEXT PRIMARY KEY, payload TEXT NOT NULL, saved_at INTEGER NOT NULL)`,
		`CREATE TABLE downloads (id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	item := content.Item{ID: "bilibili:BV17x411w7KC", PlatformID: "BV17x411w7KC", Platform: "bilibili", Kind: "video", Title: "existing library", URL: "https://www.bilibili.com/video/BV17x411w7KC/", SavedAt: 123}
	job := downloads.Job{ID: "existing-download", URL: item.URL, State: "completed", CreatedAt: 124}
	itemJSON, _ := json.Marshal(item)
	jobJSON, _ := json.Marshal(job)
	if _, err := db.Exec(`INSERT INTO saved_content VALUES (?,?,?)`, item.ID, string(itemJSON), item.SavedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO downloads VALUES (?,?,?)`, job.ID, string(jobJSON), job.CreatedAt); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: item.ID, Body: "new note"}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx)
	if err != nil || len(items) != 1 || items[0].SavedAt != 123 {
		t.Fatal("notes affected existing collection")
	}
	jobs, err := store.ListDownloads(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatal("notes affected downloads")
	}
	var version int
	if err := store.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version < 3 {
		t.Fatal("schema did not upgrade")
	}
}
