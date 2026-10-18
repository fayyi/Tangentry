package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
)

func (s *Store) GetSubscriptionInboxItem(ctx context.Context, id string) (content.Item, error) {
	var item content.Item
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM subscription_inbox WHERE content_id=?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return item, subscriptions.ErrInboxNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(payload), &item)
	}
	if err == nil {
		items := []content.Item{item}
		s.applyArticlePreviews(ctx, items)
		item = items[0]
	}
	return item, err
}
func (s *Store) ListSubscriptionInbox(ctx context.Context, q subscriptions.InboxQuery, p subscriptions.InboxPosition) (subscriptions.InboxPage, subscriptions.InboxPosition, error) {
	page := subscriptions.InboxPage{Entries: []subscriptions.InboxEntry{}, Sources: []subscriptions.InboxSource{}}
	limit := q.Limit
	if limit == 0 {
		limit = subscriptions.InboxPageSize
	}
	switch limit {
	case 10, 20, 30, 50:
	default:
		return page, p, subscriptions.ErrInboxInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, p, err
	}
	defer tx.Rollback()
	if p.Ceiling == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM subscription_inbox`).Scan(&p.Ceiling); err != nil {
			return page, p, err
		}
	}
	conditions := []string{"i.sequence<=?"}
	args := []any{p.Ceiling}
	if p.Before > 0 {
		conditions = append(conditions, "i.sequence<?")
		args = append(args, p.Before)
	}
	filtered, filterArgs := subscriptionInboxConditions(q)
	conditions = append(conditions, filtered[1:]...)
	args = append(args, filterArgs...)
	rows, err := tx.QueryContext(ctx, `SELECT i.sequence,i.payload,i.first_seen_at,i.last_seen_at FROM subscription_inbox i LEFT JOIN read_state r ON r.content_id=i.content_id WHERE `+strings.Join(conditions, " AND ")+` ORDER BY i.sequence DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return page, p, err
	}
	sequences := []int64{}
	for rows.Next() {
		var entry subscriptions.InboxEntry
		var sequence int64
		var payload string
		if err = rows.Scan(&sequence, &payload, &entry.FirstSeenAt, &entry.LastSeenAt); err != nil {
			rows.Close()
			return page, p, err
		}
		if err = json.Unmarshal([]byte(payload), &entry.Item); err != nil {
			rows.Close()
			return page, p, err
		}
		sequences = append(sequences, sequence)
		page.Entries = append(page.Entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, p, err
	}
	next := subscriptions.InboxPosition{}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		next = p
		next.Before = sequences[limit-1]
	}
	for i := range page.Entries {
		entry := &page.Entries[i]
		entry.Sources = []subscriptions.InboxSource{}
		rows, err = tx.QueryContext(ctx, `SELECT a.source_id,COALESCE(s.name,a.source_name),s.id IS NULL FROM subscription_inbox_sources a LEFT JOIN subscriptions s ON s.id=a.source_id WHERE a.content_id=? ORDER BY a.source_id`, entry.Item.ID)
		if err != nil {
			return page, p, err
		}
		for rows.Next() {
			var source subscriptions.InboxSource
			if err = rows.Scan(&source.ID, &source.Name, &source.Removed); err != nil {
				rows.Close()
				return page, p, err
			}
			entry.Sources = append(entry.Sources, source)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, p, err
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT DISTINCT a.source_id,COALESCE(s.name,a.source_name),s.id IS NULL FROM subscription_inbox_sources a LEFT JOIN subscriptions s ON s.id=a.source_id ORDER BY a.source_id`)
	if err != nil {
		return page, p, err
	}
	for rows.Next() {
		var source subscriptions.InboxSource
		if err = rows.Scan(&source.ID, &source.Name, &source.Removed); err != nil {
			rows.Close()
			return page, p, err
		}
		page.Sources = append(page.Sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, p, err
	}
	if err = tx.Commit(); err != nil {
		return page, p, err
	}
	items := make([]content.Item, len(page.Entries))
	for i := range page.Entries {
		items[i] = page.Entries[i].Item
	}
	s.applyArticlePreviews(ctx, items)
	for i := range page.Entries {
		page.Entries[i].Item = items[i]
	}
	return page, next, nil
}

func (s *Store) CountSubscriptionInbox(ctx context.Context, q subscriptions.InboxQuery) (int, error) {
	conditions, args := subscriptionInboxConditions(q)
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_inbox i LEFT JOIN read_state r ON r.content_id=i.content_id WHERE `+strings.Join(conditions, " AND "), args...).Scan(&count)
	return count, err
}

func ftsQuery(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func subscriptionInboxConditions(q subscriptions.InboxQuery) ([]string, []any) {
	conditions := []string{"1=1"}
	args := []any{}
	if q.Kind == "graphical" {
		conditions = append(conditions, "i.kind IN ('article','image')")
	} else if q.Kind != "" {
		conditions = append(conditions, "i.kind=?")
		args = append(args, q.Kind)
	}
	if q.Query != "" {
		for _, term := range strings.Fields(q.Query) {
			if len([]rune(term)) >= 3 {
				conditions = append(conditions, `i.content_id IN (SELECT content_id FROM subscription_inbox_search WHERE subscription_inbox_search MATCH ?)`)
				args = append(args, ftsQuery(term))
			} else {
				pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(term) + "%"
				conditions = append(conditions, `i.content_id IN (SELECT content_id FROM subscription_inbox_search WHERE title LIKE ? ESCAPE '\' OR author LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')`)
				args = append(args, pattern, pattern, pattern)
			}
		}
	}
	if q.Platform != "" {
		conditions = append(conditions, `json_extract(i.payload,'$.platform')=?`)
		args = append(args, q.Platform)
	}
	if q.Topic != "" {
		conditions = append(conditions, `(json_extract(i.payload,'$.category')=? OR json_extract(i.payload,'$.topicId')=?)`)
		args = append(args, q.Topic, q.Topic)
	}
	if q.Since > 0 {
		conditions = append(conditions, "json_extract(i.payload,'$.publishedAt')>=?")
		args = append(args, q.Since)
	}
	if q.Until > 0 {
		conditions = append(conditions, "json_extract(i.payload,'$.publishedAt')<=?")
		args = append(args, q.Until)
	}
	if q.Favorite {
		conditions = append(conditions, `EXISTS(SELECT 1 FROM saved_content saved WHERE saved.id=i.content_id)`)
	}
	if q.Tag != "" {
		conditions = append(conditions, `EXISTS(SELECT 1 FROM notes n,json_each(COALESCE(n.tags,'[]')) t WHERE n.content_id=i.content_id AND t.type='text' AND t.value=?)`)
		args = append(args, q.Tag)
	}
	if q.Read != "" && q.Read != "all" {
		read := 0
		if q.Read == "read" {
			read = 1
		}
		conditions = append(conditions, "COALESCE(r.is_read,0)=?")
		args = append(args, read)
	}
	if q.SourceID != "" || q.GroupID != "" {
		match := `EXISTS(SELECT 1 FROM subscription_inbox_sources a LEFT JOIN subscription_group_members m ON m.source_id=a.source_id WHERE a.content_id=i.content_id`
		if q.SourceID != "" {
			match += " AND a.source_id=?"
			args = append(args, q.SourceID)
		}
		if q.GroupID != "" {
			match += " AND m.group_id=?"
			args = append(args, q.GroupID)
		}
		conditions = append(conditions, match+")")
	}
	return conditions, args
}
