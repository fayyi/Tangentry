package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"tangentry/internal/notes"
)

// scanNote is the single wire-format decoder for persisted notes. Keeping the
// compatibility handling here means mutations and list queries share exactly
// the same legacy frame behavior.
func scanNote(row interface{ Scan(...any) error }) (notes.Note, error) {
	var note notes.Note
	var position sql.NullFloat64
	var anchor sql.NullString
	var pdfAnchor sql.NullString
	var tags sql.NullString
	var references sql.NullString
	var frame sql.NullString
	note.Tags = []string{}
	err := row.Scan(&note.ID, &note.ContentID, &note.Body, &position, &note.CreatedAt, &note.UpdatedAt, &anchor, &pdfAnchor, &tags, &note.Pinned, &references, &frame)
	if errors.Is(err, sql.ErrNoRows) {
		return notes.Note{}, notes.ErrNotFound
	}
	if position.Valid {
		note.PositionSeconds = &position.Float64
	}
	if err == nil && anchor.Valid {
		err = json.Unmarshal([]byte(anchor.String), &note.TextAnchor)
	}
	if err == nil && pdfAnchor.Valid {
		err = json.Unmarshal([]byte(pdfAnchor.String), &note.PDFAnchor)
	}
	if err == nil && tags.Valid {
		err = json.Unmarshal([]byte(tags.String), &note.Tags)
		if note.Tags == nil {
			note.Tags = []string{}
		}
	}
	if err == nil && references.Valid {
		err = json.Unmarshal([]byte(references.String), &note.SubtitleReferences)
	}
	if err == nil && frame.Valid {
		if strings.HasPrefix(strings.TrimSpace(frame.String), "[") {
			err = json.Unmarshal([]byte(frame.String), &note.Frames)
		} else {
			err = json.Unmarshal([]byte(frame.String), &note.Frame)
			if err == nil && note.Frame != nil {
				note.Frames = []notes.Frame{*note.Frame}
			}
		}
	}
	if note.Frames == nil {
		note.Frames = []notes.Frame{}
	}
	if len(note.Frames) > 0 {
		first := note.Frames[0]
		note.Frame = &first
	}
	return note, err
}
