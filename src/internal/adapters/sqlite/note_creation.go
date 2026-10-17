package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"tangentry/internal/notes"
)

type noteCreationReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Receipts survive deletion, so retrying an old POST cannot resurrect a note.
func findNoteCreation(ctx context.Context, reader noteCreationReader, id, fingerprint string) (notes.Note, bool, error) {
	var savedHash, noteID string
	err := reader.QueryRowContext(ctx, `SELECT fingerprint,note_id FROM note_creations WHERE id=?`, id).Scan(&savedHash, &noteID)
	if errors.Is(err, sql.ErrNoRows) {
		return notes.Note{}, false, nil
	}
	if err != nil {
		return notes.Note{}, false, err
	}
	if savedHash != fingerprint {
		return notes.Note{}, true, notes.ErrCreationConflict
	}
	note, err := scanNote(reader.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id=?`, noteID))
	if errors.Is(err, notes.ErrNotFound) {
		return notes.Note{}, true, notes.ErrCreationDeleted
	}
	return note, true, err
}

func (s *Store) FindNoteCreation(ctx context.Context, id, fingerprint string) (notes.Note, bool, error) {
	return findNoteCreation(ctx, s.db, id, fingerprint)
}
