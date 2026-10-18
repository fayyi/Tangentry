package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncryptDecryptAndTamperDetection(t *testing.T) {
	manifest := Manifest{KDF: KDFParams{Salt: "00112233445566778899aabbccddeeff"}}
	plain := bytes.Repeat([]byte("fixture archive contents"), 90000)
	encoded, err := Encrypt(plain, "fixture-password", &manifest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(encoded, "fixture-password")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := Decrypt(encoded, "wrong-password"); err != ErrBadPassword {
		t.Fatalf("wrong password: %v", err)
	}
	encoded[len(encoded)-1] ^= 1
	if _, err := Decrypt(encoded, "fixture-password"); err != ErrBadPassword {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := Decrypt(encoded[:len(encoded)-3], "fixture-password"); err == nil {
		t.Fatal("truncated package accepted")
	}
	for _, offset := range []int{21, 25, 29, 30, 34, 38} {
		copyOfHeader := append([]byte(nil), encoded...)
		copyOfHeader[offset] ^= 1
		if _, err := Decrypt(copyOfHeader, "fixture-password"); err == nil {
			t.Fatalf("header tamper at offset %d accepted", offset)
		}
	}
}

func TestValidateArchiveChecksManifestAndHashes(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	payload := []byte("fixture database")
	manifest := Manifest{FormatVersion: FormatVersion, SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", Files: []File{{Path: "tangentry.db", Type: FileDatabase, Size: int64(len(payload)), SHA256: "bad"}}}
	raw, _ := json.Marshal(manifest)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(raw)
	w, _ = zw.Create("tangentry.db")
	_, _ = w.Write(payload)
	_ = zw.Close()
	if _, err := validateArchive(buf.Bytes(), "snapshot-1"); err == nil {
		t.Fatal("bad manifest hash accepted")
	}
}

func TestValidateArchiveRequiresDatabaseAndValidFileTypes(t *testing.T) {
	payload := []byte("fixture data")
	digest := sha256.Sum256(payload)
	manifest := Manifest{FormatVersion: FormatVersion, SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: "00112233445566778899aabbccddeeff", Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}, Files: []File{{Path: "data.txt", Type: FileData, Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", digest)}}}
	archive := archiveFixture(t, manifest, map[string][]byte{"data.txt": payload})
	if _, err := validateArchive(archive, "snapshot-1"); err == nil {
		t.Fatal("manifest without tangentry.db accepted")
	}
	manifest.Files = append(manifest.Files, File{Path: "tangentry.db", Type: "unknown", Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", digest)})
	archive = archiveFixture(t, manifest, map[string][]byte{"data.txt": payload, "tangentry.db": payload})
	if _, err := validateArchive(archive, "snapshot-1"); err == nil {
		t.Fatal("unknown file type accepted")
	}
}

func TestManifestPathAllowlistFollowsDownloadSetting(t *testing.T) {
	for _, name := range []string{"credentials/session.json", "models/model.bin", "tools/ffmpeg", "cache/item", "documents/credentials/session.json", "documents/cache/item"} {
		files := []File{{Path: "tangentry.db", Type: FileDatabase}, {Path: name, Type: FileData}}
		if err := validateManifestFiles(files, true); err == nil {
			t.Errorf("excluded path %q accepted", name)
		}
	}
	if err := validateManifestFiles([]File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "downloads/video.mp4", Type: FileData}}, false); err == nil {
		t.Fatal("downloads accepted while disabled")
	}
	if err := validateManifestFiles([]File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "downloads/video.mp4", Type: FileData}}, true); err != nil {
		t.Fatalf("downloads rejected while enabled: %v", err)
	}
	if err := validateManifestFiles([]File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "documents/captures/frame.png", Type: FileData}}, false); err != nil {
		t.Fatalf("documents path rejected: %v", err)
	}
}

func archiveFixture(t *testing.T, manifest Manifest, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractArchiveRejectsExistingSymlink(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write([]byte(`{"formatVersion":1}`))
	w, _ = zw.Create("nested/file.txt")
	_, _ = w.Write([]byte("fixture"))
	_ = zw.Close()
	destination := filepath.Join(t.TempDir(), "restore")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "nested")); err != nil {
		t.Skip("symlink unavailable")
	}
	if err := extractArchive(buf.Bytes(), destination); err == nil {
		t.Fatal("existing symlink followed")
	}
}

func TestExtractArchiveRejectsDownloadsWhenDisabled(t *testing.T) {
	payload := []byte("media fixture")
	manifest := Manifest{FormatVersion: FormatVersion, SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", Files: []File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "downloads/video.bin", Type: FileData}}}
	archive := archiveFixture(t, manifest, map[string][]byte{"tangentry.db": []byte("db"), "downloads/video.bin": payload})
	if err := extractArchive(archive, filepath.Join(t.TempDir(), "restore")); err == nil {
		t.Fatal("download media restored when includeDownloads was false")
	}
}

func TestExtractArchiveRejectsZipSlipAndHardLinkLikeEntries(t *testing.T) {
	for _, name := range []string{"../outside.txt", "documents/../../outside.txt", "documents/credentials/secret.json"} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("fixture"))
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := extractArchive(buf.Bytes(), filepath.Join(t.TempDir(), "restore")); err == nil {
			t.Errorf("unsafe entry %q accepted", name)
		}
	}
}

func TestValidateArchiveRejectsUnknownManifestAndUnmanagedEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := Manifest{FormatVersion: FormatVersion, SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", KDF: KDFParams{Name: "Argon2id", Version: 1, Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}, Files: []File{}}
	raw, _ := json.Marshal(manifest)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(raw)
	w, _ = zw.Create("unexpected.txt")
	_, _ = w.Write([]byte("fixture"))
	_ = zw.Close()
	if _, err := validateArchive(buf.Bytes(), "snapshot-1"); err == nil {
		t.Fatal("unmanaged archive entry accepted")
	}
	manifest.FormatVersion = FormatVersion + 1
	buf.Reset()
	zw = zip.NewWriter(&buf)
	raw, _ = json.Marshal(manifest)
	w, _ = zw.Create("manifest.json")
	_, _ = w.Write(raw)
	_ = zw.Close()
	if _, err := validateArchive(buf.Bytes(), "snapshot-1"); err != ErrUnknownVersion {
		t.Fatalf("unknown manifest version: %v", err)
	}
}

func TestExtractArchiveRejectsCompressionBombAndZipSymlink(t *testing.T) {
	var bomb bytes.Buffer
	zw := zip.NewWriter(&bomb)
	w, _ := zw.Create("large.bin")
	_, _ = w.Write(bytes.Repeat([]byte("A"), 16<<20))
	_ = zw.Close()
	if err := extractArchive(bomb.Bytes(), filepath.Join(t.TempDir(), "restore")); err == nil {
		t.Fatal("compression bomb accepted")
	}
	var linked bytes.Buffer
	zw = zip.NewWriter(&linked)
	h := &zip.FileHeader{Name: "link", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0600)
	w, _ = zw.CreateHeader(h)
	_, _ = w.Write([]byte("outside"))
	_ = zw.Close()
	if err := extractArchive(linked.Bytes(), filepath.Join(t.TempDir(), "restore")); err == nil {
		t.Fatal("zip symlink accepted")
	}
}

func TestArchiveFilesRejectsStagedSymlink(t *testing.T) {
	stage := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stage, "secret.txt")); err != nil {
		t.Skip("symlink unavailable")
	}
	_, err := archiveFiles(stage, &Manifest{Files: []File{{Path: "secret.txt", Type: FileData}}})
	if err == nil {
		t.Fatal("staged symlink archived")
	}
}

func TestCleanRelativeRejectsTraversal(t *testing.T) {
	for _, value := range []string{"../secret", "/absolute", `..\secret`} {
		if _, err := cleanRelative(value); err == nil {
			t.Fatalf("accepted unsafe path %q", value)
		}
	}
}

type restoreRepo struct{}

func (restoreRepo) ListTargets(context.Context) ([]Target, error) {
	return []Target{{ID: "target-1", Type: "local", Enabled: true}}, nil
}
func (restoreRepo) GetTarget(context.Context, string) (Target, error) {
	return Target{ID: "target-1", Type: "local", Enabled: true}, nil
}
func (restoreRepo) SaveTarget(context.Context, Target) error          { return nil }
func (restoreRepo) DeleteTarget(context.Context, string) error        { return nil }
func (restoreRepo) ListTasks(context.Context) ([]Task, error)         { return nil, nil }
func (restoreRepo) GetTask(context.Context, string) (Task, error)     { return Task{}, nil }
func (restoreRepo) SaveTask(context.Context, Task) error              { return nil }
func (restoreRepo) DeleteTask(context.Context, string) error          { return nil }
func (restoreRepo) SaveSnapshot(context.Context, Snapshot) error      { return nil }
func (restoreRepo) ListSnapshots(context.Context) ([]Snapshot, error) { return nil, nil }

type restoreStore struct{ data []byte }

func (s restoreStore) Upload(context.Context, string, io.Reader) error { return nil }
func (s restoreStore) Read(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}
func (restoreStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (restoreStore) Delete(context.Context, string) error           { return nil }
func (restoreStore) TestConnection(context.Context) error           { return nil }

type restoreSource struct{}

func (restoreSource) Snapshot(_ context.Context, staging string, _ bool) ([]File, error) {
	if err := os.WriteFile(filepath.Join(staging, "tangentry.db"), []byte("current"), 0600); err != nil {
		return nil, err
	}
	return []File{{Path: "tangentry.db", Type: FileDatabase, Size: 7, SHA256: "c17b8e8b5f8f2f5f43a3a6f3c9e0a4d1f7d4c5b0e8f5f4f6b5d9f6d4c8f6c4b"}}, nil
}

func TestRestoreKeepsPreflightSnapshotUntilConfirmation(t *testing.T) {
	payload := []byte("restored")
	dbPayload := []byte("database")
	digest := sha256.Sum256(payload)
	hash := fmt.Sprintf("%x", digest)
	dbDigest := sha256.Sum256(dbPayload)
	manifest := Manifest{FormatVersion: FormatVersion, TangentryVersion: "test", SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", CreatedAt: time.Now().UTC(), Files: []File{{Path: "documents/restored.txt", Type: FileData, Size: int64(len(payload)), SHA256: hash}, {Path: "tangentry.db", Type: FileDatabase, Size: int64(len(dbPayload)), SHA256: fmt.Sprintf("%x", dbDigest)}}, KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: "00112233445566778899aabbccddeeff", Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	raw, _ := json.Marshal(manifest)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(raw)
	w, _ = zw.Create("documents/restored.txt")
	_, _ = w.Write(payload)
	w, _ = zw.Create("tangentry.db")
	_, _ = w.Write(dbPayload)
	_ = zw.Close()
	encoded, err := Encrypt(archive.Bytes(), "fixture-password", &manifest)
	if err != nil {
		t.Fatal(err)
	}
	plain, decryptErr := Decrypt(encoded, "fixture-password")
	if decryptErr != nil {
		t.Fatal(decryptErr)
	}
	if _, validateErr := validateArchive(plain, "snapshot-1"); validateErr != nil {
		t.Fatalf("fixture archive validation: %v", validateErr)
	}
	service := NewService(restoreRepo{}, restoreSource{}, func(Target) (ObjectStore, error) { return restoreStore{data: encoded}, nil }, t.TempDir(), "test")
	result, err := service.Restore(context.Background(), "target-1", "snapshot-1", "fixture-password", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pending_confirmation" || result.PreRestoreSnapshotID == "" {
		t.Fatalf("unexpected restore plan: %+v", result)
	}
	confirmed, err := service.ConfirmRestore(context.Background(), result.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "restart_required" || !confirmed.RestartRequired {
		t.Fatalf("restore was not journaled for restart: %+v", confirmed)
	}
	if _, err := service.ConfirmRestore(context.Background(), result.PlanID); err == nil {
		t.Fatal("a second confirmation replaced the active restore journal")
	}
	if _, err := service.Restore(context.Background(), "target-1", "snapshot-1", "fixture-password", ""); err == nil {
		t.Fatal("new restore was prepared while another restore awaited restart")
	}
	if err := ApplyPendingRestore(filepath.Dir(filepath.Dir(service.workDir))); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPendingRestoreIsIdempotentAfterInterruptedMoves(t *testing.T) {
	dataDir := t.TempDir()
	workDir := filepath.Join(dataDir, ".tangentry-restore")
	if err := os.MkdirAll(filepath.Join(workDir, "staged-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "documents", "old.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "staged-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "tangentry.db"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeRestoreJournal(workDir, RestoreJournal{
		StagedID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RollbackID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Entries: []RestoreJournalEntry{{Name: "tangentry.db", State: entryPending}}, CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryPending}}, Manifest: Manifest{Files: []File{{Path: "tangentry.db", Type: FileDatabase, Size: 3, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("new")))}}}, Step: "confirmed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "tangentry.db")); err != nil || string(got) != "new" {
		t.Fatalf("restored database = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, ".tangentry-restore", "rollback-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "documents", "old.txt")); err != nil || string(got) != "old" {
		t.Fatalf("rollback snapshot = %q, err=%v", got, err)
	}
}

func TestApplyPendingRestoreRecoversPartiallyMovedConfirmedJournal(t *testing.T) {
	dataDir := t.TempDir()
	workDir := restoreWorkDir(dataDir)
	stagedID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rollbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staged := filepath.Join(workDir, "staged-"+stagedID)
	rollback := filepath.Join(workDir, "rollback-"+rollbackID)
	for _, directory := range []string{staged, filepath.Join(dataDir, "documents"), filepath.Join(rollback, "documents")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db := []byte("new database")
	document := []byte("new document")
	oldDocument := []byte("old document")
	if err := os.WriteFile(filepath.Join(staged, "tangentry.db"), db, 0600); err != nil {
		t.Fatal(err)
	}
	// The staged documents directory has moved to the active directory while
	// the journal still says confirmed, as in the legacy crash window.
	if err := os.WriteFile(filepath.Join(dataDir, "documents", "new.txt"), document, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "documents", "old.txt"), oldDocument, 0600); err != nil {
		t.Fatal(err)
	}
	dbHash := sha256.Sum256(db)
	docHash := sha256.Sum256(document)
	journal := RestoreJournal{
		StagedID: stagedID, RollbackID: rollbackID,
		Entries: []RestoreJournalEntry{
			{Name: "documents", State: entryPending},
			{Name: "tangentry.db", State: entryPending},
		},
		CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryPending}},
		Manifest: Manifest{Files: []File{
			{Path: "documents/new.txt", Type: FileData, Size: int64(len(document)), SHA256: fmt.Sprintf("%x", docHash)},
			{Path: "tangentry.db", Type: FileDatabase, Size: int64(len(db)), SHA256: fmt.Sprintf("%x", dbHash)},
		}},
		Step: "confirmed",
	}
	if err := writeRestoreJournal(workDir, journal); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatalf("resume partially moved confirmed restore: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "tangentry.db")); err != nil || !bytes.Equal(got, db) {
		t.Fatalf("restored database = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "documents", "new.txt")); err != nil || !bytes.Equal(got, document) {
		t.Fatalf("restored document = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(rollback, "documents", "old.txt")); err != nil || !bytes.Equal(got, oldDocument) {
		t.Fatalf("pre-restore document = %q, err=%v", got, err)
	}
}

func TestApplyPendingRestorePersistsInstallingBeforeFirstMove(t *testing.T) {
	dataDir := t.TempDir()
	workDir := restoreWorkDir(dataDir)
	stagedID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rollbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staged := filepath.Join(workDir, "staged-"+stagedID)
	if err := os.MkdirAll(filepath.Join(staged, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	db := []byte("new database")
	document := []byte("new document")
	if err := os.WriteFile(filepath.Join(staged, "tangentry.db"), db, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "documents", "new.txt"), document, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "documents", "old.txt"), []byte("old document"), 0600); err != nil {
		t.Fatal(err)
	}
	dbHash := sha256.Sum256(db)
	docHash := sha256.Sum256(document)
	journal := RestoreJournal{
		StagedID: stagedID, RollbackID: rollbackID,
		Entries:        []RestoreJournalEntry{{Name: "documents", State: entryPending}, {Name: "tangentry.db", State: entryPending}},
		CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryPending}},
		Manifest: Manifest{Files: []File{
			{Path: "documents/new.txt", Type: FileData, Size: int64(len(document)), SHA256: fmt.Sprintf("%x", docHash)},
			{Path: "tangentry.db", Type: FileDatabase, Size: int64(len(db)), SHA256: fmt.Sprintf("%x", dbHash)},
		}},
		Step: "confirmed",
	}
	if err := writeRestoreJournal(workDir, journal); err != nil {
		t.Fatal(err)
	}
	previousRename := renameRestoreEntry
	crashed := false
	renameRestoreEntry = func(oldpath, newpath string) error {
		persisted, err := readRestoreJournal(workDir)
		if err != nil || persisted.Step != "installing" {
			return fmt.Errorf("journal state before first move = %q, err=%v", persisted.Step, err)
		}
		if err := os.Rename(oldpath, newpath); err != nil {
			return err
		}
		if !crashed {
			crashed = true
			panic("simulated process interruption after rename")
		}
		return nil
	}
	func() {
		defer func() { _ = recover() }()
		_ = ApplyPendingRestore(dataDir)
	}()
	renameRestoreEntry = previousRename
	if !crashed {
		t.Fatal("restore did not reach the simulated crash point")
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatalf("resume after interrupted first move: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "tangentry.db")); err != nil || !bytes.Equal(got, db) {
		t.Fatalf("restored database = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(workDir, "rollback-"+rollbackID, "documents", "old.txt")); err != nil || string(got) != "old document" {
		t.Fatalf("pre-restore document = %q, err=%v", got, err)
	}
}

func TestApplyPendingRestoreRollsBackOnlyInstalledEntriesAfterFailure(t *testing.T) {
	dataDir := t.TempDir()
	workDir := restoreWorkDir(dataDir)
	stagedID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rollbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staged := filepath.Join(workDir, "staged-"+stagedID)
	rollback := filepath.Join(workDir, "rollback-"+rollbackID)
	if err := os.MkdirAll(filepath.Join(staged, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rollback, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "tangentry.db"), []byte("new database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "documents", "new.txt"), []byte("new document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "documents", "old.txt"), []byte("old document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dataDir, "documents")); err != nil {
		t.Fatal(err)
	}
	journal := RestoreJournal{
		StagedID: stagedID, RollbackID: rollbackID,
		Entries:        []RestoreJournalEntry{{Name: "tangentry.db", State: entryInstalled}, {Name: "documents", State: entryPending}},
		CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryMovedToRollback}},
		Manifest:       Manifest{Files: []File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "documents/new.txt", Type: FileData}}},
		Step:           "installing",
	}
	if err := writeRestoreJournal(workDir, journal); err != nil {
		t.Fatal(err)
	}
	previousRename := renameRestoreEntry
	renameRestoreEntry = func(oldpath, newpath string) error {
		if oldpath == filepath.Join(staged, "documents") {
			return errors.New("injected install failure")
		}
		return os.Rename(oldpath, newpath)
	}
	err := ApplyPendingRestore(dataDir)
	renameRestoreEntry = previousRename
	if err == nil {
		t.Fatal("injected install failure was ignored")
	}
	if got, readErr := os.ReadFile(filepath.Join(dataDir, "documents", "old.txt")); readErr != nil || string(got) != "old document" {
		t.Fatalf("old data was not restored: %q, err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(filepath.Join(workDir, "failed-"+stagedID, "tangentry.db")); readErr != nil || string(got) != "new database" {
		t.Fatalf("installed data was not retained for diagnosis: %q, err=%v", got, readErr)
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatalf("startup cleanup after rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "tangentry.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new database remained installed after rollback: %v", err)
	}
}

func TestApplyPendingRestoreResumesHalfInstalledJournal(t *testing.T) {
	dataDir := t.TempDir()
	workDir := restoreWorkDir(dataDir)
	stagedID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rollbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staged := filepath.Join(workDir, "staged-"+stagedID)
	rollback := filepath.Join(workDir, "rollback-"+rollbackID)
	if err := os.MkdirAll(filepath.Join(staged, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rollback, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "tangentry.db"), []byte("new database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "documents", "new.txt"), []byte("new document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "documents", "old.txt"), []byte("old document"), 0600); err != nil {
		t.Fatal(err)
	}
	journal := RestoreJournal{
		StagedID: stagedID, RollbackID: rollbackID,
		Entries:        []RestoreJournalEntry{{Name: "tangentry.db", State: entryInstalled}, {Name: "documents", State: entryPending}},
		CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryMovedToRollback}},
		Manifest:       Manifest{Files: []File{{Path: "tangentry.db", Type: FileDatabase}, {Path: "documents/new.txt", Type: FileData}}},
		Step:           "installing",
	}
	if err := writeRestoreJournal(workDir, journal); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dataDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "documents", "new.txt")); err != nil || string(got) != "new document" {
		t.Fatalf("staged install was not resumed: %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(workDir, "rollback-"+rollbackID, "documents", "old.txt")); err != nil || string(got) != "old document" {
		t.Fatalf("pre-restore data was not retained: %q, err=%v", got, err)
	}
}

func TestApplyPendingRestoreContinuesAfterRollbackWasInterrupted(t *testing.T) {
	dataDir := t.TempDir()
	workDir := restoreWorkDir(dataDir)
	stagedID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rollbackID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	staged := filepath.Join(workDir, "staged-"+stagedID)
	rollback := filepath.Join(workDir, "rollback-"+rollbackID)
	failed := filepath.Join(workDir, "failed-"+stagedID)
	for _, directory := range []string{staged, rollback, failed} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "documents"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failed, "tangentry.db"), []byte("new database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "documents-old.txt"), []byte("old document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "documents", "current.txt"), []byte("current document"), 0600); err != nil {
		t.Fatal(err)
	}
	journal := RestoreJournal{
		StagedID: stagedID, RollbackID: rollbackID,
		Entries:        []RestoreJournalEntry{{Name: "tangentry.db", State: entryMovedToFailed}},
		CurrentEntries: []RestoreJournalEntry{{Name: "documents", State: entryRestored}},
		Manifest:       Manifest{Files: []File{{Path: "tangentry.db", Type: FileDatabase}}},
		Step:           "rolling_back",
	}
	if err := writeRestoreJournal(workDir, journal); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPendingRestore(dataDir); err == nil {
		t.Fatal("rollback completion status was not reported")
	}
	if got, err := os.ReadFile(filepath.Join(dataDir, "documents", "current.txt")); err != nil || string(got) != "current document" {
		t.Fatalf("restored current data was lost: %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "journal.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed rollback journal not cleaned: %v", err)
	}
}

func TestCancelConfirmedRestoreAfterServiceRestart(t *testing.T) {
	workDir := t.TempDir()
	planID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	preflightID := "cccccccccccccccccccccccccccccccc"
	staged := filepath.Join(workDir, "staged-"+planID)
	preflight := filepath.Join(workDir, "pre-"+preflightID)
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(preflight, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SnapshotID: "snapshot-1", Files: []File{{Path: "tangentry.db", Type: FileDatabase}}}
	if err := writeRestoreJournal(workDir, RestoreJournal{PlanID: planID, StagedID: planID, RollbackID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PreflightID: preflightID, Entries: []RestoreJournalEntry{{Name: "tangentry.db", State: entryPending}}, CurrentEntries: []RestoreJournalEntry{{Name: "tangentry.db", State: entryPending}}, Manifest: manifest, Step: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(restoreRepo{}, restoreSource{}, nil, workDir, "test")
	if err := service.CancelRestore(context.Background(), planID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "journal.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists after cancellation: %v", err)
	}
	for _, path := range []string{staged, preflight} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("restore workspace remains at %q: %v", path, err)
		}
	}
}

func TestPruneRollbackKeepsMostRecentDirectory(t *testing.T) {
	workDir := t.TempDir()
	for i, id := range []string{"old", "middle", "new"} {
		path := filepath.Join(workDir, "rollback-"+id)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneRollbackDirs(workDir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "rollback-new")); err != nil {
		t.Fatalf("newest rollback was pruned: %v", err)
	}
	for _, id := range []string{"old", "middle"} {
		if _, err := os.Stat(filepath.Join(workDir, "rollback-"+id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old rollback %q remains: %v", id, err)
		}
	}
}

func TestRestoreWorkDirSizeRejectsOverLimit(t *testing.T) {
	workDir := t.TempDir()
	file, err := os.Create(filepath.Join(workDir, "large-sparse-file"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxRestoreWorkDirSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if size, err := restoreWorkDirSize(workDir); err == nil || size <= MaxRestoreWorkDirSize {
		t.Fatalf("oversized workspace accepted: size=%d, err=%v", size, err)
	}
}

func TestRestoreSerializesWorkspaceReservations(t *testing.T) {
	workDir := t.TempDir()
	db := []byte("fixture database")
	digest := sha256.Sum256(db)
	manifest := Manifest{FormatVersion: FormatVersion, SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", Files: []File{{Path: "tangentry.db", Type: FileDatabase, Size: int64(len(db)), SHA256: fmt.Sprintf("%x", digest)}}, KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: "00112233445566778899aabbccddeeff", Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}}
	encoded, err := Encrypt(archiveFixture(t, manifest, map[string][]byte{"tangentry.db": db}), "fixture-password", &manifest)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(restoreRepo{}, restoreSource{}, func(Target) (ObjectStore, error) { return restoreStore{data: encoded}, nil }, workDir, "test")
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, MaxRestorePlans+1)
	for range MaxRestorePlans + 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Restore(context.Background(), "target-1", "snapshot-1", "fixture-password", "")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	quotaErrors := 0
	for err := range results {
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "too many pending restore plans") {
			quotaErrors++
		} else {
			t.Fatalf("unexpected restore result: %v", err)
		}
	}
	if successes != MaxRestorePlans || quotaErrors != 1 {
		t.Fatalf("restore reservations: successes=%d quotaErrors=%d", successes, quotaErrors)
	}
}

func TestRestorePlansExpireAndEnforceCountLimit(t *testing.T) {
	workDir := t.TempDir()
	service := NewService(restoreRepo{}, restoreSource{}, nil, workDir, "test")
	now := time.Now().UTC()
	for i := 0; i < MaxRestorePlans+1; i++ {
		id := fmt.Sprintf("%032x", i+1)
		preflightID := fmt.Sprintf("%032x", i+101)
		stage := filepath.Join(workDir, "staged-"+id)
		preflight := filepath.Join(workDir, "pre-"+preflightID)
		if err := os.Mkdir(stage, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(preflight, 0700); err != nil {
			t.Fatal(err)
		}
		plan := testRestorePlan(id, preflightID, stage, preflight, now.Add(time.Duration(i)*time.Second))
		if err := service.savePlan(plan); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.cleanExpiredPlans(now.Add(RestorePlanTTL + time.Hour)); err != nil {
		t.Fatal(err)
	}
	count, err := pendingPlanCount(workDir)
	if err != nil || count != 0 {
		t.Fatalf("expired plan count = %d, err=%v", count, err)
	}
}

func TestRestorePlanCountPrunesOldestWhenOverLimit(t *testing.T) {
	workDir := t.TempDir()
	service := NewService(restoreRepo{}, restoreSource{}, nil, workDir, "test")
	for i := 0; i < MaxRestorePlans+1; i++ {
		id := fmt.Sprintf("%032x", i+1)
		preflightID := fmt.Sprintf("%032x", i+101)
		stage := filepath.Join(workDir, "staged-"+id)
		preflight := filepath.Join(workDir, "pre-"+preflightID)
		if err := os.Mkdir(stage, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(preflight, 0700); err != nil {
			t.Fatal(err)
		}
		plan := testRestorePlan(id, preflightID, stage, preflight, time.Now().UTC().Add(time.Duration(i)*time.Second))
		if err := service.savePlan(plan); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.cleanExpiredPlans(time.Now()); err != nil {
		t.Fatal(err)
	}
	count, err := pendingPlanCount(workDir)
	if err != nil || count != MaxRestorePlans {
		t.Fatalf("pending plan count = %d, err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "staged-00000000000000000000000000000001")); !os.IsNotExist(err) {
		t.Fatalf("oldest staged plan was not removed: %v", err)
	}
}

func testRestorePlan(id, preflightID, stage, preflight string, created time.Time) restorePlan {
	snapshotID := "snapshot-" + id[:8]
	manifest := Manifest{SnapshotID: snapshotID, Files: []File{{Path: "tangentry.db", Type: FileDatabase}}}
	return restorePlan{
		RestoreResult: RestoreResult{PlanID: id, StagedID: id, PreRestoreSnapshotID: preflightID, SnapshotID: snapshotID},
		StagePath:     stage,
		PreflightPath: preflight,
		CreatedAt:     created,
		Manifest:      manifest,
	}
}
