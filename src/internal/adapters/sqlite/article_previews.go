package sqlite

import (
	"context"
	"encoding/json"
	"strings"

	"tangentry/internal/content"
)

// Previews are projections of cached original text, never replacements for feed
// text. A failed cache read must not prevent reading the confirmed content list.
func (s *Store) applyArticlePreviews(ctx context.Context, items []content.Item) {
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for i := range items {
		item := &items[i]
		item.PreviewText = ""
		if item.Platform == "rss" && item.Kind == "article" && !content.UsefulDescription(item.Description) && !seen[item.ID] {
			ids = append(ids, item.ID)
			seen[item.ID] = true
		}
	}
	previews := make(map[string]string)
	for start := 0; start < len(ids); start += 256 {
		encoded, err := json.Marshal(ids[start:min(start+256, len(ids))])
		if err != nil {
			return
		}
		rows, err := s.db.QueryContext(ctx, `SELECT content_id,
			(SELECT substr(json_extract(value,'$.text'),1,1200)
			 FROM json_each(json_extract(a.payload,'$.blocks'))
			 WHERE json_extract(value,'$.kind')='paragraph'
			   AND length(trim(json_extract(value,'$.text')))>=40
			 ORDER BY CAST(key AS INTEGER) LIMIT 1)
			FROM article_documents a WHERE content_id IN (SELECT value FROM json_each(?))`, string(encoded))
		if err != nil {
			return
		}
		failed := false
		for rows.Next() {
			var id string
			var text *string
			if err = rows.Scan(&id, &text); err != nil {
				failed = true
				break
			}
			if text != nil {
				normalized := []rune(strings.Join(strings.Fields(*text), " "))
				previews[id] = string(normalized[:min(len(normalized), 280)])
			}
		}
		err = rows.Err()
		rows.Close()
		if failed || err != nil {
			return
		}
	}
	for i := range items {
		if items[i].Platform == "rss" && items[i].Kind == "article" && !content.UsefulDescription(items[i].Description) {
			items[i].PreviewText = previews[items[i].ID]
		}
	}
}
