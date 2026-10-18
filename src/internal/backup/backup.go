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
	"sort"
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
	MaxPackageSize      = MaxArchiveSize + (MaxArchiveSize/ChunkSize+2)*64 + headerSize
	MaxEntries          = 10000
	MaxFileSize         = 256 << 20
	MaxCompressionRatio = 1000
	MaxRestorePlans     = 3
	RestorePlanTTL      = 24 * time.Hour
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
	PlanID               string `json:"planId"`
	SnapshotID           string `json:"snapshotId"`
	StagedID             string `json:"stagedId"`
	PreRestoreSnapshotID string `json:"preRestoreSnapshotId"`
	Status               string `json:"status"`
	RestartRequired      bool   `json:"restartRequired"`
}

type restorePlan struct {
	RestoreResult
	TargetID      string
	StagePath     string
	PreflightPath string
	CreatedAt     time.Time
	Manifest      Manifest
}

func manifestDatabase(files []File) (File, bool) {
	for _, file := range files {
		if file.Path == "tangentry.db" && file.Type == FileDatabase {
			return file, true
		}
	}
	return File{}, false
}

func validateManifestFiles(files []File) error {
	if len(files) == 0 || len(files)+1 > MaxEntries {
		return ErrInvalidPackage
	}
	var total int64
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		clean, err := cleanRelative(file.Path)
		if err != nil || clean != file.Path || seen[clean] || file.Size < 0 || file.Size > MaxFileSize {
			return ErrInvalidPackage
		}
		if file.Path == "tangentry.db" {
			if file.Type != FileDatabase {
				return ErrInvalidPackage
			}
		} else if file.Type != FileData {
			return ErrInvalidPackage
		}
		seen[clean] = true
		total += file.Size
		if total > MaxArchiveSize {
			return ErrInvalidPackage
		}
	}
	if _, ok := manifestDatabase(files); !ok {
		return ErrInvalidPackage
	}
	return nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrInvalidPackage
	}
	return data, nil
}

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
}

func chmodTree(root string, fileMode, dirMode os.FileMode) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(path, dirMode)
		}
		return os.Chmod(path, fileMode)
	})
}

func validateStaged(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPackage
	}
	dbInfo, err := os.Lstat(filepath.Join(path, "tangentry.db"))
	if err != nil || !dbInfo.Mode().IsRegular() || dbInfo.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPackage
	}
	return nil
}

func planPath(workDir, id string) string { return filepath.Join(workDir, "plan-"+id+".json") }

func (s *Service) savePlan(plan restorePlan) error {
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(planPath(s.workDir, plan.PlanID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s *Service) cleanExpiredPlans(now time.Time) error {
	entries, err := os.ReadDir(s.workDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type pending struct {
		path string
		plan restorePlan
	}
	var pendingPlans []pending
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "plan-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.workDir, entry.Name())
		var plan restorePlan
		data, readErr := os.ReadFile(path)
		if readErr != nil || json.Unmarshal(data, &plan) != nil || now.Sub(plan.CreatedAt) > RestorePlanTTL {
			removeAll(path)
			if plan.StagePath != "" {
				removeAll(plan.StagePath)
			}
			if plan.PreflightPath != "" {
				removeAll(plan.PreflightPath)
			}
			continue
		}
		pendingPlans = append(pendingPlans, pending{path: path, plan: plan})
	}
	sort.Slice(pendingPlans, func(i, j int) bool { return pendingPlans[i].plan.CreatedAt.Before(pendingPlans[j].plan.CreatedAt) })
	for len(pendingPlans) > MaxRestorePlans {
		oldest := pendingPlans[0]
		pendingPlans = pendingPlans[1:]
		removeAll(oldest.path)
		removeAll(oldest.plan.StagePath)
		removeAll(oldest.plan.PreflightPath)
	}
	return nil
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
	repo    Repository
	source  Source
	stores  StoreFactory
	workDir string
	version string
	mu      sync.Mutex
	plans   map[string]restorePlan
}

func NewService(repo Repository, source Source, stores StoreFactory, workDir, version string) *Service {
	_ = cleanupRestoreWorkDir(workDir)
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
	if err := ensurePrivateDir(s.workDir); err != nil {
		return Snapshot{}, err
	}
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
	if err := validateManifestFiles(manifest.Files); err != nil {
		return Snapshot{}, fmt.Errorf("backup source omitted tangentry.db")
	}
	archivePath := filepath.Join(s.workDir, "archive-"+id+".zip")
	archive, err := os.OpenFile(archivePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Snapshot{}, err
	}
	defer removeAll(archivePath)
	if err := archiveFilesTo(stage, &manifest, archive); err != nil {
		archive.Close()
		return Snapshot{}, err
	}
	archiveInfo, err := archive.Stat()
	if err != nil || archiveInfo.Size() > MaxArchiveSize {
		archive.Close()
		return Snapshot{}, ErrInvalidPackage
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		archive.Close()
		return Snapshot{}, err
	}
	packagePath := filepath.Join(s.workDir, "package-"+id+".tgb")
	packageFile, err := os.OpenFile(packagePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		archive.Close()
		return Snapshot{}, err
	}
	defer removeAll(packagePath)
	defer packageFile.Close()
	if err := EncryptReader(archive, archiveInfo.Size(), password, &manifest, packageFile); err != nil {
		archive.Close()
		packageFile.Close()
		return Snapshot{}, err
	}
	if err := archive.Close(); err != nil {
		packageFile.Close()
		return Snapshot{}, err
	}
	if err := packageFile.Sync(); err != nil {
		packageFile.Close()
		return Snapshot{}, err
	}
	packageInfo, err := packageFile.Stat()
	if err != nil || packageInfo.Size() > MaxPackageSize {
		packageFile.Close()
		return Snapshot{}, ErrInvalidPackage
	}
	if _, err := packageFile.Seek(0, io.SeekStart); err != nil {
		packageFile.Close()
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
			packageFile.Close()
			return Snapshot{}, e
		}
		defer closeStore(store)
		if _, e = packageFile.Seek(0, io.SeekStart); e != nil {
			packageFile.Close()
			return Snapshot{}, e
		}
		if e = store.Upload(ctx, name, packageFile); e != nil {
			packageFile.Close()
			return Snapshot{}, e
		}
	}
	if err := packageFile.Close(); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{ID: id, TaskID: task.ID, CreatedAt: manifest.CreatedAt, Size: packageInfo.Size(), Status: "succeeded"}
	if err = s.repo.SaveSnapshot(ctx, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (s *Service) Validate(ctx context.Context, targetID, snapshotID, password string) (Validation, error) {
	if !validSnapshotID(snapshotID) {
		return Validation{}, ErrInvalidPackage
	}
	if err := ensurePrivateDir(s.workDir); err != nil {
		return Validation{}, err
	}
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return Validation{}, err
	}
	store, err := s.stores(target)
	if err != nil {
		return Validation{}, err
	}
	defer closeStore(store)
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return Validation{}, err
	}
	defer r.Close()
	plainPath := filepath.Join(s.workDir, "validate-"+randomID()+".zip")
	plainFile, err := os.OpenFile(plainPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Validation{}, err
	}
	defer removeAll(plainPath)
	defer plainFile.Close()
	packageSize, err := DecryptReader(r, password, plainFile)
	if err != nil {
		return Validation{}, err
	}
	if _, err := plainFile.Seek(0, io.SeekStart); err != nil {
		return Validation{}, err
	}
	plainInfo, err := plainFile.Stat()
	if err != nil {
		return Validation{}, err
	}
	manifest, err := validateArchiveReader(plainFile, plainInfo.Size(), snapshotID)
	if err != nil {
		return Validation{}, err
	}
	return Validation{SnapshotID: snapshotID, Manifest: manifest, Size: packageSize}, nil
}

func (s *Service) Restore(ctx context.Context, targetID, snapshotID, password, destination string) (RestoreResult, error) {
	if !validSnapshotID(snapshotID) {
		return RestoreResult{}, ErrInvalidPackage
	}
	_ = destination // retained for wire compatibility; restart applies to the active data directory.
	if err := s.cleanExpiredPlans(time.Now()); err != nil {
		return RestoreResult{}, err
	}
	pending, err := pendingPlanCount(s.workDir)
	if err != nil {
		return RestoreResult{}, err
	}
	if pending >= MaxRestorePlans {
		return RestoreResult{}, errors.New("too many pending restore plans")
	}
	if err := ensurePrivateDir(s.workDir); err != nil {
		return RestoreResult{}, err
	}
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return RestoreResult{}, err
	}
	store, err := s.stores(target)
	if err != nil {
		return RestoreResult{}, err
	}
	defer closeStore(store)
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return RestoreResult{}, err
	}
	defer r.Close()
	if err := ensurePrivateDir(s.workDir); err != nil {
		return RestoreResult{}, err
	}
	plainPath := filepath.Join(s.workDir, "restore-"+randomID()+".zip")
	plainFile, err := os.OpenFile(plainPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return RestoreResult{}, err
	}
	defer removeAll(plainPath)
	defer plainFile.Close()
	_, err = DecryptReader(r, password, plainFile)
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err := plainFile.Seek(0, io.SeekStart); err != nil {
		return RestoreResult{}, err
	}
	plainInfo, err := plainFile.Stat()
	if err != nil {
		return RestoreResult{}, err
	}
	validatedManifest, err := validateArchiveReader(plainFile, plainInfo.Size(), snapshotID)
	if err != nil {
		return RestoreResult{}, err
	}
	if s.source == nil {
		return RestoreResult{}, errors.New("restore source unavailable")
	}
	if err := ensurePrivateDir(s.workDir); err != nil {
		return RestoreResult{}, err
	}
	id := randomID()
	preflightID := randomID()
	preflight := filepath.Join(s.workDir, "pre-"+preflightID)
	if err := ensurePrivateDir(preflight); err != nil {
		return RestoreResult{}, err
	}
	if _, err := s.source.Snapshot(ctx, preflight, false); err != nil {
		removeAll(preflight)
		return RestoreResult{}, fmt.Errorf("create pre-restore snapshot: %w", err)
	}
	if err := chmodTree(preflight, 0600, 0700); err != nil {
		removeAll(preflight)
		return RestoreResult{}, err
	}
	stagePath := filepath.Join(s.workDir, "staged-"+id)
	if _, err := plainFile.Seek(0, io.SeekStart); err != nil {
		removeAll(preflight)
		return RestoreResult{}, err
	}
	if err := extractArchiveReader(plainFile, plainInfo.Size(), stagePath); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, fmt.Errorf("stage restored data: %w", err)
	}
	if err := chmodTree(stagePath, 0600, 0700); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, err
	}
	result := RestoreResult{PlanID: id, SnapshotID: snapshotID, StagedID: id, PreRestoreSnapshotID: preflightID, Status: "pending_confirmation"}
	plan := restorePlan{RestoreResult: result, TargetID: targetID, StagePath: stagePath, PreflightPath: preflight, CreatedAt: time.Now().UTC(), Manifest: validatedManifest}
	if err := s.savePlan(plan); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, err
	}
	s.mu.Lock()
	s.plans[result.PlanID] = plan
	s.mu.Unlock()
	return result, nil
}

func pendingPlanCount(workDir string) (int, error) {
	entries, err := os.ReadDir(workDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "plan-") && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count, nil
}

func (s *Service) ConfirmRestore(ctx context.Context, planID string) (RestoreResult, error) {
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	if !validRestoreID(planID) {
		return RestoreResult{}, errors.New("restore plan not found")
	}
	s.mu.Lock()
	plan, ok := s.plans[planID]
	s.mu.Unlock()
	if !ok {
		data, err := os.ReadFile(planPath(s.workDir, planID))
		if err != nil {
			return RestoreResult{}, errors.New("restore plan not found")
		}
		if err := json.Unmarshal(data, &plan); err != nil || plan.PlanID != planID {
			return RestoreResult{}, errors.New("restore plan not found")
		}
	}
	if plan.Status == "restart_required" {
		return plan.RestoreResult, nil
	}
	if time.Since(plan.CreatedAt) > RestorePlanTTL {
		_ = s.CancelRestore(ctx, planID)
		return RestoreResult{}, errors.New("restore plan expired")
	}
	if err := validateStaged(plan.StagePath); err != nil {
		return RestoreResult{}, err
	}
	if err := validateStagedManifest(plan.StagePath, plan.Manifest); err != nil {
		return RestoreResult{}, err
	}
	rollbackID := randomID()
	entries, err := stagedEntries(plan.StagePath)
	if err != nil {
		return RestoreResult{}, err
	}
	current, err := currentEntries(filepath.Dir(s.workDir))
	if err != nil {
		return RestoreResult{}, err
	}
	if err := writeRestoreJournal(s.workDir, RestoreJournal{StagedID: plan.StagedID, RollbackID: rollbackID, PreflightID: plan.PreRestoreSnapshotID, Entries: entries, CurrentEntries: current, Manifest: plan.Manifest, Step: "confirmed"}); err != nil {
		return RestoreResult{}, err
	}
	plan.Status = "restart_required"
	plan.RestartRequired = true
	s.mu.Lock()
	s.plans[planID] = plan
	s.mu.Unlock()
	_ = os.Remove(planPath(s.workDir, planID))
	return plan.RestoreResult, nil
}

func (s *Service) CancelRestore(ctx context.Context, planID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	plan, ok := s.plans[planID]
	if ok {
		delete(s.plans, planID)
	}
	s.mu.Unlock()
	if !ok {
		if !validRestoreID(planID) {
			return os.ErrNotExist
		}
		data, err := os.ReadFile(planPath(s.workDir, planID))
		if err != nil || json.Unmarshal(data, &plan) != nil || plan.PlanID != planID {
			return os.ErrNotExist
		}
	}
	return errors.Join(removeAllErr(plan.StagePath), removeAllErr(plan.PreflightPath), os.Remove(planPath(s.workDir, planID)))
}

func validSnapshotID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func validateStagedManifest(stage string, manifest Manifest) error {
	if err := validateManifestFiles(manifest.Files); err != nil {
		return err
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, expected := range manifest.Files {
		before, err := root.Lstat(expected.Path)
		if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidPackage
		}
		file, err := root.Open(expected.Path)
		if err != nil {
			return ErrInvalidPackage
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != expected.Size {
			file.Close()
			return ErrInvalidPackage
		}
		hash := sha256.New()
		n, copyErr := io.Copy(hash, io.LimitReader(file, MaxFileSize+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || n != expected.Size || fmt.Sprintf("%x", hash.Sum(nil)) != strings.ToLower(expected.SHA256) {
			return ErrInvalidPackage
		}
	}
	return nil
}

func closeStore(store ObjectStore) {
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func Encrypt(archive []byte, password string, manifest *Manifest) ([]byte, error) {
	if int64(len(archive)) > MaxArchiveSize {
		return nil, ErrInvalidPackage
	}
	var out bytes.Buffer
	if err := EncryptReader(bytes.NewReader(archive), int64(len(archive)), password, manifest, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func EncryptReader(archive io.Reader, original int64, password string, manifest *Manifest, out io.Writer) error {
	if original < 0 || original > MaxArchiveSize {
		return ErrInvalidPackage
	}
	salt, err := hex.DecodeString(manifest.KDF.Salt)
	if len(salt) != SaltSize {
		salt = make([]byte, SaltSize)
		if _, err := rand.Read(salt); err != nil {
			return err
		}
		manifest.KDF.Salt = hex.EncodeToString(salt)
	}
	key := argon2.IDKey([]byte(password), salt, KDFTime, KDFMemory, KDFThreads, KDFKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return err
	}
	header := make([]byte, headerSize)
	copy(header[:4], Magic)
	header[4] = byte(FormatVersion)
	copy(header[5:21], salt)
	binary.BigEndian.PutUint32(header[21:25], KDFTime)
	binary.BigEndian.PutUint32(header[25:29], KDFMemory)
	header[29] = byte(KDFThreads)
	binary.BigEndian.PutUint32(header[30:34], KDFKeyLen)
	binary.BigEndian.PutUint32(header[34:38], ChunkSize)
	binary.BigEndian.PutUint64(header[38:46], uint64(original))
	if _, err := out.Write(header); err != nil {
		return err
	}
	chunks := (original + ChunkSize - 1) / ChunkSize
	if chunks == 0 {
		chunks = 1
	}
	buf := make([]byte, ChunkSize)
	for index := int64(0); index < chunks; index++ {
		want := ChunkSize
		if remaining := original - index*ChunkSize; remaining < int64(want) {
			want = int(remaining)
		}
		read := 0
		for read < want {
			n, readErr := archive.Read(buf[read:want])
			read += n
			if readErr != nil {
				if readErr == io.EOF && read == want {
					break
				}
				return ErrInvalidPackage
			}
			if n == 0 {
				return ErrInvalidPackage
			}
		}
		nonce := make([]byte, NonceSize)
		binary.BigEndian.PutUint64(nonce[4:], uint64(index))
		sealed := aead.Seal(nil, nonce, buf[:want], header)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
		if _, err := out.Write(length[:]); err != nil {
			return err
		}
		if _, err := out.Write(sealed); err != nil {
			return err
		}
	}
	return nil
}

func Decrypt(data []byte, password string) ([]byte, error) {
	var out bytes.Buffer
	if _, err := DecryptReader(bytes.NewReader(data), password, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func DecryptReader(input io.Reader, password string, output io.Writer) (int64, error) {
	var header [headerSize]byte
	read, err := io.ReadFull(input, header[:])
	if err != nil || read != headerSize || string(header[:4]) != Magic || int(header[4]) != FormatVersion {
		return int64(read), ErrInvalidPackage
	}
	consumed := int64(headerSize)
	if consumed > MaxPackageSize {
		return consumed, ErrInvalidPackage
	}
	data := header[:]
	if len(data) < headerSize {
		return consumed, ErrInvalidPackage
	}
	salt := data[5 : 5+SaltSize]
	timeParam := binary.BigEndian.Uint32(data[21:25])
	memoryParam := binary.BigEndian.Uint32(data[25:29])
	threadsParam := data[29]
	keyLenParam := binary.BigEndian.Uint32(data[30:34])
	chunk := binary.BigEndian.Uint32(data[34:38])
	original := binary.BigEndian.Uint64(data[38:46])
	if original > MaxArchiveSize || chunk != ChunkSize {
		return consumed, ErrInvalidPackage
	}
	if timeParam != KDFTime || memoryParam != KDFMemory || threadsParam != KDFThreads || keyLenParam != KDFKeyLen {
		return consumed, ErrInvalidPackage
	}
	key := argon2.IDKey([]byte(password), salt, KDFTime, KDFMemory, KDFThreads, KDFKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return consumed, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceSize)
	if err != nil {
		return consumed, err
	}
	headerBytes := append([]byte(nil), header[:]...)
	var index uint64
	expectedChunks := (original + uint64(chunk) - 1) / uint64(chunk)
	if expectedChunks == 0 {
		expectedChunks = 1
	}
	for index < expectedChunks {
		var length [4]byte
		if _, err := io.ReadFull(input, length[:]); err != nil {
			return consumed, ErrInvalidPackage
		}
		consumed += 4
		size := int(binary.BigEndian.Uint32(length[:]))
		plainSize := int64(chunk)
		if remain := int64(original) - int64(index)*int64(chunk); remain < plainSize {
			plainSize = remain
		}
		if size != int(plainSize)+aead.Overhead() || consumed+int64(size) > MaxPackageSize {
			return consumed, ErrInvalidPackage
		}
		sealed := make([]byte, size)
		if _, err := io.ReadFull(input, sealed); err != nil {
			return consumed, ErrInvalidPackage
		}
		consumed += int64(size)
		nonce := make([]byte, NonceSize)
		binary.BigEndian.PutUint64(nonce[4:], index)
		plain, e := aead.Open(nil, nonce, sealed, headerBytes)
		if e != nil {
			return consumed, ErrBadPassword
		}
		if int64(len(plain)) != plainSize {
			return consumed, ErrInvalidPackage
		}
		if _, err := output.Write(plain); err != nil {
			return consumed, err
		}
		index++
	}
	var trailing [1]byte
	if n, err := input.Read(trailing[:]); n != 0 || err == nil {
		return consumed + int64(n), ErrInvalidPackage
	} else if err != io.EOF {
		return consumed, err
	}
	if consumed > MaxPackageSize {
		return consumed, ErrInvalidPackage
	}
	return consumed, nil
}

func archiveFiles(stage string, manifest *Manifest) ([]byte, error) {
	var out limitedBuffer
	if err := archiveFilesTo(stage, manifest, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func archiveFilesTo(stage string, manifest *Manifest, output io.Writer) error {
	limited := &countingLimitWriter{writer: output}
	zw := zip.NewWriter(limited)
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return err
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, file := range manifest.Files {
		clean, err := cleanRelative(file.Path)
		if err != nil {
			return err
		}
		info, err := root.Lstat(filepath.ToSlash(clean))
		if err != nil || !info.Mode().IsRegular() {
			return ErrInvalidPackage
		}
		inFile, err := root.Open(filepath.ToSlash(clean))
		if err != nil {
			return err
		}
		w, err := zw.Create(clean)
		if err != nil {
			inFile.Close()
			return err
		}
		written, copyErr := io.Copy(w, io.LimitReader(inFile, MaxFileSize+1))
		closeErr := inFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil || written > MaxFileSize || written != file.Size {
			return ErrInvalidPackage
		}
	}
	if err = zw.Close(); err != nil {
		return err
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len()+len(p)) > MaxArchiveSize {
		return 0, ErrInvalidPackage
	}
	return b.Buffer.Write(p)
}

type countingLimitWriter struct {
	writer io.Writer
	total  int64
}

func (w *countingLimitWriter) Write(p []byte) (int, error) {
	if w.total+int64(len(p)) > MaxArchiveSize {
		return 0, ErrInvalidPackage
	}
	n, err := w.writer.Write(p)
	w.total += int64(n)
	return n, err
}

func extractArchive(data []byte, destination string) error {
	return extractArchiveReader(bytes.NewReader(data), int64(len(data)), destination)
}

func extractArchiveReader(source io.ReaderAt, size int64, destination string) error {
	if size > MaxArchiveSize {
		return fmt.Errorf("archive exceeds limit: %w", ErrInvalidPackage)
	}
	if err := safeDestinationDir(destination); err != nil {
		return fmt.Errorf("restore destination: %w", err)
	}
	zr, err := zip.NewReader(source, size)
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
		root, rootErr := os.OpenRoot(destination)
		if rootErr != nil {
			return rootErr
		}
		r, e := f.Open()
		if e != nil {
			root.Close()
			return e
		}
		if existing, statErr := root.Lstat(filepath.ToSlash(clean)); statErr == nil && existing.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return fmt.Errorf("target symlink: %w", ErrInvalidPackage)
		}
		out, e := root.OpenFile(filepath.ToSlash(clean), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			written, copyErr := io.Copy(out, io.LimitReader(r, MaxFileSize+1))
			if copyErr != nil {
				e = copyErr
			} else if written > MaxFileSize || written != int64(f.UncompressedSize64) {
				e = ErrInvalidPackage
			}
			_ = out.Close()
		}
		r.Close()
		root.Close()
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
	return validateArchiveReader(bytes.NewReader(data), int64(len(data)), snapshotID)
}

func validateArchiveReader(source io.ReaderAt, size int64, snapshotID string) (Manifest, error) {
	if size > MaxArchiveSize {
		return Manifest{}, ErrInvalidPackage
	}
	zr, err := zip.NewReader(source, size)
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
	if err := validateManifestFiles(manifest.Files); err != nil {
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
