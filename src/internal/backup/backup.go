// Package backup contains the client-side, versioned snapshot workflow.
// It deliberately knows nothing about SQLite, HTTP, or cloud SDKs.
package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	FormatVersion       = 1
	Magic               = "TGB1"
	ChunkSize           = 1024 * 1024
	SaltSize            = 16
	NonceSize           = 12
	KDFTime             = 3
	KDFMemory           = 64 * 1024
	KDFThreads          = 2
	KDFKeyLen           = 32
	SchemaVersion       = 69
	MaxArchiveSize      = 1 << 30
	MaxEntries          = 10000
	MaxFileSize         = 256 << 20
	MaxCompressionRatio = 1000
	headerSize          = 46
)

var (
	ErrInvalidPackage = errors.New("invalid backup package")
	ErrBadPassword    = errors.New("backup password rejected")
	ErrUnknownVersion = errors.New("unsupported backup format version")
)

type FileType string

const (
	FileDatabase FileType = "database"
	FileData     FileType = "data"
)

type File struct {
	Path   string   `json:"path"`
	Type   FileType `json:"type"`
	Size   int64    `json:"size"`
	SHA256 string   `json:"sha256"`
}

type Manifest struct {
	FormatVersion    int        `json:"formatVersion"`
	TangentryVersion string     `json:"tangentryVersion"`
	SchemaVersion    int        `json:"schemaVersion"`
	SnapshotID       string     `json:"snapshotID"`
	CreatedAt        time.Time  `json:"createdAt"`
	TaskID           string     `json:"taskID,omitempty"`
	IncludeDownloads bool       `json:"includeDownloads"`
	Files            []File     `json:"files"`
	KDF              KDFParams  `json:"kdf"`
	AEAD             AEADParams `json:"aead"`
}

type KDFParams struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Salt    string `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLength"`
}

type AEADParams struct {
	Name      string `json:"name"`
	Version   int    `json:"version"`
	ChunkSize int    `json:"chunkSize"`
	NonceSize int    `json:"nonceSize"`
}

// Source writes a complete, consistent copy of allowed application files to
// staging and returns the relative files that should enter the archive.
type Source interface {
	Snapshot(context.Context, string, bool) ([]File, error)
}

type ObjectStore interface {
	Upload(context.Context, string, io.Reader) error
	Read(context.Context, string) (io.ReadCloser, error)
	List(context.Context, string) ([]string, error)
	Delete(context.Context, string) error
	TestConnection(context.Context) error
}

type Target struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Enabled bool   `json:"enabled"`
}

type Task struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	TargetIDs        []string `json:"targetIds"`
	IncludeDownloads bool     `json:"includeDownloads"`
	Retention        int      `json:"retention"`
	Enabled          bool     `json:"enabled"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"taskId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
}

type Validation struct {
	SnapshotID string   `json:"snapshotId"`
	Manifest   Manifest `json:"manifest"`
	Size       int64    `json:"size"`
}

type RestoreResult struct {
	PlanID             string `json:"planId"`
	SnapshotID         string `json:"snapshotId"`
	Destination        string `json:"destination"`
	PreRestoreSnapshot string `json:"preRestoreSnapshot"`
	Status             string `json:"status"`
}

type restorePlan struct {
	RestoreResult
	TargetID    string
	ArchivePath string
	StagePath   string
}

type Repository interface {
	ListTargets(context.Context) ([]Target, error)
	GetTarget(context.Context, string) (Target, error)
	SaveTarget(context.Context, Target) error
	DeleteTarget(context.Context, string) error
	ListTasks(context.Context) ([]Task, error)
	GetTask(context.Context, string) (Task, error)
	SaveTask(context.Context, Task) error
	DeleteTask(context.Context, string) error
	SaveSnapshot(context.Context, Snapshot) error
	ListSnapshots(context.Context) ([]Snapshot, error)
}

type StoreFactory func(Target) (ObjectStore, error)

type Service struct {
	repo          Repository
	source        Source
	stores        StoreFactory
	workDir       string
	version       string
	mu            sync.Mutex
	plans         map[string]restorePlan
	switchRestore func(context.Context, string) error
}

func (s *Service) SetRestoreSwitcher(switcher func(context.Context, string) error) {
	s.switchRestore = switcher
}

func NewService(repo Repository, source Source, stores StoreFactory, workDir, version string) *Service {
	return &Service{repo: repo, source: source, stores: stores, workDir: workDir, version: version, plans: make(map[string]restorePlan)}
}

func (s *Service) Targets(ctx context.Context) ([]Target, error) { return s.repo.ListTargets(ctx) }
func (s *Service) Tasks(ctx context.Context) ([]Task, error)     { return s.repo.ListTasks(ctx) }
func (s *Service) Snapshots(ctx context.Context) ([]Snapshot, error) {
	return s.repo.ListSnapshots(ctx)
}
func (s *Service) SaveTarget(ctx context.Context, target Target) error {
	if target.ID == "" || target.Name == "" || target.Type != "local" || target.Path == "" {
		return errors.New("invalid backup target")
	}
	return s.repo.SaveTarget(ctx, target)
}
func (s *Service) SaveTask(ctx context.Context, task Task) error {
	if task.ID == "" || task.Name == "" || len(task.TargetIDs) == 0 {
		return errors.New("invalid backup task")
	}
	return s.repo.SaveTask(ctx, task)
}
func (s *Service) DeleteTarget(ctx context.Context, id string) error {
	return s.repo.DeleteTarget(ctx, id)
}
func (s *Service) DeleteTask(ctx context.Context, id string) error { return s.repo.DeleteTask(ctx, id) }

func (s *Service) Create(ctx context.Context, taskID, password string) (Snapshot, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return Snapshot{}, err
	}
	return s.create(ctx, task, password)
}

func (s *Service) create(ctx context.Context, task Task, password string) (Snapshot, error) {
	if password == "" {
		return Snapshot{}, errors.New("backup password is required")
	}
	id := randomID()
	stage, err := filepath.Abs(filepath.Join(s.workDir, "backup", id))
	if err != nil {
		return Snapshot{}, err
	}
	if err = ensureDir(stage); err != nil {
		return Snapshot{}, err
	}
	defer removeAll(stage)
	files, err := s.source.Snapshot(ctx, stage, task.IncludeDownloads)
	if err != nil {
		return Snapshot{}, err
	}
	salt := make([]byte, SaltSize)
	if _, err = rand.Read(salt); err != nil {
		return Snapshot{}, err
	}
	manifest := Manifest{FormatVersion: FormatVersion, TangentryVersion: s.version, SchemaVersion: SchemaVersion, SnapshotID: id, CreatedAt: time.Now().UTC(), TaskID: task.ID, IncludeDownloads: task.IncludeDownloads, Files: files, KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: hex.EncodeToString(salt), Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}}
	archive, err := archiveFiles(stage, &manifest)
	if err != nil {
		return Snapshot{}, err
	}
	encoded, err := Encrypt(archive, password, &manifest)
	if err != nil {
		return Snapshot{}, err
	}
	name := "tangentry/v1/snapshots/" + id + ".tgb"
	for _, targetID := range task.TargetIDs {
		target, e := s.repo.GetTarget(ctx, targetID)
		if e != nil {
			return Snapshot{}, e
		}
		if !target.Enabled {
			continue
		}
		store, e := s.stores(target)
		if e != nil {
			return Snapshot{}, e
		}
		if e = store.Upload(ctx, name, bytes.NewReader(encoded)); e != nil {
			return Snapshot{}, e
		}
	}
	snap := Snapshot{ID: id, TaskID: task.ID, CreatedAt: manifest.CreatedAt, Size: int64(len(encoded)), Status: "succeeded"}
	if err = s.repo.SaveSnapshot(ctx, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (s *Service) Validate(ctx context.Context, targetID, snapshotID, password string) (Validation, error) {
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return Validation{}, err
	}
	store, err := s.stores(target)
	if err != nil {
		return Validation{}, err
	}
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return Validation{}, err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return Validation{}, err
	}
	plain, err := Decrypt(b, password)
	if err != nil {
		return Validation{}, err
	}
	manifest, err := validateArchive(plain, snapshotID)
	if err != nil {
		return Validation{}, err
	}
	return Validation{SnapshotID: snapshotID, Manifest: manifest, Size: int64(len(b))}, nil
}

func (s *Service) Restore(ctx context.Context, targetID, snapshotID, password, destination string) (RestoreResult, error) {
	if !filepath.IsAbs(destination) {
		return RestoreResult{}, errors.New("restore destination must be an absolute directory")
	}
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return RestoreResult{}, err
	}
	store, err := s.stores(target)
	if err != nil {
		return RestoreResult{}, err
	}
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return RestoreResult{}, err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return RestoreResult{}, err
	}
	plain, err := Decrypt(b, password)
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err := validateArchive(plain, snapshotID); err != nil {
		return RestoreResult{}, err
	}
	if s.source == nil {
		return RestoreResult{}, errors.New("restore source unavailable")
	}
	if _, statErr := os.Stat(destination); statErr == nil {
		return RestoreResult{}, errors.New("restore destination must not already exist")
	} else if !os.IsNotExist(statErr) {
		return RestoreResult{}, statErr
	}
	preflight := filepath.Join(s.workDir, "restore", randomID())
	if err := ensureDir(preflight); err != nil {
		return RestoreResult{}, err
	}
	if _, err := s.source.Snapshot(ctx, preflight, false); err != nil {
		return RestoreResult{}, fmt.Errorf("create pre-restore snapshot: %w", err)
	}
	archivePath := filepath.Join(s.workDir, "restore", "plan-"+randomID()+".zip")
	if err := os.WriteFile(archivePath, plain, 0600); err != nil {
		return RestoreResult{}, err
	}
	stagePath := filepath.Join(s.workDir, "restore", "stage-"+randomID())
	if err := extractArchive(plain, stagePath); err != nil {
		removeAll(archivePath)
		removeAll(stagePath)
		return RestoreResult{}, fmt.Errorf("stage restored data: %w", err)
	}
	result := RestoreResult{PlanID: randomID(), SnapshotID: snapshotID, Destination: destination, PreRestoreSnapshot: preflight, Status: "pending_confirmation"}
	s.mu.Lock()
	s.plans[result.PlanID] = restorePlan{RestoreResult: result, TargetID: targetID, ArchivePath: archivePath, StagePath: stagePath}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) ConfirmRestore(ctx context.Context, planID string) (RestoreResult, error) {
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	s.mu.Lock()
	plan, ok := s.plans[planID]
	s.mu.Unlock()
	if !ok {
		return RestoreResult{}, errors.New("restore plan not found")
	}
	data, err := os.ReadFile(plan.ArchivePath)
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err = validateArchive(data, plan.SnapshotID); err != nil {
		return RestoreResult{}, err
	}
	if info, statErr := os.Lstat(plan.Destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() || info.Mode().IsRegular() {
			return RestoreResult{}, errors.New("restore destination must not already exist")
		}
		return RestoreResult{}, ErrInvalidPackage
	} else if !os.IsNotExist(statErr) {
		return RestoreResult{}, statErr
	}
	// Rebuild the staged directory after the second validation, so any
	// tampering with the preflight staging tree is discarded before activation.
	removeAll(plan.StagePath)
	if err = extractArchive(data, plan.StagePath); err != nil {
		removeAll(plan.StagePath)
		return RestoreResult{}, err
	}
	if err = os.Rename(plan.StagePath, plan.Destination); err != nil {
		removeAll(plan.StagePath)
		return RestoreResult{}, err
	}
	if s.switchRestore != nil {
		if err := s.switchRestore(ctx, plan.Destination); err != nil {
			return RestoreResult{}, err
		}
		plan.Status = "switched"
	} else {
		plan.Status = "restored_new_location"
	}
	// Keep the encrypted plan until switching succeeds so a transient runtime
	// failure can be retried without asking for the password again.
	removeAll(plan.ArchivePath)
	s.mu.Lock()
	s.plans[planID] = plan
	s.mu.Unlock()
	return plan.RestoreResult, nil
}

func Encrypt(archive []byte, password string, manifest *Manifest) ([]byte, error) {
	salt, err := hex.DecodeString(manifest.KDF.Salt)
	if len(salt) != SaltSize {
		salt = make([]byte, SaltSize)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		manifest.KDF.Salt = hex.EncodeToString(salt)
	}
	key := argon2.IDKey([]byte(password), salt, KDFTime, KDFMemory, KDFThreads, KDFKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString(Magic)
	out.WriteByte(byte(FormatVersion))
	out.Write(salt)
	binary.Write(&out, binary.BigEndian, uint32(KDFTime))
	binary.Write(&out, binary.BigEndian, uint32(KDFMemory))
	out.WriteByte(byte(KDFThreads))
	binary.Write(&out, binary.BigEndian, uint32(KDFKeyLen))
	binary.Write(&out, binary.BigEndian, uint32(ChunkSize))
	binary.Write(&out, binary.BigEndian, uint64(len(archive)))
	header := append([]byte(nil), out.Bytes()...)
	chunks := (len(archive) + ChunkSize - 1) / ChunkSize
	if chunks == 0 {
		chunks = 1
	}
	for offset, index := 0, uint64(0); index < uint64(chunks); offset, index = offset+ChunkSize, index+1 {
		end := min(offset+ChunkSize, len(archive))
		nonce := make([]byte, NonceSize)
		binary.BigEndian.PutUint64(nonce[4:], index)
		sealed := aead.Seal(nil, nonce, archive[offset:end], header)
		binary.Write(&out, binary.BigEndian, uint32(len(sealed)))
		out.Write(sealed)
	}
	return out.Bytes(), nil
}

func Decrypt(data []byte, password string) ([]byte, error) {
	if len(data) < headerSize || string(data[:4]) != Magic || int(data[4]) != FormatVersion {
		return nil, ErrInvalidPackage
	}
	salt := data[5 : 5+SaltSize]
	timeParam := binary.BigEndian.Uint32(data[21:25])
	memoryParam := binary.BigEndian.Uint32(data[25:29])
	threadsParam := data[29]
	keyLenParam := binary.BigEndian.Uint32(data[30:34])
	chunk := binary.BigEndian.Uint32(data[34:38])
	original := binary.BigEndian.Uint64(data[38:46])
	if original > MaxArchiveSize {
		return nil, ErrInvalidPackage
	}
	if timeParam != KDFTime || memoryParam != KDFMemory || threadsParam != KDFThreads || keyLenParam != KDFKeyLen {
		return nil, ErrInvalidPackage
	}
	if chunk == 0 || chunk > 64*1024*1024 {
		return nil, ErrInvalidPackage
	}
	key := argon2.IDKey([]byte(password), salt, KDFTime, KDFMemory, KDFThreads, KDFKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	header := append([]byte(nil), data[:headerSize]...)
	pos := headerSize
	var index uint64
	expectedChunks := (original + uint64(chunk) - 1) / uint64(chunk)
	if expectedChunks == 0 {
		expectedChunks = 1
	}
	for index < expectedChunks {
		if pos+4 > len(data) {
			return nil, ErrInvalidPackage
		}
		size := int(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		if size < aead.Overhead() || pos+size > len(data) {
			return nil, ErrInvalidPackage
		}
		nonce := make([]byte, NonceSize)
		binary.BigEndian.PutUint64(nonce[4:], index)
		plain, e := aead.Open(nil, nonce, data[pos:pos+size], header)
		if e != nil {
			return nil, ErrBadPassword
		}
		out.Write(plain)
		if len(plain) > int(chunk) || out.Len() > int(original) {
			return nil, ErrInvalidPackage
		}
		pos += size
		index++
	}
	if out.Len() != int(original) || pos != len(data) {
		return nil, ErrInvalidPackage
	}
	return out.Bytes(), nil
}

func archiveFiles(stage string, manifest *Manifest) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(raw); err != nil {
		return nil, err
	}
	for _, file := range manifest.Files {
		clean, err := cleanRelative(file.Path)
		if err != nil {
			return nil, err
		}
		src := filepath.Join(stage, filepath.FromSlash(clean))
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() || hasMultipleLinks(info) {
			return nil, ErrInvalidPackage
		}
		inFile, err := open(src)
		if err != nil {
			return nil, err
		}
		in, err := io.ReadAll(inFile)
		inFile.Close()
		if err != nil {
			return nil, err
		}
		w, err := zw.Create(clean)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(in); err != nil {
			return nil, err
		}
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func extractArchive(data []byte, destination string) error {
	if len(data) > MaxArchiveSize {
		return fmt.Errorf("archive exceeds limit: %w", ErrInvalidPackage)
	}
	if err := safeDestinationDir(destination); err != nil {
		return fmt.Errorf("restore destination: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if len(zr.File) > MaxEntries {
		return fmt.Errorf("too many entries: %w", ErrInvalidPackage)
	}
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 || f.FileInfo().Mode()&os.ModeNamedPipe != 0 || f.FileInfo().Mode()&os.ModeDevice != 0 {
			return fmt.Errorf("unsafe entry %q: %w", f.Name, ErrInvalidPackage)
		}
		if f.UncompressedSize64 > MaxFileSize || f.UncompressedSize64 > uint64(MaxCompressionRatio)*max64(f.CompressedSize64, 1) {
			return fmt.Errorf("entry limits %q: %w", f.Name, ErrInvalidPackage)
		}
		total += f.UncompressedSize64
		if total > MaxArchiveSize {
			return fmt.Errorf("total size: %w", ErrInvalidPackage)
		}
		clean, e := cleanRelative(f.Name)
		if e != nil {
			return fmt.Errorf("restore path %q: %w", f.Name, e)
		}
		if clean == "manifest.json" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		if !within(destination, target) {
			return fmt.Errorf("target outside destination: %w", ErrInvalidPackage)
		}
		if err = safeRestoreParents(destination, filepath.Dir(target)); err != nil {
			return fmt.Errorf("restore parent %q: %w", filepath.Dir(target), err)
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		if existing, statErr := os.Lstat(target); statErr == nil && existing.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("target symlink: %w", ErrInvalidPackage)
		}
		out, e := create(target)
		if e == nil {
			written, copyErr := io.Copy(out, io.LimitReader(r, MaxFileSize+1))
			if copyErr != nil {
				e = copyErr
			} else if written > MaxFileSize || written != int64(f.UncompressedSize64) {
				e = ErrInvalidPackage
			}
			out.Close()
		}
		r.Close()
		if e != nil {
			if errors.Is(e, os.ErrExist) {
				return fmt.Errorf("restore target already exists: %w", e)
			}
			return fmt.Errorf("write restore target %q: %w", target, e)
		}
	}
	return nil
}

func validateArchive(data []byte, snapshotID string) (Manifest, error) {
	if len(data) > MaxArchiveSize {
		return Manifest{}, ErrInvalidPackage
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Manifest{}, err
	}
	if len(zr.File) > MaxEntries {
		return Manifest{}, ErrInvalidPackage
	}
	var manifest Manifest
	found := false
	files := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 || f.FileInfo().Mode()&os.ModeNamedPipe != 0 || f.FileInfo().Mode()&os.ModeDevice != 0 {
			return Manifest{}, ErrInvalidPackage
		}
		if f.UncompressedSize64 > MaxFileSize || f.UncompressedSize64 > uint64(MaxCompressionRatio)*max64(f.CompressedSize64, 1) {
			return Manifest{}, ErrInvalidPackage
		}
		total += f.UncompressedSize64
		if total > MaxArchiveSize {
			return Manifest{}, ErrInvalidPackage
		}
		if f.Name != "manifest.json" {
			continue
		}
		if found || f.UncompressedSize64 > 1<<20 {
			return Manifest{}, ErrInvalidPackage
		}
		r, e := f.Open()
		if e != nil {
			return Manifest{}, e
		}
		e = json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&manifest)
		r.Close()
		if e != nil {
			return Manifest{}, ErrInvalidPackage
		}
		found = true
	}
	if !found || manifest.FormatVersion != FormatVersion || manifest.SnapshotID != snapshotID || manifest.SchemaVersion <= 0 || manifest.SchemaVersion > SchemaVersion {
		return Manifest{}, ErrUnknownVersion
	}
	if manifest.KDF.Name != "Argon2id" || manifest.KDF.Version != 1 || manifest.KDF.Time != KDFTime || manifest.KDF.Memory != KDFMemory || manifest.KDF.Threads != KDFThreads || manifest.KDF.KeyLen != KDFKeyLen || manifest.AEAD.Name != "AES-256-GCM" || manifest.AEAD.Version != 1 || manifest.AEAD.ChunkSize != ChunkSize || manifest.AEAD.NonceSize != NonceSize {
		return Manifest{}, ErrUnknownVersion
	}
	if salt, saltErr := hex.DecodeString(manifest.KDF.Salt); saltErr != nil || len(salt) != SaltSize {
		return Manifest{}, ErrInvalidPackage
	}
	if len(manifest.Files) == 0 || len(manifest.Files)+1 != len(zr.File) {
		return Manifest{}, ErrInvalidPackage
	}
	expected := map[string]bool{"manifest.json": true}
	for _, f := range manifest.Files {
		clean, e := cleanRelative(f.Path)
		if e != nil || clean != f.Path || files[clean] || expected[clean] || f.Size < 0 || f.Size > MaxFileSize || len(f.SHA256) != sha256.Size*2 {
			return Manifest{}, ErrInvalidPackage
		}
		if _, e := hex.DecodeString(f.SHA256); e != nil {
			return Manifest{}, ErrInvalidPackage
		}
		files[clean] = true
		expected[clean] = true
		foundFile := false
		for _, entry := range zr.File {
			if entry.Name != clean {
				continue
			}
			foundFile = true
			if entry.FileInfo().Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 != uint64(f.Size) {
				return Manifest{}, ErrInvalidPackage
			}
			reader, re := entry.Open()
			if re != nil {
				return Manifest{}, re
			}
			h := sha256.New()
			n, re := io.Copy(h, io.LimitReader(reader, MaxFileSize+1))
			reader.Close()
			if re != nil || n != f.Size || fmt.Sprintf("%x", h.Sum(nil)) != strings.ToLower(f.SHA256) {
				return Manifest{}, ErrInvalidPackage
			}
			break
		}
		if !foundFile {
			return Manifest{}, ErrInvalidPackage
		}
	}
	for _, entry := range zr.File {
		if !expected[entry.Name] {
			return Manifest{}, ErrInvalidPackage
		}
	}
	return manifest, nil
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func hasMultipleLinks(info os.FileInfo) bool {
	sys := info.Sys()
	if sys == nil {
		return false
	}
	v := reflect.ValueOf(sys)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	links := v.FieldByName("Nlink")
	if !links.IsValid() || !links.CanUint() {
		return false
	}
	return links.Uint() > 1
}

func safeMkdirParents(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	parts := strings.Split(abs, string(filepath.Separator))
	current := string(filepath.Separator)
	if len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			if e = os.Mkdir(current, 0700); e != nil {
				return e
			}
			continue
		}
		if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidPackage
		}
	}
	return nil
}

// safeDestinationDir permits already-existing OS-level ancestors (for
// example /var -> /private/var on macOS), but never follows a symlink inside
// the restore destination itself.
func safeDestinationDir(destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	if err := safeMkdirParentsAllowSymlink(parent); err != nil {
		return err
	}
	if info, statErr := os.Lstat(abs); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidPackage
		}
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return os.Mkdir(abs, 0700)
}

func safeMkdirParentsAllowSymlink(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	parts := strings.Split(abs, string(filepath.Separator))
	current := string(filepath.Separator)
	if len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			if e = os.Mkdir(current, 0700); e != nil {
				return e
			}
			continue
		}
		if e != nil || (!info.IsDir() && info.Mode()&os.ModeSymlink == 0) {
			return ErrInvalidPackage
		}
	}
	return nil
}

func safeRestoreParents(root, dir string) error {
	root, _ = filepath.Abs(root)
	dir, _ = filepath.Abs(dir)
	if !within(root, dir) {
		return ErrInvalidPackage
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPackage
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			if e = os.Mkdir(current, 0700); e != nil {
				return e
			}
			continue
		}
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidPackage
		}
	}
	return nil
}

func cleanRelative(name string) (string, error) {
	p := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return "", ErrInvalidPackage
	}
	return p, nil
}
func within(root, target string) bool {
	r, _ := filepath.Abs(root)
	t, _ := filepath.Abs(target)
	return t == r || strings.HasPrefix(t, r+string(filepath.Separator))
}
func randomID() string            { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func ensureDir(path string) error { return mkdirAll(path, 0700) }
func removeAll(path string)       { _ = remove(path) }

// Small indirections keep the package easy to exercise with filesystem fakes.
var mkdirAll = func(name string, perm uint32) error { return osMkdirAll(name, perm) }
var remove = func(name string) error { return osRemoveAll(name) }
var open = func(name string) (io.ReadCloser, error) { return osOpen(name) }
var create = func(name string) (io.WriteCloser, error) { return osCreate(name) }

// Implementations are kept here to avoid exposing OS helpers in the domain API.
func osMkdirAll(name string, perm uint32) error { return os.MkdirAll(name, os.FileMode(perm)) }
func osRemoveAll(name string) error             { return os.RemoveAll(name) }
func osOpen(name string) (io.ReadCloser, error) { return os.Open(name) }
func osCreate(name string) (io.WriteCloser, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
