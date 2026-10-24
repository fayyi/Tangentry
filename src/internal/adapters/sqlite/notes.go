package sqlite

import (
	"context"

	"tangentry/internal/notes"
)

const noteColumns = `id, content_id, body, position_seconds, created_at, updated_at, text_anchor, pdf_anchor, summary_anchor, tags, pinned, subtitle_references, frame`

func (s *Store) ListNotes(ctx context.Context, contentID string) ([]notes.Note, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE content_id=? ORDER BY created_at DESC, rowid DESC`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notes.Note, 0)
	for rows.Next() {
		note, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, note)
	}
	return items, rows.Err()
}
