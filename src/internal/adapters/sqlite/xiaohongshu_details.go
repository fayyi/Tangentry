package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func (s *Store) SaveXiaohongshuDetail(ctx context.Context, item content.Item) error {
	if item.Platform != "xiaohongshu" || content.Validate(item) != nil {
		return content.ErrInvalid
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO xiaohongshu_details(content_id,payload) VALUES(?,?) ON CONFLICT(content_id) DO UPDATE SET payload=excluded.payload`, item.ID, string(raw)); err != nil {
		return err
	}
	// Refresh existing locators so reopening a collected or recommended note
	// cannot resurrect an expired share URL. Keep saved content, timestamps
	// and recommendation read state intact.
	if _, err := tx.ExecContext(ctx, `UPDATE saved_content SET payload=json_set(payload,'$.url',?) WHERE id=? AND json_valid(payload)`, item.URL, item.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE recommendation_queue SET payload=json_set(payload,'$.url',?) WHERE platform='xiaohongshu' AND account=? AND content_id=? AND json_valid(payload)`, item.URL, discovery.PersonalAccount, item.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Complete details take precedence over feed covers. A feed locator remains
// useful even after the item has been opened and is no longer in the queue UI.
func (s *Store) XiaohongshuDetail(ctx context.Context, id string) (content.Item, bool, error) {
	if !strings.HasPrefix(id, "xiaohongshu:") || !content.ValidID(id) {
		return content.Item{}, false, content.ErrInvalid
	}
	var raw string
	complete := true
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM xiaohongshu_details WHERE content_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		complete = false
		err = s.db.QueryRowContext(ctx, `SELECT payload FROM recommendation_queue WHERE platform='xiaohongshu' AND account=? AND content_id=?`, discovery.PersonalAccount, id).Scan(&raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return content.Item{}, false, nil
	}
	if err != nil {
		return content.Item{}, false, err
	}
	var item content.Item
	if json.Unmarshal([]byte(raw), &item) != nil || item.ID != id || item.Platform != "xiaohongshu" || content.Validate(item) != nil {
		return content.Item{}, false, content.ErrInvalid
	}
	return item, complete, nil
}

// Cards may still use a previous feed cover after the reader obtains a newer
// album. Resolve registered images across all local copies of this one note.
func (s *Store) XiaohongshuImageCandidates(ctx context.Context, id string) ([]content.Item, error) {
	if !strings.HasPrefix(id, "xiaohongshu:") || !content.ValidID(id) {
		return nil, content.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM xiaohongshu_details WHERE content_id=?
		UNION ALL SELECT payload FROM saved_content WHERE id=?
		UNION ALL SELECT payload FROM recommendation_queue WHERE platform='xiaohongshu' AND account=? AND content_id=?`, id, id, discovery.PersonalAccount, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []content.Item{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item content.Item
		if json.Unmarshal([]byte(raw), &item) == nil && item.ID == id && item.Platform == "xiaohongshu" && content.Validate(item) == nil {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}
