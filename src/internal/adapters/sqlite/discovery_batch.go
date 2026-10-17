package sqlite

import (
	"context"

	"tangentry/internal/discovery"
)

func (s *Store) PutDiscoveryBatch(ctx context.Context, id, requestKey string, payload []byte, expiresAt int64) error {
	if id == "" || requestKey == "" || len(payload) == 0 || expiresAt <= 0 {
		return discovery.ErrInvalidBatch
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO discovery_batch_snapshots(id,request_key,payload,expires_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET request_key=excluded.request_key,payload=excluded.payload,expires_at=excluded.expires_at`, id, requestKey, string(payload), expiresAt)
	return err
}

func (s *Store) GetDiscoveryBatch(ctx context.Context, id, requestKey string, now int64) ([]byte, error) {
	if id == "" || requestKey == "" || now <= 0 {
		return nil, discovery.ErrInvalidBatch
	}
	var payload string
	if err := s.db.QueryRowContext(ctx, `SELECT payload FROM discovery_batch_snapshots WHERE id=? AND request_key=? AND expires_at>?`, id, requestKey, now).Scan(&payload); err != nil {
		return nil, discovery.ErrUnavailable
	}
	return []byte(payload), nil
}
