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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tangentry/internal/credentials"
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
func (restoreRepo) GetSnapshot(context.Context, string) (Snapshot, error) {
	return Snapshot{}, errors.New("not found")
}
func (restoreRepo) SaveTargetRuns(context.Context, string, []TargetRun) error { return nil }

type fanoutRepo struct {
	mu        sync.Mutex
	targets   map[string]Target
	failSave  error
	task      Task
	snapshots map[string]Snapshot
	runs      map[string][]TargetRun
}

type targetCredentialHostTransport struct {
	firstHost  string
	secondHost string
	first      http.RoundTripper
	second     http.RoundTripper
}

func (t targetCredentialHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Hostname() {
	case t.firstHost:
		return t.first.RoundTrip(request)
	case t.secondHost:
		return t.second.RoundTrip(request)
	default:
		return nil, fmt.Errorf("unexpected fixture host %q", request.URL.Hostname())
	}
}

func (r *fanoutRepo) ListTargets(context.Context) ([]Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Target, 0, len(r.targets))
	for _, target := range r.targets {
		out = append(out, target)
	}
	return out, nil
}
func (r *fanoutRepo) GetTarget(_ context.Context, id string) (Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, ok := r.targets[id]
	if !ok {
		return Target{}, errors.New("target not found")
	}
	return target, nil
}
func (r *fanoutRepo) SaveTarget(_ context.Context, target Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failSave != nil {
		return r.failSave
	}
	target.Password, target.UpdateCredential = "", false
	r.targets[target.ID] = target
	return nil
}
func (r *fanoutRepo) DeleteTarget(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.targets, id)
	return nil
}
func (*fanoutRepo) ListTasks(context.Context) ([]Task, error)       { return nil, nil }
func (r *fanoutRepo) GetTask(context.Context, string) (Task, error) { return r.task, nil }
func (*fanoutRepo) SaveTask(context.Context, Task) error            { return nil }
func (*fanoutRepo) DeleteTask(context.Context, string) error        { return nil }
func (r *fanoutRepo) SaveSnapshot(_ context.Context, snapshot Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots[snapshot.ID] = snapshot
	return nil
}
func (r *fanoutRepo) ListSnapshots(context.Context) ([]Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Snapshot, 0, len(r.snapshots))
	for id, snapshot := range r.snapshots {
		snapshot.Targets = append([]TargetRun(nil), r.runs[id]...)
		out = append(out, snapshot)
	}
	return out, nil
}
func (r *fanoutRepo) GetSnapshot(_ context.Context, id string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.snapshots[id]
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	snapshot.Targets = append([]TargetRun(nil), r.runs[id]...)
	return snapshot, nil
}
func (r *fanoutRepo) SaveTargetRuns(_ context.Context, id string, runs []TargetRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[id] = append([]TargetRun(nil), runs...)
	succeeded, failed := countTargetRuns(runs)
	snapshot := r.snapshots[id]
	snapshot.Targets = append([]TargetRun(nil), runs...)
	if failed > 0 && succeeded > 0 {
		snapshot.Status = "partial_success"
	} else if failed > 0 {
		snapshot.Status = "failed"
	} else {
		snapshot.Status = "succeeded"
	}
	r.snapshots[id] = snapshot
	return nil
}

type fanoutStore struct {
	count   *atomic.Int32
	fail    bool
	content *[]byte
}

func (s fanoutStore) Upload(_ context.Context, _ string, r io.Reader) error {
	s.count.Add(1)
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if s.content != nil {
		*s.content = append([]byte(nil), data...)
	}
	if s.fail {
		return errors.New("fixture failure with secret fixture-password")
	}
	return nil
}
func (fanoutStore) Read(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (fanoutStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (fanoutStore) Delete(context.Context, string) error           { return nil }
func (fanoutStore) TestConnection(context.Context) error           { return nil }

type fixtureTargetCredentials struct {
	values     map[string]string
	failDelete bool
}

type fixtureFolderStore struct {
	browsePath string
	closed     bool
}

func (s *fixtureFolderStore) Browse(_ context.Context, path string) ([]RemoteFolder, error) {
	s.browsePath = path
	childPath := "Tangentry"
	if path != "" {
		childPath = path + "/Tangentry"
	}
	return []RemoteFolder{{Name: "Tangentry", Path: childPath}}, nil
}
func (*fixtureFolderStore) CreateFolder(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *fixtureFolderStore) Close() error { s.closed = true; return nil }

func (c *fixtureTargetCredentials) SaveBackupTargetPassword(id, password string) error {
	c.values[id] = password
	return nil
}
func (c *fixtureTargetCredentials) BackupTargetPassword(id string) (string, bool) {
	value, ok := c.values[id]
	return value, ok
}
func (c *fixtureTargetCredentials) BackupTargetConfigured(id string) bool {
	_, ok := c.values[id]
	return ok
}
func (c *fixtureTargetCredentials) DeleteBackupTargetPassword(id string) error {
	if c.failDelete {
		return errors.New("fixture cleanup failure")
	}
	delete(c.values, id)
	return nil
}
func (c *fixtureTargetCredentials) CleanupBackupTargetPasswords(ids []string) error {
	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	for id := range c.values {
		if !keep[id] {
			delete(c.values, id)
		}
	}
	return nil
}

func TestFanoutPersistsPartialSuccessAndRetriesOnlyFailedTarget(t *testing.T) {
	repo := &fanoutRepo{
		targets: map[string]Target{"good": {ID: "good", Name: "good target", Type: "local", Enabled: true}, "bad": {ID: "bad", Name: "bad target", Type: "local", Enabled: true}},
		task:    Task{ID: "task-1", TargetIDs: []string{"good", "bad"}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{},
	}
	var goodUploads, badUploads atomic.Int32
	var uploadedPackage []byte
	var failedPackage []byte
	goodContent := &uploadedPackage
	badContent := &failedPackage
	workDir := t.TempDir()
	service := NewService(repo, fanoutSource{}, func(target Target) (ObjectStore, error) {
		if target.ID == "good" {
			return fanoutStore{count: &goodUploads, content: goodContent}, nil
		}
		return fanoutStore{count: &badUploads, fail: badUploads.Load() == 0, content: badContent}, nil
	}, workDir, "test")
	snapshot, err := service.Create(context.Background(), "task-1", "fixture-backup-passphrase")
	var targetErr safeSummary
	if !errors.As(err, &targetErr) || !strings.Contains(targetErr.ErrorSummary(), "部分目标") {
		t.Fatalf("partial upload was not returned as a failure: snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Status != "partial_success" || snapshot.Targets[0].Status != "succeeded" || snapshot.Targets[1].Status != "failed" {
		t.Fatalf("partial result: %+v", snapshot)
	}
	if !bytes.Equal(uploadedPackage, failedPackage) {
		t.Fatal("fan-out targets did not receive identical encrypted bytes")
	}
	if strings.Contains(snapshot.Targets[1].Error, "fixture-password") {
		t.Fatal("target error leaked secret material")
	}
	if _, err := os.Stat(filepath.Join(workDir, "retry-"+snapshot.ID+".tgb")); err != nil {
		t.Fatalf("encrypted retry package missing: %v", err)
	}
	for _, path := range []string{"credentials/backup-targets.json", "documents/credentials/backup-targets.json"} {
		if _, err := os.Stat(filepath.Join(workDir, "backup", snapshot.ID, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("snapshot staging retained credential path %q: %v", path, err)
		}
	}
	badUploads.Store(1)
	retried, err := service.RetryFailed(context.Background(), snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != "succeeded" || goodUploads.Load() != 1 || badUploads.Load() != 2 {
		t.Fatalf("retry result=%+v uploads good=%d bad=%d", retried, goodUploads.Load(), badUploads.Load())
	}
	if _, err := os.Stat(filepath.Join(workDir, "retry-"+snapshot.ID+".tgb")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry package retained after success: %v", err)
	}
}

func TestScheduledTargetIntervalsAreIndependentAndHonorRecentFailures(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	task := Task{TargetIDs: []string{"default", "custom", "disabled"}, IntervalHours: 6, TargetIntervalHours: map[string]int{"custom": 3}}
	snapshots := []Snapshot{
		{ID: "default-last", CreatedAt: now.Add(-5 * time.Hour), Targets: []TargetRun{{TargetID: "default", Status: "succeeded"}}},
		{ID: "custom-last", CreatedAt: now.Add(-4 * time.Hour), Targets: []TargetRun{{TargetID: "custom", Status: "failed"}}},
		{ID: "disabled-last", CreatedAt: now.Add(-30 * time.Hour), Targets: []TargetRun{{TargetID: "disabled", Status: "succeeded"}}},
	}
	due := scheduledTargetIDs(task, snapshots, map[string]bool{"default": true, "custom": true, "disabled": false}, now)
	if len(due) != 1 || due[0] != "custom" {
		t.Fatalf("due targets = %v, want only custom interval target", due)
	}
	due = scheduledTargetIDs(task, snapshots, map[string]bool{"default": true, "custom": true}, now.Add(2*time.Hour))
	if len(due) != 2 || due[0] != "default" || due[1] != "custom" {
		t.Fatalf("due targets after 2 hours = %v, want default and custom", due)
	}
}

func TestScheduledTargetPendingRunWaitsForIntervalBeforeRetry(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	task := Task{TargetIDs: []string{"target"}, IntervalHours: 3}
	snapshots := []Snapshot{{
		ID: "interrupted-run", CreatedAt: now.Add(-time.Hour),
		Targets: []TargetRun{{TargetID: "target", Status: "pending"}},
	}}
	enabled := map[string]bool{"target": true}
	if due := scheduledTargetIDs(task, snapshots, enabled, now); len(due) != 0 {
		t.Fatalf("pending target due after 1 hour = %v, want no retry before interval", due)
	}
	if due := scheduledTargetIDs(task, snapshots, enabled, now.Add(2*time.Hour)); len(due) != 1 || due[0] != "target" {
		t.Fatalf("pending target due after 3 hours = %v, want target retry", due)
	}
}

func TestManualTargetSubsetUploadsOnlyDueDestination(t *testing.T) {
	repo := &fanoutRepo{
		targets: map[string]Target{"first": {ID: "first", Name: "first", Type: "local", Enabled: true}, "second": {ID: "second", Name: "second", Type: "local", Enabled: true}},
		task:    Task{ID: "task-1", TargetIDs: []string{"first", "second"}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{},
	}
	var firstUploads, secondUploads atomic.Int32
	service := NewService(repo, fanoutSource{}, func(target Target) (ObjectStore, error) {
		if target.ID == "first" {
			return fanoutStore{count: &firstUploads}, nil
		}
		return fanoutStore{count: &secondUploads}, nil
	}, t.TempDir(), "test")
	snapshot, err := service.CreateForTargets(context.Background(), "task-1", []string{"second"}, "fixture-backup-passphrase")
	if err != nil || snapshot.Status != "succeeded" || len(snapshot.Targets) != 1 || snapshot.Targets[0].TargetID != "second" || firstUploads.Load() != 0 || secondUploads.Load() != 1 {
		t.Fatalf("target subset was not isolated: snapshot=%+v uploads=(%d,%d) err=%v", snapshot, firstUploads.Load(), secondUploads.Load(), err)
	}
}

type failingSnapshotSource struct{ err error }

func (s failingSnapshotSource) Snapshot(context.Context, string, bool) ([]File, error) {
	return nil, s.err
}

func TestCreateReportsLocalSnapshotFailureBeforeAnyUpload(t *testing.T) {
	repo := &fanoutRepo{
		targets:   map[string]Target{"target": {ID: "target", Name: "Fixture", Type: "local", Enabled: true}},
		task:      Task{ID: "task-1", TargetIDs: []string{"target"}},
		snapshots: map[string]Snapshot{},
		runs:      map[string][]TargetRun{},
	}
	var uploads atomic.Int32
	service := NewService(repo, failingSnapshotSource{err: errors.New("fixture database snapshot failure")}, func(Target) (ObjectStore, error) {
		return fanoutStore{count: &uploads}, nil
	}, t.TempDir(), "test")

	snapshot, err := service.Create(context.Background(), "task-1", "fixture-backup-passphrase")
	var summary safeSummary
	if err == nil || !errors.As(err, &summary) || !strings.Contains(summary.ErrorSummary(), "尚未上传") {
		t.Fatalf("local snapshot failure was not explained: snapshot=%+v err=%v", snapshot, err)
	}
	if uploads.Load() != 0 || len(repo.snapshots) != 0 {
		t.Fatalf("local snapshot failure reached upload or created history: uploads=%d snapshots=%d", uploads.Load(), len(repo.snapshots))
	}
}

func TestRetryFailedPersistsSuccessfulTargetBeforeLaterFailure(t *testing.T) {
	repo := &fanoutRepo{
		targets:   map[string]Target{"good": {ID: "good", Name: "Good", Type: "local", Enabled: true}, "bad": {ID: "bad", Name: "Bad", Type: "local", Enabled: true}},
		task:      Task{ID: "task-1", TargetIDs: []string{"good", "bad"}},
		snapshots: map[string]Snapshot{"snapshot-retry": {ID: "snapshot-retry", TaskID: "task-1", Status: "partial_success", Targets: []TargetRun{{TargetID: "good", Name: "Good", Status: "succeeded"}, {TargetID: "bad", Name: "Bad", Status: "failed"}}}},
		runs:      map[string][]TargetRun{"snapshot-retry": {{TargetID: "good", Name: "Good", Status: "succeeded"}, {TargetID: "bad", Name: "Bad", Status: "failed"}}},
	}
	packagePath := filepath.Join(t.TempDir(), "retry-snapshot-retry.tgb")
	if err := os.WriteFile(packagePath, []byte("encrypted-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var goodUploads, badUploads atomic.Int32
	service := NewService(repo, fanoutSource{}, func(target Target) (ObjectStore, error) {
		if target.ID == "good" {
			return fanoutStore{count: &goodUploads}, nil
		}
		return fanoutStore{count: &badUploads, fail: true}, nil
	}, filepath.Dir(packagePath), "test")
	if _, err := service.RetryFailed(context.Background(), "snapshot-retry"); err != nil {
		t.Fatal(err)
	}
	if goodUploads.Load() != 0 || badUploads.Load() != 1 {
		t.Fatalf("retry touched wrong targets: good=%d bad=%d", goodUploads.Load(), badUploads.Load())
	}
	if runs := repo.runs["snapshot-retry"]; len(runs) != 2 || runs[0].Status != "succeeded" || runs[1].Status != "failed" {
		t.Fatalf("retry status was not persisted: %+v", runs)
	}
}

func TestRetryPackageCleanupKeepsMoreThanThreeActiveRunsAndMarksExpiredRun(t *testing.T) {
	workDir := t.TempDir()
	repo := &fanoutRepo{targets: map[string]Target{}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("snapshot-%d", i)
		path := filepath.Join(workDir, "retry-"+id+".tgb")
		if err := os.WriteFile(path, []byte("encrypted-fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, time.Now().Add(-8*24*time.Hour), time.Now().Add(-8*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if err := os.Chtimes(path, time.Now().Add(-8*24*time.Hour), time.Now().Add(-8*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
		repo.snapshots[id] = Snapshot{ID: id, TaskID: "task-1", Status: "partial_success", Targets: []TargetRun{{TargetID: "failed", Status: "failed"}}}
		repo.runs[id] = append([]TargetRun(nil), repo.snapshots[id].Targets...)
	}
	_ = NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, workDir, "test")
	for i := 2; i <= 4; i++ {
		if _, err := os.Stat(filepath.Join(workDir, fmt.Sprintf("retry-snapshot-%d.tgb", i))); err != nil {
			t.Fatalf("active retry package %d was deleted: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, "retry-snapshot-1.tgb")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired retry package retained: %v", err)
	}
	expired, err := repo.GetSnapshot(context.Background(), "snapshot-1")
	if err != nil || expired.RetryAvailable || expired.RetryUnavailableReason == "" {
		t.Fatalf("expired run was not marked unavailable: %+v %v", expired, err)
	}
}

func TestRetryAfterPackageExpiresPersistsUnavailableReason(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "retry-snapshot-expired.tgb")
	if err := os.WriteFile(path, []byte("encrypted-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	repo := &fanoutRepo{
		targets:   map[string]Target{},
		snapshots: map[string]Snapshot{"snapshot-expired": {ID: "snapshot-expired", TaskID: "task-1", Status: "failed", RetryAvailable: true, Targets: []TargetRun{{TargetID: "failed", Status: "failed"}}}},
		runs:      map[string][]TargetRun{"snapshot-expired": {{TargetID: "failed", Status: "failed"}}},
	}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, workDir, "test")
	snapshots, err := service.Snapshots(context.Background())
	if err != nil || len(snapshots) != 1 || snapshots[0].RetryAvailable || snapshots[0].RetryUnavailableReason == "" {
		t.Fatalf("expired package was not marked unavailable while service is running: %+v %v", snapshots, err)
	}
	saved, err := repo.GetSnapshot(context.Background(), "snapshot-expired")
	if err != nil || saved.RetryAvailable || saved.RetryUnavailableReason == "" {
		t.Fatalf("expired retry state did not persist: %+v %v", saved, err)
	}
}

type fanoutSource struct{}

func (fanoutSource) Snapshot(_ context.Context, stage string, _ bool) ([]File, error) {
	data := []byte("fixture database content")
	if err := os.WriteFile(filepath.Join(stage, "tangentry.db"), data, 0600); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return []File{{Path: "tangentry.db", Type: FileDatabase, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", digest)}}, nil
}

func TestWebDAVTargetCredentialIsNotReturnedOrStoredInRepository(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{}}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	target := Target{ID: "webdav-1", Name: "NAS", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Password: "fixture-password", UpdateCredential: true, Enabled: true}
	if err := service.SaveTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	saved := repo.targets[target.ID]
	if saved.Password != "" || saved.UpdateCredential || saved.Endpoint == "" || credentials.values[target.ID] != "fixture-password" {
		t.Fatal("credential handling did not isolate the password")
	}
	targets, err := service.Targets(context.Background())
	if err != nil || len(targets) == 0 || targets[0].Password != "" || !targets[0].CredentialConfigured {
		t.Fatalf("target query leaked credential or missed state: %+v err=%v", targets, err)
	}
	if err := service.DeleteTarget(context.Background(), target.ID); err != nil || len(credentials.values) != 0 {
		t.Fatalf("credential was not cleared: %v", err)
	}
}

func TestSaveWebDAVTargetIsLocalFastAndLogsRedactedStages(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentialStore, err := credentials.OpenWithKey(filepath.Join(t.TempDir(), "credentials"), "", "", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("not used") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentialStore)
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	service.SetTargetTester(func(context.Context, Target) error { t.Fatal("SaveTarget called remote test connection"); return nil })
	service.SetTargetFolderStores(func(Target) (TargetFolderStore, error) {
		t.Fatal("SaveTarget called remote folder operation")
		return nil, errors.New("unreachable")
	})
	target := Target{ID: "target-fixture", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/base", Username: "private-fixture-user", Password: "private-fixture-password", UpdateCredential: true, Enabled: true}
	started := time.Now()
	if err := service.SaveTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed >= time.Second {
		t.Fatalf("local target save took %s", elapsed)
	}
	t.Logf("local target save elapsed=%s timings=%s", elapsed, logs.String())
	for _, phase := range []string{"validation", "credential_write", "repository_lookup", "repository_write", "service_total", "encryption", "persistence"} {
		if !strings.Contains(logs.String(), "phase="+phase) {
			t.Errorf("missing timing phase %q in %s", phase, logs.String())
		}
	}
	for _, sensitive := range []string{"private-fixture-user", "private-fixture-password", "dav.example.test"} {
		if strings.Contains(logs.String(), sensitive) {
			t.Errorf("timing log contains sensitive field %q", sensitive)
		}
	}
}

func TestBrowseTargetLoadsSavedCredentialsAndRejectsInvalidPathBeforeFactory(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{"saved": {ID: "saved", Name: "Saved", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", AllowInsecureTLS: true}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{"saved": "fixture-password"}}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	var received Target
	store := &fixtureFolderStore{}
	service.SetTargetFolderStores(func(target Target) (TargetFolderStore, error) { received = target; return store, nil })
	folders, err := service.BrowseTarget(context.Background(), Target{ID: "saved", Type: "webdav"}, "")
	if err != nil || len(folders) != 1 {
		t.Fatalf("saved target browse failed: folders=%v err=%v", folders, err)
	}
	if received.Endpoint != repo.targets["saved"].Endpoint || received.Username != repo.targets["saved"].Username || received.Password != "fixture-password" || !received.AllowInsecureTLS {
		t.Fatalf("saved target configuration was not loaded safely: %+v", received)
	}
	if !store.closed {
		t.Fatal("folder store was not closed")
	}
	called := false
	service.SetTargetFolderStores(func(Target) (TargetFolderStore, error) { called = true; return &fixtureFolderStore{}, nil })
	if _, err := service.BrowseTarget(context.Background(), Target{ID: "saved", Type: "webdav", Password: "fixture-password"}, "../outside"); !errors.Is(err, ErrInvalidFolderPath) {
		t.Fatalf("invalid browse path error = %v", err)
	}
	if called {
		t.Fatal("invalid browse path reached folder store factory")
	}
}

func TestSavedTargetCredentialRequiresMatchingEndpointAndUsername(t *testing.T) {
	var firstAuthorization atomic.Value
	firstAuthorization.Store("")
	var firstRequests atomic.Int32
	var secondAuthorization atomic.Value
	secondAuthorization.Store("")
	var secondRequests atomic.Int32
	firstServer := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstRequests.Add(1)
		firstAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	secondServer := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondRequests.Add(1)
		secondAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	firstClient, secondClient := firstServer.Client(), secondServer.Client()
	client := &http.Client{Transport: targetCredentialHostTransport{
		firstHost:  "dav.example.test",
		secondHost: "other.example.test",
		first:      firstClient.Transport,
		second:     secondClient.Transport,
	}}
	repo := &fanoutRepo{targets: map[string]Target{"saved": {ID: "saved", Name: "Saved", Type: "webdav", Endpoint: "http://dav.example.test/dav/base", Username: "fixture-user", AllowInsecureTLS: true}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{"saved": "fixture-secret"}}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	var tested Target
	service.SetTargetTester(func(_ context.Context, target Target) error {
		tested = target
		request, err := http.NewRequest(http.MethodGet, target.Endpoint, nil)
		if err != nil {
			return err
		}
		request.SetBasicAuth(target.Username, target.Password)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		return response.Body.Close()
	})
	matchingEndpoint := "HTTP://DAV.EXAMPLE.TEST:80/dav/base/"
	requestedNormalized, requestedNormalizeErr := normalizeWebDAVEndpoint(matchingEndpoint)
	savedNormalized, savedNormalizeErr := normalizeWebDAVEndpoint(repo.targets["saved"].Endpoint)
	if requestedNormalizeErr != nil || savedNormalizeErr != nil || requestedNormalized != savedNormalized {
		t.Fatalf("endpoint normalization mismatch: request=%q saved=%q request_error=%v saved_error=%v", requestedNormalized, savedNormalized, requestedNormalizeErr, savedNormalizeErr)
	}
	if err := service.TestTarget(context.Background(), Target{ID: "saved", Type: "webdav", Endpoint: matchingEndpoint, Username: "fixture-user", AllowInsecureTLS: false}); err != nil {
		t.Fatalf("normalized saved target connection test failed: %v", err)
	}
	if tested.Password != "fixture-secret" || tested.Endpoint != repo.targets["saved"].Endpoint || tested.Username != repo.targets["saved"].Username || tested.AllowInsecureTLS {
		t.Fatalf("resolved target mismatch: endpoint=%q username=%q tls=%t password_set=%t", tested.Endpoint, tested.Username, tested.AllowInsecureTLS, tested.Password != "")
	}
	if got := firstAuthorization.Load().(string); got == "" || firstRequests.Load() != 1 {
		t.Fatalf("matching saved credentials did not reach the first local host: requests=%d authorization_set=%t", firstRequests.Load(), got != "")
	}
	if err := service.TestTarget(context.Background(), Target{ID: "saved", Type: "webdav", Endpoint: "http://dav.example.test/dav/other", Username: "fixture-user"}); !errors.Is(err, ErrTargetConnectionChanged) {
		t.Fatalf("changed endpoint path error = %v", err)
	}
	if err := service.TestTarget(context.Background(), Target{ID: "saved", Type: "webdav", Endpoint: "http://other.example.test/dav", Username: "fixture-user"}); !errors.Is(err, ErrTargetConnectionChanged) {
		t.Fatalf("changed endpoint error = %v", err)
	}
	if got := secondAuthorization.Load().(string); got != "" || secondRequests.Load() != 0 {
		t.Fatalf("stored credentials reached second host: requests=%d authorization_set=%t", secondRequests.Load(), got != "")
	}
	if err := service.TestTarget(context.Background(), Target{ID: "saved", Type: "webdav", Endpoint: "http://dav.example.test/dav/base", Username: "other-user"}); !errors.Is(err, ErrTargetConnectionChanged) {
		t.Fatalf("changed username error = %v", err)
	}

	factoryCalls := 0
	var folderTarget Target
	service.SetTargetFolderStores(func(target Target) (TargetFolderStore, error) {
		factoryCalls++
		folderTarget = target
		request, err := http.NewRequest(http.MethodGet, target.Endpoint, nil)
		if err != nil {
			return nil, err
		}
		request.SetBasicAuth(target.Username, target.Password)
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		_ = response.Body.Close()
		return &fixtureFolderStore{}, nil
	})
	matchingTarget := Target{ID: "saved", Type: "webdav", Endpoint: matchingEndpoint, Username: "fixture-user", AllowInsecureTLS: false}
	if _, err := service.BrowseTarget(context.Background(), matchingTarget, ""); err != nil {
		t.Fatalf("matching saved target browse failed: %v", err)
	}
	if folderTarget.Endpoint != repo.targets["saved"].Endpoint || folderTarget.Username != repo.targets["saved"].Username || folderTarget.Password != "fixture-secret" || folderTarget.AllowInsecureTLS {
		t.Fatalf("browse target mismatch: endpoint=%q username=%q tls=%t password_set=%t", folderTarget.Endpoint, folderTarget.Username, folderTarget.AllowInsecureTLS, folderTarget.Password != "")
	}
	if _, err := service.CreateTargetFolder(context.Background(), matchingTarget, "", "new-folder"); err != nil {
		t.Fatalf("matching saved target mkdir failed: %v", err)
	}
	if folderTarget.Endpoint != repo.targets["saved"].Endpoint || folderTarget.Username != repo.targets["saved"].Username || folderTarget.Password != "fixture-secret" || folderTarget.AllowInsecureTLS {
		t.Fatalf("mkdir target mismatch: endpoint=%q username=%q tls=%t password_set=%t", folderTarget.Endpoint, folderTarget.Username, folderTarget.AllowInsecureTLS, folderTarget.Password != "")
	}
	if firstRequests.Load() != 3 {
		t.Fatalf("matching saved target operations did not reach the first local host: requests=%d", firstRequests.Load())
	}
	factoryCalls = 0
	firstRequests.Store(0)
	firstAuthorization.Store("")
	secondRequests.Store(0)
	secondAuthorization.Store("")
	for _, request := range []Target{
		{ID: "saved", Type: "webdav", Endpoint: "http://dav.example.test/dav/other", Username: "fixture-user"},
		{ID: "saved", Type: "webdav", Endpoint: "http://other.example.test/dav", Username: "fixture-user"},
		{ID: "saved", Type: "webdav", Endpoint: "http://dav.example.test/dav/base", Username: "other-user"},
	} {
		if _, err := service.BrowseTarget(context.Background(), request, ""); !errors.Is(err, ErrTargetConnectionChanged) {
			t.Errorf("browse accepted changed target identity: %v", err)
		}
		if _, err := service.CreateTargetFolder(context.Background(), request, "", "new-folder"); !errors.Is(err, ErrTargetConnectionChanged) {
			t.Errorf("mkdir accepted changed target identity: %v", err)
		}
	}
	if factoryCalls != 0 {
		t.Fatalf("changed saved target identity reached remote folder store %d times", factoryCalls)
	}
	if firstRequests.Load() != 0 || firstAuthorization.Load().(string) != "" {
		t.Fatalf("changed saved target identity reached the first host: requests=%d authorization_set=%t", firstRequests.Load(), firstAuthorization.Load().(string) != "")
	}
	if got := secondAuthorization.Load().(string); got != "" || secondRequests.Load() != 0 {
		t.Fatalf("stored credentials reached second host during browse/mkdir: requests=%d authorization_set=%t", secondRequests.Load(), got != "")
	}
}

func TestSaveTargetLogsCredentialFailureAndCompensationPhases(t *testing.T) {
	t.Run("credential unavailable", func(t *testing.T) {
		repo := &fanoutRepo{targets: map[string]Target{}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
		service := NewService(repo, fanoutSource{}, nil, t.TempDir(), "test")
		var logs bytes.Buffer
		previousLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		t.Cleanup(func() { slog.SetDefault(previousLogger) })
		target := Target{ID: "missing-credentials", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Password: "fixture-secret", UpdateCredential: true}
		if err := service.SaveTarget(context.Background(), target); err == nil {
			t.Fatal("save without credential store succeeded")
		}
		for _, phase := range []string{"credential_read", "credential_write", "service_total"} {
			if !strings.Contains(logs.String(), "phase="+phase) {
				t.Errorf("missing phase %q in %s", phase, logs.String())
			}
		}
	})

	t.Run("repository failure compensates credential", func(t *testing.T) {
		repo := &fanoutRepo{targets: map[string]Target{}, failSave: errors.New("fixture database failure"), snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
		credentials := &fixtureTargetCredentials{values: map[string]string{"target-fixture": "old-secret"}}
		service := NewService(repo, fanoutSource{}, nil, t.TempDir(), "test")
		service.SetTargetCredentials(credentials)
		var logs bytes.Buffer
		previousLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		t.Cleanup(func() { slog.SetDefault(previousLogger) })
		target := Target{ID: "target-fixture", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Password: "new-secret", UpdateCredential: true}
		if err := service.SaveTarget(context.Background(), target); err == nil {
			t.Fatal("save with repository failure succeeded")
		}
		if credentials.values[target.ID] != "old-secret" {
			t.Fatal("credential compensation did not restore previous value")
		}
		if !strings.Contains(logs.String(), "phase=credential_compensation") {
			t.Fatalf("missing compensation timing log: %s", logs.String())
		}
	})

	t.Run("changed saved connection needs password", func(t *testing.T) {
		repo := &fanoutRepo{targets: map[string]Target{"target-fixture": {ID: "target-fixture", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/old", Username: "fixture-user"}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
		credentials := &fixtureTargetCredentials{values: map[string]string{"target-fixture": "fixture-secret"}}
		service := NewService(repo, fanoutSource{}, nil, t.TempDir(), "test")
		service.SetTargetCredentials(credentials)
		for _, target := range []Target{
			{ID: "target-fixture", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/new", Username: "fixture-user"},
			{ID: "target-fixture", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/old", Username: "other-user"},
		} {
			if err := service.SaveTarget(context.Background(), target); !errors.Is(err, ErrTargetConnectionChanged) {
				t.Errorf("saving changed endpoint or username without a password error = %v", err)
			}
			if credentials.values[target.ID] != "fixture-secret" {
				t.Error("rejected target edit changed its stored credential")
			}
		}
	})
}

func TestTargetCredentialCleanupFailureDoesNotFailDatabaseMutationAndStartupCleansOrphans(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{"live": {ID: "live", Name: "Live", Type: "webdav", Enabled: true}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{"live": "fixture-live", "orphan": "fixture-orphan"}, failDelete: true}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	if err := service.DeleteTarget(context.Background(), "live"); err != nil {
		t.Fatalf("credential cleanup failure leaked as target delete failure: %v", err)
	}
	if _, err := repo.GetTarget(context.Background(), "live"); err == nil {
		t.Fatal("database target was not deleted")
	}
	credentials.failDelete = false
	if err := service.CleanupOrphanTargetCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if credentials.BackupTargetConfigured("live") || credentials.BackupTargetConfigured("orphan") {
		t.Fatal("startup orphan cleanup retained credentials without matching WebDAV targets")
	}
}

func TestChangingTargetTypeDeletesDatabaseBeforeCredentialCleanup(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{"live": {ID: "live", Name: "Live", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", Enabled: true}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{"live": "fixture-live"}, failDelete: true}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	local := Target{ID: "live", Name: "Local", Type: "local", Path: "/tmp/backup-fixture", Enabled: true}
	if err := service.SaveTarget(context.Background(), local); err != nil {
		t.Fatalf("credential cleanup failure leaked as target save failure: %v", err)
	}
	saved, err := repo.GetTarget(context.Background(), "live")
	if err != nil || saved.Type != "local" || !credentials.BackupTargetConfigured("live") {
		t.Fatalf("database change or orphan fixture missing: target=%+v configured=%v err=%v", saved, credentials.BackupTargetConfigured("live"), err)
	}
	credentials.failDelete = false
	if err := service.CleanupOrphanTargetCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if credentials.BackupTargetConfigured("live") {
		t.Fatal("startup cleanup retained credential for non-WebDAV target")
	}
}

func TestSaveWebDAVTargetRejectsEndpointPathTraversal(t *testing.T) {
	repo := &fanoutRepo{targets: map[string]Target{}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{}}
	credentials := &fixtureTargetCredentials{values: map[string]string{"webdav-1": "fixture-password"}}
	service := NewService(repo, fanoutSource{}, func(Target) (ObjectStore, error) { return nil, errors.New("unused") }, t.TempDir(), "test")
	service.SetTargetCredentials(credentials)
	for _, endpoint := range []string{"https://dav.example.test/%2e%2e/private", "https://dav.example.test/a/../private", "https://dav.example.test/%252e%252e/private"} {
		target := Target{ID: "webdav-1", Name: "Fixture", Type: "webdav", Endpoint: endpoint, Username: "fixture-user", Enabled: true}
		if err := service.SaveTarget(context.Background(), target); err == nil {
			t.Fatalf("accepted traversing endpoint %q", endpoint)
		}
	}
	for _, remotePath := range []string{"../outside", "%2e%2e/outside", "/absolute"} {
		target := Target{ID: "webdav-1", Name: "Fixture", Type: "webdav", Endpoint: "https://dav.example.test/root", Username: "fixture-user", RemotePath: remotePath, Enabled: true}
		if err := service.SaveTarget(context.Background(), target); err == nil {
			t.Fatalf("accepted traversing remote prefix %q", remotePath)
		}
	}
}

func TestFanoutContinuesAfterEachTargetFactoryAndUploadError(t *testing.T) {
	repo := &fanoutRepo{
		targets: map[string]Target{"factory-error": {ID: "factory-error", Name: "Factory error", Type: "local", Enabled: true}, "upload-error": {ID: "upload-error", Name: "Upload error", Type: "local", Enabled: true}, "good": {ID: "good", Name: "Good", Type: "local", Enabled: true}},
		task:    Task{ID: "task-1", TargetIDs: []string{"factory-error", "upload-error", "good"}}, snapshots: map[string]Snapshot{}, runs: map[string][]TargetRun{},
	}
	var uploaded atomic.Int32
	service := NewService(repo, fanoutSource{}, func(target Target) (ObjectStore, error) {
		if target.ID == "factory-error" {
			return nil, errors.New("secret fixture-password")
		}
		return fanoutStore{count: &uploaded, fail: target.ID == "upload-error"}, nil
	}, t.TempDir(), "test")
	snapshot, err := service.Create(context.Background(), "task-1", "fixture-backup-passphrase")
	var targetErr safeSummary
	if !errors.As(err, &targetErr) || !strings.Contains(targetErr.ErrorSummary(), "部分目标") {
		t.Fatalf("failed uploads were not returned as a failure: snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.Status != "partial_success" || snapshot.Targets[0].Status != "failed" || snapshot.Targets[1].Status != "failed" || snapshot.Targets[2].Status != "succeeded" || uploaded.Load() != 2 {
		t.Fatalf("fan-out did not continue to final target: %+v uploads=%d", snapshot, uploaded.Load())
	}
}

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
