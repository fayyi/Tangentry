package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tangentry/internal/downloads"
)

func (s *Store) ListDownloads(ctx context.Context) ([]downloads.Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM downloads ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]downloads.Job, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var job downloads.Job
		if err := json.Unmarshal([]byte(payload), &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetDownload(ctx context.Context, id string) (downloads.Job, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM downloads WHERE id=?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return downloads.Job{}, downloads.ErrNotFound
	}
	if err != nil {
		return downloads.Job{}, err
	}
	var job downloads.Job
	err = json.Unmarshal([]byte(payload), &job)
	return job, err
}

func (s *Store) SaveDownload(ctx context.Context, job downloads.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO downloads(id,payload,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, job.ID, string(payload), job.CreatedAt)
	return err
}

func (s *Store) DeleteDownload(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM downloads WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return downloads.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDownloads(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM downloads WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
