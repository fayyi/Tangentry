package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tangentry/internal/backup"
)

func TestSnapshotCollectsDocumentsAndHonorsDownloadSwitch(t *testing.T) {
	store := inboxTestStore(t)
	root := filepath.Dir(store.path)
	if err := os.MkdirAll(filepath.Join(root, "credentials"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "credentials", "backup-targets.json"), []byte(`{"fixture":"redacted-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "documents", "fixture.pdf"), []byte("pdf-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"credentials", "models", "tools", "cache"} {
		if err := os.MkdirAll(filepath.Join(root, "documents", name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "documents", name, "must-not-backup"), []byte("sensitive-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	downloads := filepath.Join(t.TempDir(), "downloads")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "media.bin"), []byte("media-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	store.SetBackupDownloadDirectory(downloads)
	without, err := store.Snapshot(context.Background(), filepath.Join(t.TempDir(), "stage"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBackupFile(without, "documents/fixture.pdf") || hasBackupFile(without, "downloads/media.bin") || hasBackupFile(without, "documents/credentials/must-not-backup") || hasBackupFile(without, "credentials/backup-targets.json") {
		t.Fatalf("unexpected default range: %#v", without)
	}
	with, err := store.Snapshot(context.Background(), filepath.Join(t.TempDir(), "stage-with"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBackupFile(with, "documents/fixture.pdf") || !hasBackupFile(with, "downloads/media.bin") || hasBackupFile(with, "documents/models/must-not-backup") || hasBackupFile(with, "credentials/backup-targets.json") {
		t.Fatalf("download switch ignored: %#v", with)
	}
}

func TestBackupWebDAVConfigurationAndTargetRunStatusesPersist(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	target := backup.Target{ID: "webdav-fixture", Name: "Fixture DAV", Type: "webdav", Endpoint: "https://dav.example.test/base", Username: "fixture-user", RemotePath: "backups", AllowInsecureTLS: true, Enabled: true}
	if err := store.SaveTarget(ctx, target); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetTarget(ctx, target.ID)
	if err != nil || loaded.Endpoint != target.Endpoint || loaded.RemotePath != target.RemotePath || !loaded.AllowInsecureTLS {
		t.Fatalf("WebDAV configuration did not persist: %+v %v", loaded, err)
	}
	task := backup.Task{ID: "task-fixture", Name: "Fixture", TargetIDs: []string{target.ID, "local-fixture"}, Enabled: true}
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	snapshot := backup.Snapshot{ID: "snapshot-fixture", TaskID: "task-fixture", CreatedAt: time.Now().UTC(), Size: 7, Status: "running", RetryAvailable: true}
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	runs := []backup.TargetRun{{TargetID: target.ID, Name: target.Name, Status: "failed", Error: "WebDAV 返回 HTTP 401。"}, {TargetID: "local-fixture", Name: "Local", Status: "succeeded"}}
	if err := store.SaveTargetRuns(ctx, snapshot.ID, runs); err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE backup_run_target SET position=0 WHERE run_id=?`, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if err := addBackupRetryColumns(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	loadedSnapshot, err := store.GetSnapshot(ctx, snapshot.ID)
	if err != nil || loadedSnapshot.Status != "partial_success" || len(loadedSnapshot.Targets) != 2 || loadedSnapshot.Targets[0].TargetID != target.ID || loadedSnapshot.Targets[1].Error != "" || !loadedSnapshot.RetryAvailable {
		t.Fatalf("target run status did not persist: %+v %v", loadedSnapshot, err)
	}
	if err := store.SaveTask(ctx, backup.Task{ID: task.ID, Name: task.Name, TargetIDs: []string{"local-fixture", target.ID}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	loadedSnapshot, err = store.GetSnapshot(ctx, snapshot.ID)
	if err != nil || loadedSnapshot.Targets[0].TargetID != target.ID || loadedSnapshot.Targets[1].TargetID != "local-fixture" {
		t.Fatalf("historical target run order changed with current task binding: %+v %v", loadedSnapshot.Targets, err)
	}
	loadedSnapshot.RetryAvailable = false
	loadedSnapshot.RetryUnavailableReason = "加密快照已超过 7 天，无法重试。"
	if err := store.SaveSnapshot(ctx, loadedSnapshot); err != nil {
		t.Fatal(err)
	}
	loadedSnapshot, err = store.GetSnapshot(ctx, snapshot.ID)
	if err != nil || loadedSnapshot.RetryAvailable || loadedSnapshot.RetryUnavailableReason == "" {
		t.Fatalf("retry unavailability did not persist: %+v %v", loadedSnapshot, err)
	}
}

func TestSnapshotIsReadableDuringWALWrites(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	writer, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`INSERT INTO saved_content(id,payload,saved_at) VALUES(?,?,?)`, "wal-fixture", `{"title":"fixture"}`, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = writer.ExecContext(ctx, `INSERT INTO saved_content(id,payload,saved_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, "wal-fixture", `{"title":"fixture"}`, time.Now().UnixNano())
		}
	}()
	stage := filepath.Join(t.TempDir(), "stage")
	files, err := store.Snapshot(ctx, stage, false)
	close(stop)
	<-done
	if err != nil {
		t.Fatalf("concurrent snapshot: %v", err)
	}
	if !hasBackupFile(files, "tangentry.db") {
		t.Fatal("snapshot omitted database")
	}
	copyDB, err := sql.Open("sqlite", filepath.Join(stage, "tangentry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var payload string
	if err := copyDB.QueryRow(`SELECT payload FROM saved_content WHERE id=?`, "wal-fixture").Scan(&payload); err != nil {
		t.Fatalf("snapshot database is not readable: %v", err)
	}
}

func hasBackupFile(files []backup.File, name string) bool {
	for _, file := range files {
		if file.Path == name {
			return true
		}
	}
	return false
}
