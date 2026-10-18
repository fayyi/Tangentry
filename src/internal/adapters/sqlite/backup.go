package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tangentry/internal/backup"
)

// Snapshot writes a consistent SQLite backup using VACUUM INTO. It never
// copies the live WAL-mode database file directly.
func (s *Store) Snapshot(ctx context.Context, staging string, includeDownloads bool) ([]backup.File, error) {
	if err := os.MkdirAll(staging, 0700); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(staging, "tangentry.db")
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dbPath); err != nil {
		return nil, fmt.Errorf("sqlite consistency snapshot: %w", err)
	}
	files, err := describeFiles(staging, dbPath)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(s.path)
	// Business results are in SQLite (including subtitles, transcripts, AI
	// results and note-frame metadata/blobs). Only imported PDF sidecars live in
	// the filesystem; credential/config, models, tools and caches stay excluded.
	allowed := []string{"documents"}
	for _, name := range allowed {
		extra, e := collectTree(root, name, staging, backup.FileData)
		if e != nil {
			return nil, e
		}
		files = append(files, extra...)
	}
	if includeDownloads {
		downloads := s.downloadDir
		if downloads == "" {
			downloads = filepath.Join(root, "downloads")
		}
		extra, e := collectTree(filepath.Dir(downloads), filepath.Base(downloads), staging, backup.FileData)
		if e != nil {
			return nil, e
		}
		files = append(files, extra...)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func collectTree(root, name, staging string, kind backup.FileType) ([]backup.File, error) {
	if excludedBackupComponent(name) {
		return nil, nil
	}
	sourceRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer sourceRoot.Close()
	info, err := sourceRoot.Lstat(filepath.ToSlash(name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil
	}
	stageRoot, err := os.OpenRoot(staging)
	if err != nil {
		return nil, err
	}
	defer stageRoot.Close()
	var out []backup.File
	err = fs.WalkDir(sourceRoot.FS(), filepath.ToSlash(name), func(p string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		base := filepath.Base(p)
		if excludedBackupComponent(base) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		before, err := sourceRoot.Lstat(filepath.ToSlash(p))
		if err != nil || !before.Mode().IsRegular() {
			return nil
		}
		in, err := sourceRoot.Open(filepath.ToSlash(p))
		if err != nil {
			return err
		}
		opened, err := in.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
			in.Close()
			return backup.ErrInvalidPackage
		}
		if err := stageRoot.MkdirAll(filepath.ToSlash(filepath.Dir(p)), 0700); err != nil {
			in.Close()
			return err
		}
		outFile, err := stageRoot.OpenFile(filepath.ToSlash(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return err
		}
		hash := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(outFile, hash), io.LimitReader(in, backup.MaxFileSize+1))
		inErr := in.Close()
		outErr := outFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if outErr != nil {
			return outErr
		}
		if size > backup.MaxFileSize {
			return backup.ErrInvalidPackage
		}
		out = append(out, backup.File{Path: filepath.ToSlash(p), Type: kind, Size: size, SHA256: fmt.Sprintf("%x", hash.Sum(nil))})
		return nil
	})
	return out, err
}

func excludedBackupComponent(name string) bool {
	switch strings.ToLower(filepath.Base(filepath.Clean(name))) {
	case ".tangentry-restore", "credentials", "credential", "cookies", "cookie", "models", "model", "tools", "tool", "cache", "caches", "runtime", "keychain", "keychains":
		return true
	default:
		return false
	}
}

func excludedBackupPath(rel string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(rel), func(r rune) bool { return r == '/' }) {
		if excludedBackupComponent(part) {
			return true
		}
	}
	return false
}

func describeFiles(root, dbPath string) ([]backup.File, error) {
	file, err := os.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > backup.MaxFileSize {
		return nil, backup.ErrInvalidPackage
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, backup.MaxFileSize+1)); err != nil {
		return nil, err
	}
	return []backup.File{{Path: "tangentry.db", Type: backup.FileDatabase, Size: info.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))}}, nil
}

func (s *Store) BackupTargets(ctx context.Context) ([]backup.Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,type,path,enabled FROM backup_target ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Target
	for rows.Next() {
		var t backup.Target
		var enabled int
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Path, &enabled); err != nil {
			return nil, err
		}
		t.Enabled = enabled != 0
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) BackupTarget(ctx context.Context, id string) (backup.Target, error) {
	var t backup.Target
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,type,path,enabled FROM backup_target WHERE id=?`, id).Scan(&t.ID, &t.Name, &t.Type, &t.Path, &enabled)
	t.Enabled = enabled != 0
	return t, err
}
func (s *Store) SaveBackupTarget(ctx context.Context, t backup.Target) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO backup_target(id,name,type,path,enabled) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,path=excluded.path,enabled=excluded.enabled`, t.ID, t.Name, t.Type, t.Path, boolInt(t.Enabled))
	return err
}
func (s *Store) DeleteBackupTarget(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_target WHERE id=?`, id)
	return err
}
func (s *Store) BackupTasks(ctx context.Context) ([]backup.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,include_downloads,retention,enabled FROM backup_task ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Task
	for rows.Next() {
		var t backup.Task
		var inc, en int
		if err := rows.Scan(&t.ID, &t.Name, &inc, &t.Retention, &en); err != nil {
			return nil, err
		}
		t.IncludeDownloads = inc != 0
		t.Enabled = en != 0
		ids, err := s.backupTaskTargets(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.TargetIDs = ids
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) BackupTask(ctx context.Context, id string) (backup.Task, error) {
	var t backup.Task
	var inc, en int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,include_downloads,retention,enabled FROM backup_task WHERE id=?`, id).Scan(&t.ID, &t.Name, &inc, &t.Retention, &en)
	if err != nil {
		return t, err
	}
	t.IncludeDownloads = inc != 0
	t.Enabled = en != 0
	t.TargetIDs, _ = s.backupTaskTargets(ctx, id)
	return t, nil
}
func (s *Store) backupTaskTargets(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT target_id FROM backup_task_target WHERE task_id=? ORDER BY position,target_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		ids = append(ids, v)
	}
	return ids, rows.Err()
}
func (s *Store) SaveBackupTask(ctx context.Context, t backup.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO backup_task(id,name,include_downloads,retention,enabled) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,include_downloads=excluded.include_downloads,retention=excluded.retention,enabled=excluded.enabled`, t.ID, t.Name, boolInt(t.IncludeDownloads), t.Retention, boolInt(t.Enabled)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM backup_task_target WHERE task_id=?`, t.ID); err != nil {
		return err
	}
	for i, id := range t.TargetIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO backup_task_target(task_id,target_id,position) VALUES(?,?,?)`, t.ID, id, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteBackupTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_task WHERE id=?`, id)
	return err
}
func (s *Store) SaveBackupSnapshot(ctx context.Context, v backup.Snapshot) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO backup_run(id,task_id,created_at,size,status) VALUES(?,?,?,?,?)`, v.ID, v.TaskID, v.CreatedAt.Unix(), v.Size, v.Status)
	return err
}
func (s *Store) BackupSnapshots(ctx context.Context) ([]backup.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,created_at,size,status FROM backup_run ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Snapshot
	for rows.Next() {
		var v backup.Snapshot
		var ts int64
		if err := rows.Scan(&v.ID, &v.TaskID, &ts, &v.Size, &v.Status); err != nil {
			return nil, err
		}
		v.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListTargets(ctx context.Context) ([]backup.Target, error) {
	return s.BackupTargets(ctx)
}
func (s *Store) GetTarget(ctx context.Context, id string) (backup.Target, error) {
	return s.BackupTarget(ctx, id)
}
func (s *Store) SaveTarget(ctx context.Context, t backup.Target) error {
	return s.SaveBackupTarget(ctx, t)
}
func (s *Store) DeleteTarget(ctx context.Context, id string) error {
	return s.DeleteBackupTarget(ctx, id)
}
func (s *Store) ListTasks(ctx context.Context) ([]backup.Task, error) { return s.BackupTasks(ctx) }
func (s *Store) GetTask(ctx context.Context, id string) (backup.Task, error) {
	return s.BackupTask(ctx, id)
}
func (s *Store) SaveTask(ctx context.Context, t backup.Task) error { return s.SaveBackupTask(ctx, t) }
func (s *Store) DeleteTask(ctx context.Context, id string) error   { return s.DeleteBackupTask(ctx, id) }
func (s *Store) SaveSnapshot(ctx context.Context, v backup.Snapshot) error {
	return s.SaveBackupSnapshot(ctx, v)
}
func (s *Store) ListSnapshots(ctx context.Context) ([]backup.Snapshot, error) {
	return s.BackupSnapshots(ctx)
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func sha256Sum(v []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write(v)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
