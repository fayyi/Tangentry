package sqlite

import (
	"context"

	"tangentry/internal/discovery"
)

func (s *Store) ListDiscoveryFeedback(ctx context.Context, now int64) ([]discovery.Feedback, error) {
	if now <= 0 {
		return nil, discovery.ErrInvalidFeedback
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,target,value,expires_at,created_at FROM discovery_feedback WHERE expires_at=0 OR expires_at>? ORDER BY created_at DESC,id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]discovery.Feedback, 0)
	for rows.Next() {
		var value discovery.Feedback
		if err := rows.Scan(&value.ID, &value.Type, &value.Target, &value.Value, &value.ExpiresAt, &value.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Store) SaveDiscoveryFeedback(ctx context.Context, value discovery.Feedback) error {
	if value.ID == "" || value.CreatedAt <= 0 {
		return discovery.ErrInvalidFeedback
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO discovery_feedback(id,type,target,value,expires_at,created_at) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET type=excluded.type,target=excluded.target,value=excluded.value,expires_at=excluded.expires_at,created_at=excluded.created_at`, value.ID, value.Type, value.Target, value.Value, value.ExpiresAt, value.CreatedAt)
	return err
}

func (s *Store) DeleteDiscoveryFeedback(ctx context.Context, id string) error {
	if id == "" {
		return discovery.ErrInvalidFeedback
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM discovery_feedback WHERE id=?`, id)
	return err
}

func (s *Store) ClearDiscoveryFeedback(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM discovery_feedback`)
	return err
}
