package sqlite

import (
	"context"
	"encoding/json"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func (s *Store) PageQueuedRecommendations(ctx context.Context, scope discovery.QueueScope, cursor discovery.QueueCursor, limit int) ([]discovery.QueuedItem, error) {
	if !discovery.ValidQueueScope(scope) || cursor.Account != scope.Account || cursor.Snapshot <= 0 || limit < 1 || limit > 501 {
		return nil, discovery.ErrInvalidQuery
	}
	// Freeze seen priority at the snapshot; later exposure marks cannot move rows
	// across the cursor. Opening content may safely remove it from future pages.
	rows, err := s.db.QueryContext(ctx, `SELECT content_id,payload,fetched_at,seen_at,opened_at FROM recommendation_queue
	 WHERE platform=? AND account=? AND opened_at=0 AND fetched_at<=?
	 AND (?='' OR (seen_at>0 AND seen_at<=?)>? OR
	 ((seen_at>0 AND seen_at<=?)=? AND (fetched_at>? OR (fetched_at=? AND content_id>?))))
	 ORDER BY (seen_at>0 AND seen_at<=?),fetched_at,content_id`,
		scope.Platform, scope.Account, cursor.Snapshot, cursor.ID, cursor.Snapshot, cursor.Seen,
		cursor.Snapshot, cursor.Seen, cursor.FetchedAt, cursor.FetchedAt, cursor.ID, cursor.Snapshot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]discovery.QueuedItem, 0)
	for rows.Next() {
		var item discovery.QueuedItem
		var id, raw string
		if err := rows.Scan(&id, &raw, &item.FetchedAt, &item.SeenAt, &item.OpenedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(raw), &item.Item) != nil || item.Item.ID != id || item.Item.Platform != scope.Platform || content.Validate(item.Item) != nil {
			continue
		}
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return items, rows.Err()
}
