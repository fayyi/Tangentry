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
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	FormatVersion = 1
	Magic         = "TGB1"
	ChunkSize     = 1024 * 1024
	SaltSize      = 16
	NonceSize     = 12
	KDFTime       = 3
	KDFMemory     = 64 * 1024
	KDFThreads    = 2
	KDFKeyLen     = 32
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
}

func NewService(repo Repository, source Source, stores StoreFactory, workDir, version string) *Service {
	return &Service{repo: repo, source: source, stores: stores, workDir: workDir, version: version}
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
	manifest := Manifest{FormatVersion: FormatVersion, TangentryVersion: s.version, SnapshotID: id, CreatedAt: time.Now().UTC(), TaskID: task.ID, IncludeDownloads: task.IncludeDownloads, Files: files, KDF: KDFParams{Name: "Argon2id", Version: 1, Salt: hex.EncodeToString(salt), Time: KDFTime, Memory: KDFMemory, Threads: KDFThreads, KeyLen: KDFKeyLen}, AEAD: AEADParams{Name: "AES-256-GCM", Version: 1, ChunkSize: ChunkSize, NonceSize: NonceSize}}
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

func (s *Service) Validate(ctx context.Context, targetID, snapshotID, password string) error {
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return err
	}
	store, err := s.stores(target)
	if err != nil {
		return err
	}
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_, err = Decrypt(b, password)
	return err
}

func (s *Service) Restore(ctx context.Context, targetID, snapshotID, password, destination string) error {
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return err
	}
	store, err := s.stores(target)
	if err != nil {
		return err
	}
	r, err := store.Read(ctx, "tangentry/v1/snapshots/"+snapshotID+".tgb")
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	plain, err := Decrypt(b, password)
	if err != nil {
		return err
	}
	// Preserve the current local state before writing restored files. The source
	// adapter chooses the consistent snapshot mechanism (SQLite VACUUM INTO).
	if s.source != nil {
		preflight := filepath.Join(s.workDir, "restore", randomID())
		if err := ensureDir(preflight); err != nil {
			return err
		}
		defer removeAll(preflight)
		if _, err := s.source.Snapshot(ctx, preflight, false); err != nil {
			return fmt.Errorf("create pre-restore snapshot: %w", err)
		}
	}
	return extractArchive(plain, destination)
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
	binary.Write(&out, binary.BigEndian, uint32(ChunkSize))
	binary.Write(&out, binary.BigEndian, uint64(len(archive)))
	for offset, index := 0, uint64(0); offset < len(archive); offset, index = offset+ChunkSize, index+1 {
		end := min(offset+ChunkSize, len(archive))
		nonce := make([]byte, NonceSize)
		binary.BigEndian.PutUint64(nonce[4:], index)
		sealed := aead.Seal(nil, nonce, archive[offset:end], nil)
		binary.Write(&out, binary.BigEndian, uint32(len(sealed)))
		out.Write(sealed)
	}
	return out.Bytes(), nil
}

func Decrypt(data []byte, password string) ([]byte, error) {
	if len(data) < 4+1+SaltSize+4+8 || string(data[:4]) != Magic || int(data[4]) != FormatVersion {
		return nil, ErrInvalidPackage
	}
	salt := data[5 : 5+SaltSize]
	chunk := binary.BigEndian.Uint32(data[21:25])
	original := binary.BigEndian.Uint64(data[25:33])
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
	pos := 33
	var index uint64
	for out.Len() < int(original) {
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
		plain, e := aead.Open(nil, nonce, data[pos:pos+size], nil)
		if e != nil {
			return nil, ErrBadPassword
		}
		out.Write(plain)
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
	if err := ensureDir(destination); err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		clean, e := cleanRelative(f.Name)
		if e != nil {
			return e
		}
		if clean == "manifest.json" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		if !within(destination, target) {
			return ErrInvalidPackage
		}
		if err = ensureDir(filepath.Dir(target)); err != nil {
			return err
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		out, e := create(target)
		if e == nil {
			_, e = io.Copy(out, io.LimitReader(r, 256<<20))
			out.Close()
		}
		r.Close()
		if e != nil {
			return e
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
func osMkdirAll(name string, perm uint32) error    { return os.MkdirAll(name, os.FileMode(perm)) }
func osRemoveAll(name string) error                { return os.RemoveAll(name) }
func osOpen(name string) (io.ReadCloser, error)    { return os.Open(name) }
func osCreate(name string) (io.WriteCloser, error) { return os.Create(name) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
