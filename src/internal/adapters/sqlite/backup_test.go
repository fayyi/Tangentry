package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tangentry/internal/backup"
	"tangentry/internal/credentials"
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

func TestSnapshotNormalizesUserSelectedDownloadDirectory(t *testing.T) {
	store := inboxTestStore(t)
	downloads := filepath.Join(t.TempDir(), "Downloads")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "media.bin"), []byte("media-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	store.SetBackupDownloadDirectory(downloads)

	files, err := store.Snapshot(context.Background(), filepath.Join(t.TempDir(), "stage"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBackupFile(files, "downloads/media.bin") || hasBackupFile(files, "Downloads/media.bin") {
		t.Fatalf("user-selected download directory was not normalized: %#v", files)
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

func TestBackupListsReleaseSQLiteConnectionBeforeLoadingRelatedRows(t *testing.T) {
	store := inboxTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.SaveTarget(ctx, backup.Target{ID: "list-target", Name: "List target", Type: "local", Path: t.TempDir(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, backup.Task{ID: "list-task", Name: "List task", TargetIDs: []string{"list-target"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	snapshot := backup.Snapshot{ID: "list-snapshot", TaskID: "list-task", CreatedAt: time.Now().UTC(), Status: "succeeded"}
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTargetRuns(ctx, snapshot.ID, []backup.TargetRun{{TargetID: "list-target", Name: "List target", Status: "succeeded"}}); err != nil {
		t.Fatal(err)
	}

	tasks, err := store.BackupTasks(ctx)
	if err != nil || len(tasks) != 1 || len(tasks[0].TargetIDs) != 1 {
		t.Fatalf("backup task list did not load target bindings: tasks=%+v err=%v", tasks, err)
	}
	snapshots, err := store.BackupSnapshots(ctx)
	if err != nil || len(snapshots) != 1 || len(snapshots[0].Targets) != 1 {
		t.Fatalf("backup snapshot list did not load target runs: snapshots=%+v err=%v", snapshots, err)
	}

	if err := store.SaveTarget(ctx, backup.Target{ID: "list-target", Name: "Updated target", Type: "local", Path: t.TempDir(), Enabled: true}); err != nil {
		t.Fatalf("target save blocked after loading task/snapshot associations: %v", err)
	}
}

func TestBackupPlanPersistsDefaultAndTargetIntervals(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"schedule-a", "schedule-b"} {
		if err := store.SaveTarget(ctx, backup.Target{ID: id, Name: id, Type: "local", Path: t.TempDir(), Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	task := backup.Task{ID: "schedule-plan", Name: "Auto backup", TargetIDs: []string{"schedule-a", "schedule-b"}, IntervalHours: 6, TargetIntervalHours: map[string]int{"schedule-b": 3}, Retention: 12, Enabled: true}
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetTask(ctx, task.ID)
	if err != nil || loaded.IntervalHours != 6 || loaded.TargetIntervalHours["schedule-b"] != 3 || loaded.TargetIntervalHours["schedule-a"] != 0 || loaded.Retention != 12 {
		t.Fatalf("backup schedule did not round-trip: %+v %v", loaded, err)
	}
}

func TestBackupStoreRejectsSecondBackupPlan(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	first := backup.Task{ID: "single-plan-a", Name: "Auto backup", TargetIDs: []string{"target-a"}, Enabled: true}
	if err := store.SaveTarget(ctx, backup.Target{ID: "target-a", Name: "A", Type: "local", Path: t.TempDir(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, backup.Task{ID: "single-plan-b", Name: "Another plan", TargetIDs: []string{"target-a"}, Enabled: true}); err == nil {
		t.Fatal("second backup plan was accepted")
	}
	tasks, err := store.BackupTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("single-plan invariant changed after rejected save: %+v %v", tasks, err)
	}
}

func TestDeletingBackupTaskKeepsSnapshotHistory(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	if err := store.SaveTarget(ctx, backup.Target{ID: "history-target", Name: "History target", Type: "local", Path: t.TempDir(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	task := backup.Task{ID: "history-task", Name: "History task", TargetIDs: []string{"history-target"}, Enabled: true}
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	snapshot := backup.Snapshot{ID: "history-snapshot", TaskID: task.ID, CreatedAt: time.Now().UTC(), Size: 42, Status: "succeeded"}
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	run := backup.TargetRun{TargetID: "history-target", Name: "History target", Status: "succeeded"}
	if err := store.SaveTargetRuns(ctx, snapshot.ID, []backup.TargetRun{run}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTask(ctx, task.ID); err == nil {
		t.Fatal("deleted backup task still exists")
	}
	loaded, err := store.GetSnapshot(ctx, snapshot.ID)
	if err != nil || loaded.TaskID != task.ID || loaded.Status != "succeeded" || len(loaded.Targets) != 1 || loaded.Targets[0].TargetID != run.TargetID {
		t.Fatalf("snapshot history was not preserved: %+v %v", loaded, err)
	}
	var targetBindings int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM backup_task_target WHERE task_id=?`, task.ID).Scan(&targetBindings); err != nil || targetBindings != 0 {
		t.Fatalf("task target bindings were not removed: count=%d err=%v", targetBindings, err)
	}
}

func TestSaveBackupTargetLogsSQLiteConnectionAndWriteTiming(t *testing.T) {
	store := inboxTestStore(t)
	credentialStore, err := credentials.OpenWithKey(filepath.Join(t.TempDir(), "credentials"), "", "", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := backup.NewService(store, nil, nil, t.TempDir(), "test")
	service.SetTargetCredentials(credentialStore)
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	target := backup.Target{ID: "sqlite-timing", Name: "SQLite timing fixture", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Password: "fixture-password", UpdateCredential: true, Enabled: true}
	started := time.Now()
	if err := service.SaveTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("SQLite target save took %s", elapsed)
	}
	for _, phase := range []string{"connection_wait", "target_lookup", "target_write"} {
		if !strings.Contains(logs.String(), "phase="+phase) {
			t.Errorf("missing SQLite timing phase %q in %s", phase, logs.String())
		}
	}
	for _, sensitive := range []string{"fixture-user", "fixture-password", "dav.example.test"} {
		if strings.Contains(logs.String(), sensitive) {
			t.Errorf("SQLite timing log contains sensitive field %q", sensitive)
		}
	}
	loaded, err := store.GetTarget(context.Background(), target.ID)
	if err != nil || loaded.Password != "" || loaded.Endpoint != target.Endpoint {
		t.Fatalf("saved target was not persisted without password: %+v err=%v", loaded, err)
	}
}

func TestSaveBackupTargetSeparatesSQLitePoolWaitFromWrite(t *testing.T) {
	store := inboxTestStore(t)
	conn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- store.SaveTarget(context.Background(), backup.Target{ID: "sqlite-wait", Name: "Wait fixture", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Enabled: true})
	}()
	<-started
	deadline := time.Now().Add(500 * time.Millisecond)
	for store.db.Stats().WaitCount == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	waited := store.db.Stats().WaitCount > 0
	if waited {
		time.Sleep(30 * time.Millisecond)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !waited {
		t.Fatal("save did not wait for the occupied single-connection pool")
	}
	wait := regexp.MustCompile(`phase=connection_wait duration_ms=([0-9]+)`).FindStringSubmatch(logs.String())
	write := regexp.MustCompile(`phase=target_write duration_ms=([0-9]+)`).FindStringSubmatch(logs.String())
	if len(wait) != 2 || len(write) != 2 {
		t.Fatalf("missing SQLite timing detail: %s", logs.String())
	}
	waitMS, _ := strconv.Atoi(wait[1])
	writeMS, _ := strconv.Atoi(write[1])
	if waitMS < 20 || waitMS <= writeMS {
		t.Fatalf("pool wait was not distinguished from target write: wait=%dms write=%dms logs=%s", waitMS, writeMS, logs.String())
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
