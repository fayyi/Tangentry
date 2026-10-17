package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tangentry/internal/favoritesync"
)

func (s *Store) FavoriteRestriction(ctx context.Context) (int64, error) {
	var until int64
	err := s.db.QueryRowContext(ctx, `SELECT until_at FROM favorite_sync_restriction WHERE id=1`).Scan(&until)
	return until, err
}

func (s *Store) ExtendFavoriteRestriction(ctx context.Context, until int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO favorite_sync_restriction(id,until_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET until_at=MAX(until_at,excluded.until_at)`, until)
	return err
}

func scanFavorite(row interface{ Scan(...any) error }) (favoritesync.Source, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return favoritesync.Source{}, favoritesync.ErrNotFound
		}
		return favoritesync.Source{}, err
	}
	var source favoritesync.Source
	if err := json.Unmarshal([]byte(payload), &source); err != nil {
		return source, err
	}
	source.Active = false
	return source, nil
}
func (s *Store) ListFavoriteSources(ctx context.Context) ([]favoritesync.Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM favorite_sync ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]favoritesync.Source, 0)
	for rows.Next() {
		source, err := scanFavorite(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, source)
	}
	return items, rows.Err()
}
func (s *Store) GetFavoriteSource(ctx context.Context, id string) (favoritesync.Source, error) {
	return scanFavorite(s.db.QueryRowContext(ctx, `SELECT payload FROM favorite_sync WHERE id=?`, id))
}
func (s *Store) PutFavoriteSource(ctx context.Context, source favoritesync.Source) error {
	source.Active = false
	payload, err := json.Marshal(source)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO favorite_sync(id,payload,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, source.FolderID, string(payload), source.CreatedAt)
	return err
}
func (s *Store) DeleteFavoriteSource(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM favorite_sync WHERE id=?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return favoritesync.ErrNotFound
	}
	return nil
}
