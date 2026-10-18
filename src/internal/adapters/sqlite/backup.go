package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tangentry/internal/backup"
)

// Snapshot writes a consistent SQLite backup using VACUUM INTO. It never
// copies the live WAL-mode database file directly.
func (s *Store) Snapshot(ctx context.Context, staging string, includeDownloads bool) ([]backup.File, error) {
	dbPath := filepath.Join(staging, "tangentry.db")
	if err := os.MkdirAll(staging, 0700); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dbPath); err != nil {
		return nil, fmt.Errorf("sqlite consistency snapshot: %w", err)
	}
	files, err := describeFiles(staging, dbPath)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func describeFiles(root, dbPath string) ([]backup.File, error) {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}
	sum := fmt.Sprintf("%x", sha256Sum(data))
	return []backup.File{{Path: "tangentry.db", Type: backup.FileDatabase, Size: int64(len(data)), SHA256: sum}}, nil
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
