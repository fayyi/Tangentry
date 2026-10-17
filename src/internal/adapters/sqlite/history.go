package sqlite

import (
	"context"
	"encoding/json"

	"tangentry/internal/history"
)

func (s *Store) ListViewing(ctx context.Context) ([]history.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item, opened_at, open_count FROM viewing_history ORDER BY opened_at DESC, content_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []history.Entry{}
	for rows.Next() {
		var item string
		var entry history.Entry
		if err := rows.Scan(&item, &entry.OpenedAt, &entry.OpenCount); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(item), &entry.Item); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// RecordViewing keeps the newest snapshot of each item and counts visits.
func (s *Store) RecordViewing(ctx context.Context, entries []history.Entry, keep int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, entry := range entries {
		item, err := json.Marshal(entry.Item)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO viewing_history(content_id,item,opened_at,open_count) VALUES(?,?,?,?)
		 ON CONFLICT(content_id) DO UPDATE SET
		 item=CASE WHEN excluded.opened_at>=viewing_history.opened_at THEN excluded.item ELSE viewing_history.item END,
		 opened_at=MAX(excluded.opened_at,viewing_history.opened_at),
		 open_count=viewing_history.open_count+excluded.open_count`, entry.Item.ID, string(item), entry.OpenedAt, entry.OpenCount); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM viewing_history WHERE content_id NOT IN (SELECT content_id FROM viewing_history ORDER BY opened_at DESC, content_id LIMIT ?)`, keep); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteViewing(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM viewing_history WHERE content_id=?`, id)
	return err
}

func (s *Store) ClearViewing(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM viewing_history`)
	return err
}

func (s *Store) ListSearches(ctx context.Context) ([]history.Search, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, query, source, created_at FROM search_history ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []history.Search{}
	for rows.Next() {
		var search history.Search
		if err := rows.Scan(&search.ID, &search.Query, &search.Source, &search.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, search)
	}
	return out, rows.Err()
}

func (s *Store) AddSearches(ctx context.Context, searches []history.Search, keep int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, search := range searches {
		if _, err := tx.ExecContext(ctx, `INSERT INTO search_history(id,query,source,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`, search.ID, search.Query, search.Source, search.CreatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_history WHERE id NOT IN (SELECT id FROM search_history ORDER BY created_at DESC, id LIMIT ?)`, keep); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteSearch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_history WHERE id=?`, id)
	return err
}

func (s *Store) ClearSearches(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM search_history`)
	return err
}
