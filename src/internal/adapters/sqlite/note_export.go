package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/notes"
)

type exportNoteScanner struct {
	rows    *sql.Rows
	payload string
}

func (s *exportNoteScanner) Scan(values ...any) error {
	return s.rows.Scan(append(values, &s.payload)...)
}

// A single SELECT holds one SQLite read snapshot, including optional local source
// metadata. Exporting never pages through a changing series of search results.
func (s *Store) ExportNotes(ctx context.Context, q notes.SearchQuery, contentID string) ([]notes.ExportEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	columns := "n." + strings.ReplaceAll(noteColumns, ", ", ", n.")
	rows, err := tx.QueryContext(ctx, noteSources+`SELECT `+columns+`,n.source_payload FROM note_sources n
	WHERE `+noteTextMatch+`
	AND (?=0 OR n.pinned=1)
	AND `+noteTagMatch+`
	AND (?=0 OR n.created_at>=?) AND (?=0 OR n.created_at<?)
	AND (?='' OR n.content_id=?)
	ORDER BY n.created_at DESC,n.id DESC LIMIT ?`, q.Text, q.Text, q.Text, q.Text, q.Text, q.Text, q.Text, q.PinnedOnly, q.Tag, q.Tag, q.Tag, q.FromCreatedAt, q.FromCreatedAt, q.ToCreatedAt, q.ToCreatedAt, contentID, contentID, notes.ExportMaxNotes+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]notes.ExportEntry, 0)
	size := 0
	for rows.Next() {
		if len(entries) == notes.ExportMaxNotes {
			return nil, notes.ErrExportLimit
		}
		scanner := &exportNoteScanner{rows: rows}
		note, err := scanNote(scanner)
		if err != nil {
			return nil, err
		}
		entry := notes.ExportEntry{Note: note}
		if scanner.payload != "" {
			var source content.Item
			if err = json.Unmarshal([]byte(scanner.payload), &source); err != nil {
				return nil, err
			}
			if source.ID != note.ContentID {
				return nil, notes.ErrExportUnavailable
			}
			entry.Source = &notes.ExportSource{Title: source.Title, URL: source.URL, Author: source.Author, Platform: source.Platform, Kind: source.Kind}
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		size += len(data)
		if size > notes.ExportMaxSnapshotBytes {
			return nil, notes.ErrExportLimit
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range entries {
		entry := &entries[index]
		entry.FrameMissingIDs = make(map[string]bool)
		frames := entry.Note.Frames
		if len(frames) == 0 && entry.Note.Frame != nil {
			frames = []notes.Frame{*entry.Note.Frame}
		}
		missing := false
		for _, frame := range frames {
			var data []byte
			if err := tx.QueryRowContext(ctx, `SELECT data FROM note_frames WHERE note_id=? AND frame_id=?`, entry.Note.ID, frame.ID).Scan(&data); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				missing = true
				entry.FrameMissingIDs[frame.ID] = true
				entry.FramesData = append(entry.FramesData, nil)
				continue
			}
			size += len(data)
			if size > notes.ExportMaxArchiveBytes {
				return nil, notes.ErrExportLimit
			}
			entry.FramesData = append(entry.FramesData, data)
			if len(entry.FramesData) == 1 {
				entry.FrameData = data
			}
		}
		entry.FrameMissing = missing
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}
