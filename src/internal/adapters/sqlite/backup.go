package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
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
		extra, e := collectTreeAs(root, name, name, staging, backup.FileData)
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
		extra, e := collectTreeAs(filepath.Dir(downloads), filepath.Base(downloads), "downloads", staging, backup.FileData)
		if e != nil {
			return nil, e
		}
		files = append(files, extra...)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func collectTree(root, name, staging string, kind backup.FileType) ([]backup.File, error) {
	return collectTreeAs(root, name, name, staging, kind)
}

func collectTreeAs(root, sourceName, destinationName, staging string, kind backup.FileType) ([]backup.File, error) {
	if excludedBackupComponent(sourceName) {
		return nil, nil
	}
	sourceRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer sourceRoot.Close()
	info, err := sourceRoot.Lstat(filepath.ToSlash(sourceName))
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
	err = fs.WalkDir(sourceRoot.FS(), filepath.ToSlash(sourceName), func(p string, entry fs.DirEntry, e error) error {
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
		rel, err := filepath.Rel(filepath.FromSlash(sourceName), filepath.FromSlash(p))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return backup.ErrInvalidPackage
		}
		destinationPath := filepath.ToSlash(filepath.Join(destinationName, rel))
		in, err := sourceRoot.Open(filepath.ToSlash(p))
		if err != nil {
			return err
		}
		opened, err := in.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
			in.Close()
			return backup.ErrInvalidPackage
		}
		if err := stageRoot.MkdirAll(filepath.ToSlash(filepath.Dir(destinationPath)), 0700); err != nil {
			in.Close()
			return err
		}
		outFile, err := stageRoot.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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
		out = append(out, backup.File{Path: destinationPath, Type: kind, Size: size, SHA256: fmt.Sprintf("%x", hash.Sum(nil))})
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,type,path,enabled,endpoint,username,remote_path,allow_insecure_tls FROM backup_target ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Target
	for rows.Next() {
		var t backup.Target
		var enabled int
		var allowInsecure int
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Path, &enabled, &t.Endpoint, &t.Username, &t.RemotePath, &allowInsecure); err != nil {
			return nil, err
		}
		t.Enabled = enabled != 0
		t.AllowInsecureTLS = allowInsecure != 0
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) BackupTarget(ctx context.Context, id string) (backup.Target, error) {
	var t backup.Target
	var enabled int
	var allowInsecure int
	waitStarted := time.Now()
	conn, err := s.db.Conn(ctx)
	slog.Info("backup target sqlite timing", "phase", "connection_wait", "duration_ms", time.Since(waitStarted).Milliseconds(), "target_type", "unknown", "error_class", sqliteErrorClass(err))
	if err != nil {
		return t, err
	}
	defer conn.Close()
	queryStarted := time.Now()
	err = conn.QueryRowContext(ctx, `SELECT id,name,type,path,enabled,endpoint,username,remote_path,allow_insecure_tls FROM backup_target WHERE id=?`, id).Scan(&t.ID, &t.Name, &t.Type, &t.Path, &enabled, &t.Endpoint, &t.Username, &t.RemotePath, &allowInsecure)
	slog.Info("backup target sqlite timing", "phase", "target_lookup", "duration_ms", time.Since(queryStarted).Milliseconds(), "target_type", sqliteTargetType(t.Type), "error_class", sqliteErrorClass(err))
	t.Enabled = enabled != 0
	t.AllowInsecureTLS = allowInsecure != 0
	return t, err
}
func (s *Store) SaveBackupTarget(ctx context.Context, t backup.Target) error {
	waitStarted := time.Now()
	conn, err := s.db.Conn(ctx)
	targetType := sqliteTargetType(t.Type)
	slog.Info("backup target sqlite timing", "phase", "connection_wait", "duration_ms", time.Since(waitStarted).Milliseconds(), "target_type", targetType, "error_class", sqliteErrorClass(err))
	if err != nil {
		return err
	}
	defer conn.Close()
	writeStarted := time.Now()
	_, err = conn.ExecContext(ctx, `INSERT INTO backup_target(id,name,type,path,enabled,endpoint,username,remote_path,allow_insecure_tls) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,path=excluded.path,enabled=excluded.enabled,endpoint=excluded.endpoint,username=excluded.username,remote_path=excluded.remote_path,allow_insecure_tls=excluded.allow_insecure_tls`, t.ID, t.Name, t.Type, t.Path, boolInt(t.Enabled), t.Endpoint, t.Username, t.RemotePath, boolInt(t.AllowInsecureTLS))
	slog.Info("backup target sqlite timing", "phase", "target_write", "duration_ms", time.Since(writeStarted).Milliseconds(), "target_type", targetType, "error_class", sqliteErrorClass(err))
	return err
}

func sqliteTargetType(value string) string {
	if value == "local" || value == "webdav" {
		return value
	}
	return "unknown"
}

func sqliteErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	return "database"
}
func (s *Store) DeleteBackupTarget(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_target WHERE id=?`, id)
	return err
}
func (s *Store) BackupTasks(ctx context.Context) ([]backup.Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,include_downloads,retention,enabled,interval_hours FROM backup_task ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Task
	for rows.Next() {
		var t backup.Task
		var inc, en int
		if err := rows.Scan(&t.ID, &t.Name, &inc, &t.Retention, &en, &t.IntervalHours); err != nil {
			return nil, err
		}
		t.IncludeDownloads = inc != 0
		t.Enabled = en != 0
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].TargetIDs, out[i].TargetIntervalHours, err = s.backupTaskTargets(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Store) BackupTask(ctx context.Context, id string) (backup.Task, error) {
	var t backup.Task
	var inc, en int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,include_downloads,retention,enabled,interval_hours FROM backup_task WHERE id=?`, id).Scan(&t.ID, &t.Name, &inc, &t.Retention, &en, &t.IntervalHours)
	if err != nil {
		return t, err
	}
	t.IncludeDownloads = inc != 0
	t.Enabled = en != 0
	t.TargetIDs, t.TargetIntervalHours, _ = s.backupTaskTargets(ctx, id)
	return t, nil
}
func (s *Store) backupTaskTargets(ctx context.Context, id string) ([]string, map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT target_id,interval_hours FROM backup_task_target WHERE task_id=? ORDER BY position,target_id`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []string
	intervals := make(map[string]int)
	for rows.Next() {
		var v string
		var interval int
		if err := rows.Scan(&v, &interval); err != nil {
			return nil, nil, err
		}
		ids = append(ids, v)
		if interval > 0 {
			intervals[v] = interval
		}
	}
	return ids, intervals, rows.Err()
}
func (s *Store) SaveBackupTask(ctx context.Context, t backup.Task) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var otherID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM backup_task WHERE id<>? LIMIT 1`, t.ID).Scan(&otherID)
	if err == nil {
		return errors.New("only one backup plan is supported")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if t.IntervalHours < 1 || t.IntervalHours > 720 {
		t.IntervalHours = 6
	}
	if t.Retention < 1 || t.Retention > 1000 {
		t.Retention = 10
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO backup_task(id,name,include_downloads,retention,enabled,interval_hours) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,include_downloads=excluded.include_downloads,retention=excluded.retention,enabled=excluded.enabled,interval_hours=excluded.interval_hours`, t.ID, t.Name, boolInt(t.IncludeDownloads), t.Retention, boolInt(t.Enabled), t.IntervalHours); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM backup_task_target WHERE task_id=?`, t.ID); err != nil {
		return err
	}
	for i, id := range t.TargetIDs {
		interval := t.TargetIntervalHours[id]
		if interval < 0 || interval > 720 {
			interval = 0
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO backup_task_target(task_id,target_id,position,interval_hours) VALUES(?,?,?,?)`, t.ID, id, i, interval); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteBackupTask(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_task_target WHERE task_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_task WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SaveBackupSnapshot(ctx context.Context, v backup.Snapshot) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO backup_run(id,task_id,created_at,size,status,retry_available,retry_unavailable_reason) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET task_id=excluded.task_id,created_at=excluded.created_at,size=excluded.size,status=excluded.status,retry_available=excluded.retry_available,retry_unavailable_reason=excluded.retry_unavailable_reason`, v.ID, v.TaskID, v.CreatedAt.Unix(), v.Size, v.Status, boolInt(v.RetryAvailable), v.RetryUnavailableReason)
	return err
}
func (s *Store) BackupSnapshots(ctx context.Context) ([]backup.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,created_at,size,status,retry_available,retry_unavailable_reason FROM backup_run ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Snapshot
	for rows.Next() {
		var v backup.Snapshot
		var ts int64
		var retryAvailable int
		if err := rows.Scan(&v.ID, &v.TaskID, &ts, &v.Size, &v.Status, &retryAvailable, &v.RetryUnavailableReason); err != nil {
			return nil, err
		}
		v.RetryAvailable = retryAvailable != 0
		v.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Targets, err = s.backupTargetRuns(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetBackupSnapshot(ctx context.Context, id string) (backup.Snapshot, error) {
	var v backup.Snapshot
	var ts int64
	var retryAvailable int
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,created_at,size,status,retry_available,retry_unavailable_reason FROM backup_run WHERE id=?`, id).Scan(&v.ID, &v.TaskID, &ts, &v.Size, &v.Status, &retryAvailable, &v.RetryUnavailableReason)
	if err != nil {
		return v, err
	}
	v.CreatedAt = time.Unix(ts, 0).UTC()
	v.RetryAvailable = retryAvailable != 0
	v.Targets, err = s.backupTargetRuns(ctx, id)
	return v, err
}

func (s *Store) backupTargetRuns(ctx context.Context, runID string) ([]backup.TargetRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT target_id,name,status,error_summary FROM backup_run_target WHERE run_id=? ORDER BY position,target_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.TargetRun
	for rows.Next() {
		var v backup.TargetRun
		if err := rows.Scan(&v.TargetID, &v.Name, &v.Status, &v.Error); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		var status string
		if err := s.db.QueryRowContext(ctx, `SELECT status FROM backup_run WHERE id=?`, runID).Scan(&status); err != nil {
			return nil, err
		}
		if status == "running" {
			return []backup.TargetRun{}, nil
		}
	}
	return out, nil
}

func (s *Store) SaveBackupTargetRuns(ctx context.Context, runID string, runs []backup.TargetRun) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_run_target WHERE run_id=?`, runID); err != nil {
		return err
	}
	succeeded, failed, pending := 0, 0, 0
	for i, run := range runs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO backup_run_target(run_id,target_id,name,status,error_summary,position) VALUES(?,?,?,?,?,?)`, runID, run.TargetID, run.Name, run.Status, run.Error, i); err != nil {
			return err
		}
		switch run.Status {
		case "succeeded":
			succeeded++
		case "failed":
			failed++
		case "pending":
			pending++
		}
	}
	status := "succeeded"
	if pending > 0 {
		status = "running"
	} else if failed > 0 && succeeded > 0 {
		status = "partial_success"
	} else if failed > 0 || succeeded == 0 {
		status = "failed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE backup_run SET status=?,retry_available=? WHERE id=?`, status, boolInt(status != "succeeded"), runID); err != nil {
		return err
	}
	return tx.Commit()
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
func (s *Store) GetSnapshot(ctx context.Context, id string) (backup.Snapshot, error) {
	return s.GetBackupSnapshot(ctx, id)
}
func (s *Store) SaveTargetRuns(ctx context.Context, id string, runs []backup.TargetRun) error {
	return s.SaveBackupTargetRuns(ctx, id, runs)
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
