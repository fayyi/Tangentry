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

func validateManifestFiles(files []File, includeDownloads bool) error {
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
		} else {
			if file.Type != FileData || !allowedBackupPath(file.Path, includeDownloads) {
				return ErrInvalidPackage
			}
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

func allowedBackupPath(name string, includeDownloads bool) bool {
	clean, err := cleanRelative(name)
	if err != nil || clean != name {
		return false
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 || parts[0] != "documents" && !(includeDownloads && parts[0] == "downloads") {
		return false
	}
	for _, part := range parts[1:] {
		switch strings.ToLower(part) {
		case "credentials", "credential", "cookies", "cookie", "keychain", "keychains", "models", "model", "tools", "tool", "cache", "caches", "runtime", ".tangentry-restore":
			return false
		}
	}
	return true
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
	parent := filepath.Dir(dir)
	name := filepath.Base(dir)
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer parentRoot.Close()
	info, err := parentRoot.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if err := parentRoot.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = parentRoot.Lstat(name)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPackage
	}
	opened, err := parentRoot.OpenRoot(name)
	if err != nil {
		return err
	}
	openedInfo, statErr := opened.Stat(".")
	closeErr := opened.Close()
	if statErr != nil || closeErr != nil || !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		return ErrInvalidPackage
	}
	if err := parentRoot.Chmod(name, 0700); err != nil {
		return err
	}
	return nil
}

func chmodTree(root string, fileMode, dirMode os.FileMode) error {
	opened, directories, files, err := inspectTree(root)
	if err != nil {
		return err
	}
	defer opened.Close()
	for _, name := range files {
		if err := opened.Chmod(name, fileMode); err != nil {
			return err
		}
	}
	sort.Slice(directories, func(i, j int) bool { return strings.Count(directories[i], "/") > strings.Count(directories[j], "/") })
	for _, name := range directories {
		if name == "." {
			continue
		}
		if err := opened.Chmod(name, dirMode); err != nil {
			return err
		}
	}
	return opened.Chmod(".", dirMode)
}

func syncTree(root string) error {
	opened, directories, files, err := inspectTree(root)
	if err != nil {
		return err
	}
	defer opened.Close()
	for _, name := range files {
		file, err := opened.Open(name)
		if err != nil {
			return err
		}
		info, statErr := file.Stat()
		syncErr := file.Sync()
		closeErr := file.Close()
		if statErr != nil || !info.Mode().IsRegular() {
			return ErrInvalidPackage
		}
		if err := errors.Join(syncErr, closeErr); err != nil {
			return err
		}
	}
	sort.Slice(directories, func(i, j int) bool { return strings.Count(directories[i], "/") > strings.Count(directories[j], "/") })
	for _, name := range directories {
		if err := syncRootPath(opened, name); err != nil {
			return err
		}
	}
	return nil
}

func inspectTree(path string) (*os.Root, []string, []string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, nil, ErrInvalidPackage
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !after.IsDir() || !os.SameFile(before, after) {
		root.Close()
		return nil, nil, nil, ErrInvalidPackage
	}
	var directories []string
	var files []string
	err = filepath.Walk(path, func(name string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		checked, err := root.Lstat(relative)
		if err != nil || checked.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, checked) {
			return ErrInvalidPackage
		}
		if info.IsDir() {
			directories = append(directories, relative)
			return nil
		}
		if !info.Mode().IsRegular() {
			return ErrInvalidPackage
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		root.Close()
		return nil, nil, nil, err
	}
	return root, directories, files, nil
}

func syncRootPath(root *os.Root, name string) error {
	var directory *os.File
	var err error
	if name == "." {
		directory, err = root.Open(".")
	} else {
		var nested *os.Root
		nested, err = root.OpenRoot(name)
		if err == nil {
			directory, err = nested.Open(".")
			closeErr := nested.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
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

func validateRestorePlan(workDir, id string, plan restorePlan) error {
	if !validRestoreID(id) || plan.PlanID != id || plan.StagedID != id || plan.PreRestoreSnapshotID != plan.RestoreResult.PreRestoreSnapshotID || !validRestoreID(plan.PreRestoreSnapshotID) ||
		plan.StagePath != filepath.Join(workDir, "staged-"+plan.StagedID) || plan.PreflightPath != filepath.Join(workDir, "pre-"+plan.PreRestoreSnapshotID) {
		return ErrInvalidPackage
	}
	if plan.SnapshotID != plan.Manifest.SnapshotID || !validSnapshotID(plan.SnapshotID) {
		return ErrInvalidPackage
	}
	return validateManifestFiles(plan.Manifest.Files, plan.Manifest.IncludeDownloads)
}

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
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "plan-"), ".json")
		if readErr != nil || json.Unmarshal(data, &plan) != nil || validateRestorePlan(s.workDir, id, plan) != nil || now.Sub(plan.CreatedAt) > RestorePlanTTL {
			removeAll(path)
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
	return s.enforceRestoreWorkDirLimit()
}

func (s *Service) enforceRestoreWorkDirLimit() error {
	for {
		size, err := restoreWorkDirSize(s.workDir)
		if err != nil && !errors.Is(err, ErrInvalidPackage) {
			return err
		}
		if size <= MaxRestoreWorkDirSize {
			return nil
		}
		entries, err := os.ReadDir(s.workDir)
		if err != nil {
			return err
		}
		var oldest string
		var oldestTime time.Time
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "plan-") || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var plan restorePlan
			data, readErr := os.ReadFile(filepath.Join(s.workDir, entry.Name()))
			if readErr != nil || json.Unmarshal(data, &plan) != nil {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "plan-"), ".json")
			if validateRestorePlan(s.workDir, id, plan) != nil {
				continue
			}
			if oldest == "" || plan.CreatedAt.Before(oldestTime) {
				oldest = entry.Name()
				oldestTime = plan.CreatedAt
			}
		}
		if oldest == "" {
			return errors.New("restore workspace exceeds disk limit")
		}
		var plan restorePlan
		data, _ := os.ReadFile(filepath.Join(s.workDir, oldest))
		_ = json.Unmarshal(data, &plan)
		_ = removeAllErr(filepath.Join(s.workDir, oldest))
		id := strings.TrimSuffix(strings.TrimPrefix(oldest, "plan-"), ".json")
		if validateRestorePlan(s.workDir, id, plan) == nil {
			_ = removeAllErr(plan.StagePath)
			_ = removeAllErr(plan.PreflightPath)
		}
		delete(s.plans, plan.PlanID)
	}
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

func (s *Service) withRestoreLock(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
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
	if err := validateManifestFiles(manifest.Files, task.IncludeDownloads); err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	if err := cleanupRestoreWorkDir(s.workDir); err != nil {
		return RestoreResult{}, err
	}
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
	if size, err := restoreWorkDirSize(s.workDir); err != nil || size > MaxRestoreWorkDirSize-restoreReservation {
		if err != nil {
			return RestoreResult{}, err
		}
		return RestoreResult{}, errors.New("restore workspace disk limit exceeded")
	}
	if _, err := os.Stat(filepath.Join(s.workDir, "journal.json")); err == nil {
		return RestoreResult{}, errors.New("a restore is already confirmed; cancel it before preparing another")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
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
	preflightFiles, err := s.source.Snapshot(ctx, preflight, false)
	if err != nil {
		removeAll(preflight)
		return RestoreResult{}, fmt.Errorf("create pre-restore snapshot: %w", err)
	}
	if err := validateManifestFiles(preflightFiles, false); err != nil {
		removeAll(preflight)
		return RestoreResult{}, fmt.Errorf("create pre-restore snapshot: %w", err)
	}
	if err := chmodTree(preflight, 0600, 0700); err != nil {
		removeAll(preflight)
		return RestoreResult{}, err
	}
	if err := syncTree(preflight); err != nil {
		removeAll(preflight)
		return RestoreResult{}, err
	}
	stagePath := filepath.Join(s.workDir, "staged-"+id)
	if _, err := plainFile.Seek(0, io.SeekStart); err != nil {
		removeAll(preflight)
		return RestoreResult{}, err
	}
	if err := extractArchiveReader(plainFile, plainInfo.Size(), stagePath, validatedManifest.IncludeDownloads); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, fmt.Errorf("stage restored data: %w", err)
	}
	if err := chmodTree(stagePath, 0600, 0700); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, err
	}
	if err := syncTree(stagePath); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, err
	}
	if size, err := restoreWorkDirSize(s.workDir); err != nil || size > MaxRestoreWorkDirSize-restoreReservation {
		removeAll(preflight)
		removeAll(stagePath)
		if err != nil {
			return RestoreResult{}, err
		}
		return RestoreResult{}, errors.New("restore workspace disk limit exceeded")
	}
	result := RestoreResult{PlanID: id, SnapshotID: snapshotID, StagedID: id, PreRestoreSnapshotID: preflightID, Status: "pending_confirmation"}
	plan := restorePlan{RestoreResult: result, TargetID: targetID, StagePath: stagePath, PreflightPath: preflight, CreatedAt: time.Now().UTC(), Manifest: validatedManifest}
	if err := s.savePlan(plan); err != nil {
		removeAll(preflight)
		removeAll(stagePath)
		return RestoreResult{}, err
	}
	s.plans[result.PlanID] = plan
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
	defer s.mu.Unlock()
	if _, err := os.Stat(filepath.Join(s.workDir, "journal.json")); err == nil {
		return RestoreResult{}, errors.New("a restore is already confirmed; cancel it before preparing another")
	} else if !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
	}
	plan, ok := s.plans[planID]
	if !ok {
		data, err := os.ReadFile(planPath(s.workDir, planID))
		if err != nil {
			return RestoreResult{}, errors.New("restore plan not found")
		}
		if err := json.Unmarshal(data, &plan); err != nil || validateRestorePlan(s.workDir, planID, plan) != nil {
			return RestoreResult{}, errors.New("restore plan not found")
		}
	}
	if err := validateRestorePlan(s.workDir, planID, plan); err != nil {
		return RestoreResult{}, errors.New("restore plan not found")
	}
	if plan.Status != "pending_confirmation" {
		return RestoreResult{}, errors.New("restore plan not found")
	}
	if time.Since(plan.CreatedAt) > RestorePlanTTL {
		_ = removeAllErr(plan.StagePath)
		_ = removeAllErr(plan.PreflightPath)
		_ = os.Remove(planPath(s.workDir, planID))
		delete(s.plans, planID)
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
	current, err := currentEntries(filepath.Dir(s.workDir), plan.Manifest.IncludeDownloads)
	if err != nil {
		return RestoreResult{}, err
	}
	if err := writeRestoreJournal(s.workDir, RestoreJournal{PlanID: planID, StagedID: plan.StagedID, RollbackID: rollbackID, PreflightID: plan.PreRestoreSnapshotID, Entries: journalEntries(entries, entryPending), CurrentEntries: current, Manifest: plan.Manifest, Step: "confirmed"}); err != nil {
		return RestoreResult{}, err
	}
	plan.Status = "restart_required"
	plan.RestartRequired = true
	s.plans[planID] = plan
	if err := os.Remove(planPath(s.workDir, planID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return RestoreResult{}, err
	}
	if err := syncDirectory(s.workDir); err != nil {
		return RestoreResult{}, err
	}
	return plan.RestoreResult, nil
}

func (s *Service) CancelRestore(ctx context.Context, planID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[planID]
	journal, journalErr := readRestoreJournal(s.workDir)
	if journalErr == nil && journal.PlanID == planID {
		if journal.Step != "confirmed" || journal.PreflightID == "" || journal.PlanID == "" || validateRestoreJournal(journal) != nil {
			return errors.New("confirmed restore can no longer be cancelled")
		}
		if err := removeRestoreJournal(s.workDir); err != nil {
			return err
		}
		delete(s.plans, planID)
		stagePath := filepath.Join(s.workDir, "staged-"+journal.StagedID)
		preflightPath := filepath.Join(s.workDir, "pre-"+journal.PreflightID)
		return errors.Join(removeAllErr(stagePath), removeAllErr(preflightPath), syncDirectory(s.workDir))
	} else if journalErr != nil && !errors.Is(journalErr, os.ErrNotExist) {
		return journalErr
	}
	if ok {
		delete(s.plans, planID)
	}
	if !ok {
		if !validRestoreID(planID) {
			return os.ErrNotExist
		}
		data, err := os.ReadFile(planPath(s.workDir, planID))
		if err != nil || json.Unmarshal(data, &plan) != nil || validateRestorePlan(s.workDir, planID, plan) != nil {
			return os.ErrNotExist
		}
	}
	if err := os.Remove(planPath(s.workDir, planID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDirectory(s.workDir); err != nil {
		return err
	}
	return errors.Join(removeAllErr(plan.StagePath), removeAllErr(plan.PreflightPath), syncDirectory(s.workDir))
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
	if err := validateManifestFiles(manifest.Files, manifest.IncludeDownloads); err != nil {
		return err
	}
	root, err := openSafeRestoreRoot(stage)
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
	root, err := openSafeRestoreRoot(stage)
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
	return extractArchiveReader(bytes.NewReader(data), int64(len(data)), destination, false)
}

func extractArchiveReader(source io.ReaderAt, size int64, destination string, includeDownloads bool) error {
	if size > MaxArchiveSize {
		return fmt.Errorf("archive exceeds limit: %w", ErrInvalidPackage)
	}
	destinationRoot, err := openSafeRestoreRoot(destination)
	if err != nil {
		return fmt.Errorf("restore destination: %w", err)
	}
	defer destinationRoot.Close()
	zr, err := zip.NewReader(source, size)
	if err != nil {
		return err
	}
	if len(zr.File) > MaxEntries {
		return fmt.Errorf("too many entries: %w", ErrInvalidPackage)
	}
	var total uint64
	for _, f := range zr.File {
		mode := f.FileInfo().Mode()
		if !mode.IsRegular() {
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
		if !allowedBackupPath(clean, includeDownloads) && clean != "tangentry.db" {
			return fmt.Errorf("restore path outside backup scope %q: %w", clean, ErrInvalidPackage)
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		root, base, rootErr := openRestoreParent(destinationRoot, clean)
		if rootErr != nil {
			return fmt.Errorf("restore parent %q: %w", filepath.Dir(target), rootErr)
		}
		r, e := f.Open()
		if e != nil {
			root.Close()
			return e
		}
		if _, statErr := root.Lstat(base); statErr == nil {
			root.Close()
			return fmt.Errorf("restore target already exists: %w", os.ErrExist)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			root.Close()
			return statErr
		}
		out, e := root.OpenFile(base, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			written, copyErr := io.Copy(out, io.LimitReader(r, MaxFileSize+1))
			if copyErr != nil {
				e = copyErr
			} else if written > MaxFileSize || written != int64(f.UncompressedSize64) {
				e = ErrInvalidPackage
			}
			if syncErr := out.Sync(); e == nil && syncErr != nil {
				e = syncErr
			}
			if closeErr := out.Close(); e == nil && closeErr != nil {
				e = closeErr
			}
			if e == nil {
				e = syncRootDirectory(root)
			}
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
	return syncRootDirectory(destinationRoot)
}

func openSafeRestoreRoot(destination string) (*os.Root, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	base := filepath.Base(abs)
	info, err := parent.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		if err := parent.Mkdir(base, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			parent.Close()
			return nil, err
		}
		info, err = parent.Lstat(base)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		parent.Close()
		return nil, ErrInvalidPackage
	}
	root, err := parent.OpenRoot(base)
	if err != nil {
		parent.Close()
		return nil, err
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		root.Close()
		parent.Close()
		return nil, ErrInvalidPackage
	}
	if err := parent.Close(); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func openRestoreParent(destination *os.Root, relative string) (*os.Root, string, error) {
	root, err := destination.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	directory := path.Dir(relative)
	if directory == "." {
		return root, path.Base(relative), nil
	}
	for _, component := range strings.Split(directory, "/") {
		info, statErr := root.Lstat(component)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := root.Mkdir(component, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				root.Close()
				return nil, "", err
			}
			if err := syncRootDirectory(root); err != nil {
				root.Close()
				return nil, "", err
			}
			info, statErr = root.Lstat(component)
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", ErrInvalidPackage
		}
		next, openErr := root.OpenRoot(component)
		if openErr != nil {
			root.Close()
			return nil, "", openErr
		}
		nextInfo, statErr := next.Stat(".")
		if statErr != nil || !nextInfo.IsDir() || !os.SameFile(info, nextInfo) {
			next.Close()
			root.Close()
			return nil, "", ErrInvalidPackage
		}
		root.Close()
		root = next
	}
	return root, path.Base(relative), nil
}

func syncRootDirectory(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
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
		if !f.FileInfo().Mode().IsRegular() {
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
	if err := validateManifestFiles(manifest.Files, manifest.IncludeDownloads); err != nil {
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
			if !entry.FileInfo().Mode().IsRegular() || entry.UncompressedSize64 != uint64(f.Size) {
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
