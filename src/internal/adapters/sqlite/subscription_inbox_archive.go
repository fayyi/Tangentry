package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
)

// Seed existing confirmed snapshots once, in the same transaction as schema 14.
// Reads never create history. Original snapshots, refresh policies and user state
// are untouched. The immutable sequence orders first arrival, not publish time.
func archiveLegacySnapshots(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT ` + subscriptionColumns + ` FROM subscriptions WHERE snapshot<>'' ORDER BY updated_at,id`)
	if err != nil {
		return err
	}
	details := []subscriptions.Detail{}
	for rows.Next() {
		detail, err := scanSubscription(rows)
		if err != nil {
			rows.Close()
			return err
		}
		details = append(details, detail)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, detail := range details {
		if err = archiveSubscriptionSnapshot(context.Background(), tx, detail.Source, detail.Items, detail.Source.LastUpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

// The caller owns the snapshot transaction, so a failed archive cannot replace
// the source snapshot or commit fresh HTTP validators without its history.
func archiveSubscriptionSnapshot(ctx context.Context, tx *sql.Tx, source subscriptions.Source, items []content.Item, seenAt int64) error {
	// Insert oldest first so the default descending first-arrival order presents
	// a source's newest item first, including on legacy seed.
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		item.SavedAt = 0
		item.PreviewText = ""
		if err := content.Validate(item); err != nil {
			return err
		}
		body, err := json.Marshal(item)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO subscription_inbox(content_id,payload,title,author,kind,first_seen_at,last_seen_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(content_id) DO UPDATE SET payload=excluded.payload,title=excluded.title,author=excluded.author,kind=excluded.kind,last_seen_at=MAX(subscription_inbox.last_seen_at,excluded.last_seen_at) WHERE excluded.last_seen_at>=subscription_inbox.last_seen_at`, item.ID, string(body), item.Title, item.Author, item.Kind, seenAt, seenAt)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO subscription_inbox_sources(content_id,source_id,source_name) VALUES(?,?,?) ON CONFLICT(content_id,source_id) DO UPDATE SET source_name=excluded.source_name`, item.ID, source.ID, source.Name); err != nil {
			return err
		}
	}
	return nil
}
