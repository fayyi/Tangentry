package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	digest := sha256.Sum256(payload)
	hash := fmt.Sprintf("%x", digest)
	manifest := Manifest{FormatVersion: FormatVersion, TangentryVersion: "test", SchemaVersion: SchemaVersion, SnapshotID: "snapshot-1", CreatedAt: time.Now().UTC(), Files: []File{{Path: "restored.txt", Type: FileData, Size: int64(len(payload)), SHA256: hash}}, KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: "00112233445566778899aabbccddeeff", Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	raw, _ := json.Marshal(manifest)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(raw)
	w, _ = zw.Create("restored.txt")
	_, _ = w.Write(payload)
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
	switchCalled := false
	service.SetRestoreSwitcher(func(context.Context, string) error { switchCalled = true; return nil })
	destination := filepath.Join(t.TempDir(), "new-data")
	result, err := service.Restore(context.Background(), "target-1", "snapshot-1", "fixture-password", destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pending_confirmation" || result.PreRestoreSnapshot == "" {
		t.Fatalf("unexpected restore plan: %+v", result)
	}
	if _, err := os.Stat(result.PreRestoreSnapshot); err != nil {
		t.Fatalf("pre-restore snapshot was not retained: %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination populated before confirmation: %v", err)
	}
	confirmed, err := service.ConfirmRestore(context.Background(), result.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "switched" || !switchCalled {
		t.Fatalf("restore was not switched: %+v", confirmed)
	}
	if _, err := os.Stat(filepath.Join(destination, "restored.txt")); err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
}
