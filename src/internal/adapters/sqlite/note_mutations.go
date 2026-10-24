package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"tangentry/internal/notes"
)

// Pinning changes organization only, not the editor's optimistic body version.
func (s *Store) SetNotePinned(ctx context.Context, id string, pinned bool) (notes.Note, error) {
	return scanNote(s.db.QueryRowContext(ctx, `UPDATE notes SET pinned=? WHERE id=? RETURNING `+noteColumns, pinned, id))
}

func (s *Store) EditNote(ctx context.Context, id string, input notes.Edit, updatedAt int64) (notes.Note, error) {
	var anchor any
	if len(input.TextAnchor) > 0 && string(input.TextAnchor) != "null" {
		anchor = string(input.TextAnchor)
	}
	var pdfAnchor any
	if len(input.PDFAnchor) > 0 && string(input.PDFAnchor) != "null" {
		pdfAnchor = string(input.PDFAnchor)
	}
	var summaryAnchor any
	if len(input.SummaryAnchor) > 0 && string(input.SummaryAnchor) != "null" {
		summaryAnchor = string(input.SummaryAnchor)
	}
	var tags any
	var references any
	if len(input.SubtitleReferences) > 0 {
		references = string(input.SubtitleReferences)
	}
	if input.Tags != nil {
		data, err := json.Marshal(*input.Tags)
		if err != nil {
			return notes.Note{}, err
		}
		tags = string(data)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return notes.Note{}, err
	}
	defer tx.Rollback()
	note, err := scanNote(tx.QueryRowContext(ctx, `UPDATE notes SET body=?, position_seconds=?, updated_at=MAX(?, updated_at+1), text_anchor=CASE WHEN ? THEN ? ELSE text_anchor END, pdf_anchor=CASE WHEN ? THEN ? ELSE pdf_anchor END, summary_anchor=CASE WHEN ? THEN ? ELSE summary_anchor END, tags=CASE WHEN ? THEN ? ELSE tags END, subtitle_references=CASE WHEN ? THEN ? ELSE subtitle_references END WHERE id=? AND (? IS NULL OR updated_at=?) RETURNING `+noteColumns, input.Body, input.PositionSeconds, updatedAt, len(input.TextAnchor) > 0, anchor, len(input.PDFAnchor) > 0, pdfAnchor, len(input.SummaryAnchor) > 0, summaryAnchor, input.Tags != nil, tags, len(input.SubtitleReferences) > 0, references, id, input.ExpectedUpdatedAt, input.ExpectedUpdatedAt))
	if errors.Is(err, notes.ErrNotFound) && input.ExpectedUpdatedAt != nil {
		var exists int
		if lookup := tx.QueryRowContext(ctx, `SELECT 1 FROM notes WHERE id=?`, id).Scan(&exists); lookup == nil {
			return notes.Note{}, notes.ErrConflict
		} else if !errors.Is(lookup, sql.ErrNoRows) {
			return notes.Note{}, lookup
		}
	}
	if err != nil {
		return notes.Note{}, err
	}
	if note.TextAnchor != nil && note.PDFAnchor != nil {
		return notes.Note{}, notes.ErrInvalid
	}
	if len(note.SubtitleReferences) > 0 && (note.TextAnchor != nil || note.PDFAnchor != nil) {
		return notes.Note{}, notes.ErrInvalid
	}
	if strings.HasPrefix(note.ContentID, "memo:") && (note.PositionSeconds != nil || note.TextAnchor != nil || note.PDFAnchor != nil || note.SummaryAnchor != nil || len(note.SubtitleReferences) > 0) {
		return notes.Note{}, notes.ErrInvalid
	}
	if err := tx.Commit(); err != nil {
		return notes.Note{}, err
	}
	return note, nil
}

func (s *Store) DeleteNote(ctx context.Context, id string, version ...int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `DELETE FROM notes WHERE id=?`
	args := []any{id}
	if len(version) > 0 {
		query += ` AND updated_at=?`
		args = append(args, version[0])
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		if len(version) > 0 {
			var exists int
			if lookup := tx.QueryRowContext(ctx, `SELECT 1 FROM notes WHERE id=?`, id).Scan(&exists); lookup == nil {
				return notes.ErrConflict
			} else if !errors.Is(lookup, sql.ErrNoRows) {
				return lookup
			}
		}
		return notes.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_frames WHERE note_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
