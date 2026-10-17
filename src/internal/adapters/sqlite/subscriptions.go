package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"tangentry/internal/subscriptions"
)

func (s *Store) CreateSubscription(ctx context.Context, source subscriptions.Source) (subscriptions.Source, bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO subscriptions(id,creator_id,name,created_at,updated_at,snapshot) VALUES(?,?,?,?,0,'')`, source.ID, source.CreatorID, source.Name, source.CreatedAt)
	if err != nil {
		return subscriptions.Source{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return subscriptions.Source{}, false, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE creator_id=? OR id=? ORDER BY CASE WHEN creator_id=? THEN 0 ELSE 1 END LIMIT 1`, source.CreatorID, source.ID, source.CreatorID).Scan(&id)
	if err != nil {
		return subscriptions.Source{}, false, err
	}
	detail, err := s.GetSubscription(ctx, id)
	return detail.Source, count == 1, err
}
func (s *Store) RenameSubscription(ctx context.Context, id, name string) (subscriptions.Source, error) {
	detail, err := scanSubscription(s.db.QueryRowContext(ctx, `UPDATE subscriptions SET name=? WHERE id=? RETURNING `+subscriptionColumns, name, id))
	if err == nil {
		detail, err = s.GetSubscription(ctx, id)
	}
	return detail.Source, err
}
func (s *Store) UpdateSubscription(ctx context.Context, id string, snapshot subscriptions.Snapshot, updatedAt int64) (subscriptions.Detail, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return subscriptions.Detail{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.Detail{}, err
	}
	defer tx.Rollback()
	detail, err := scanSubscription(tx.QueryRowContext(ctx, `UPDATE subscriptions SET snapshot=?,updated_at=? WHERE id=? RETURNING `+subscriptionColumns, string(payload), updatedAt, id))
	if err != nil {
		return detail, err
	}
	if err = archiveSubscriptionSnapshot(ctx, tx, detail.Source, snapshot.Items, updatedAt); err != nil {
		return detail, err
	}
	if err = tx.Commit(); err != nil {
		return detail, err
	}
	return s.GetSubscription(ctx, id)
}
func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE subscription_inbox_sources SET source_name=(SELECT name FROM subscriptions WHERE id=?) WHERE source_id=? AND EXISTS(SELECT 1 FROM subscriptions WHERE id=?)`, id, id, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return subscriptions.ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_refresh WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_group_members WHERE source_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_feed_identity WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EditSubscriptionAddress(ctx context.Context, id, expected, address string, now int64) (subscriptions.Source, error) {
	if !strings.HasPrefix(id, "rss:feed:") {
		return subscriptions.Source{}, subscriptions.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return subscriptions.Source{}, err
	}
	defer tx.Rollback()
	detail, err := scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subscriptionJoinedColumns+` FROM subscriptions s`+subscriptionJoins+`WHERE s.id=?`, id), true)
	if err != nil {
		return subscriptions.Source{}, err
	}
	if detail.Source.URL != expected {
		return subscriptions.Source{}, subscriptions.ErrAddressConflict
	}
	if address == expected {
		return detail.Source, nil
	}
	var duplicate bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE creator_id=? AND id<>?)`, address, id).Scan(&duplicate); err != nil {
		return subscriptions.Source{}, err
	}
	if duplicate {
		return subscriptions.Source{}, subscriptions.ErrAddressExists
	}
	var raw sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM subscription_refresh WHERE id=?`, id).Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return subscriptions.Source{}, err
	}
	record := subscriptions.DefaultRefresh()
	if raw.Valid {
		if err = json.Unmarshal([]byte(raw.String), &record); err != nil {
			return subscriptions.Source{}, err
		}
	}
	record.Validators = subscriptions.Validators{}
	record.State.LastAttemptAt, record.State.LastCheckedAt, record.State.Failures = 0, 0, 0
	record.State.Message, record.State.NextCheckAt = "", 0
	if record.State.Enabled {
		record.State.NextCheckAt = now + record.State.IntervalSeconds
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return subscriptions.Source{}, err
	}
	// The original parser namespace must survive every subsequent address edit.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_feed_identity(id,canonical) VALUES(?,?)`, id, detail.Source.IdentityURL); err != nil {
		return subscriptions.Source{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE subscriptions SET creator_id=? WHERE id=?`, address, id); err != nil {
		return subscriptions.Source{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscription_refresh(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, id, string(payload)); err != nil {
		return subscriptions.Source{}, err
	}
	if err = tx.Commit(); err != nil {
		return subscriptions.Source{}, err
	}
	saved, err := s.GetSubscription(ctx, id)
	return saved.Source, err
}
