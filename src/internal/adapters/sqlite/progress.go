package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"tangentry/internal/readingprogress"
)

const progressColumns = `content_id, position_seconds, duration_seconds, completed, revision, updated_at`

func (s *Store) ListReadingProgress(ctx context.Context, ids []string) ([]readingprogress.Progress, error) {
	result := make([]readingprogress.Progress, 0)
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := s.db.QueryContext(ctx, `SELECT `+progressColumns+` FROM reading_progress WHERE content_id IN (`+placeholders+`) ORDER BY content_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		value, err := scanProgress(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func scanProgress(row interface{ Scan(...any) error }) (readingprogress.Progress, error) {
	var value readingprogress.Progress
	err := row.Scan(&value.ContentID, &value.PositionSeconds, &value.DurationSeconds, &value.Completed, &value.Revision, &value.UpdatedAt)
	return value, err
}
func (s *Store) GetReadingProgress(ctx context.Context, id string) (*readingprogress.Progress, error) {
	value, err := scanProgress(s.db.QueryRowContext(ctx, `SELECT `+progressColumns+` FROM reading_progress WHERE content_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func (s *Store) SaveReadingProgress(ctx context.Context, value readingprogress.Progress, expected int64) (readingprogress.Progress, error) {
	var result readingprogress.Progress
	var err error
	if expected == 0 {
		result, err = scanProgress(s.db.QueryRowContext(ctx, `INSERT INTO reading_progress (`+progressColumns+`) VALUES(?,?,?,?,?,?) ON CONFLICT(content_id) DO NOTHING RETURNING `+progressColumns, value.ContentID, value.PositionSeconds, value.DurationSeconds, value.Completed, value.Revision, value.UpdatedAt))
	} else {
		result, err = scanProgress(s.db.QueryRowContext(ctx, `UPDATE reading_progress SET position_seconds=?,duration_seconds=?,completed=?,revision=?,updated_at=? WHERE content_id=? AND revision=? RETURNING `+progressColumns, value.PositionSeconds, value.DurationSeconds, value.Completed, value.Revision, value.UpdatedAt, value.ContentID, expected))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return readingprogress.Progress{}, readingprogress.ErrConflict
	}
	return result, err
}
