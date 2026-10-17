package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tangentry/internal/subscriptions"
)

func (s *Store) GetSubscriptionRefresh(ctx context.Context, id string) (subscriptions.RefreshRecord, error) {
	record := subscriptions.DefaultRefresh()
	var payload sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT r.payload FROM subscriptions s LEFT JOIN subscription_refresh r ON r.id=s.id WHERE s.id=?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return record, subscriptions.ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if payload.Valid {
		err = json.Unmarshal([]byte(payload.String), &record)
	}
	return record, err
}

// A changed snapshot and its validators are committed together. A 304 writes
// only refresh metadata, but both paths use the same transaction boundary.
func (s *Store) CommitSubscriptionRefresh(ctx context.Context, id string, snapshot *subscriptions.Snapshot, updatedAt int64, record subscriptions.RefreshRecord) (subscriptions.Detail, error) {
	payload, err := json.Marshal(record)
	if err != nil {
		return subscriptions.Detail{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.Detail{}, err
	}
	defer tx.Rollback()
	detail, err := scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM subscriptions WHERE id=?`, id))
	if err != nil {
		return detail, err
	}
	if snapshot != nil {
		body, err := json.Marshal(snapshot)
		if err != nil {
			return detail, err
		}
		detail, err = scanSubscription(tx.QueryRowContext(ctx, `UPDATE subscriptions SET snapshot=?,updated_at=? WHERE id=? RETURNING `+subscriptionColumns, string(body), updatedAt, id))
		if err != nil {
			return detail, err
		}
		if err = archiveSubscriptionSnapshot(ctx, tx, detail.Source, snapshot.Items, updatedAt); err != nil {
			return detail, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscription_refresh(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, id, string(payload)); err != nil {
		return detail, err
	}
	if err = tx.Commit(); err != nil {
		return detail, err
	}
	return s.GetSubscription(ctx, id)
}
