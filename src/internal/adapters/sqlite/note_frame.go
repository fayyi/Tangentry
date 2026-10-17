package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"tangentry/internal/notes"
)

func (s *Store) GetNote(ctx context.Context, id string) (notes.Note, error) {
	return scanNote(s.db.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id=?`, id))
}

func (s *Store) NoteFrameData(ctx context.Context, id, frameID string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT f.data FROM note_frames f JOIN notes n ON n.id=f.note_id WHERE f.note_id=? AND f.frame_id=?`, id, frameID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notes.ErrNotFound
	}
	return data, err
}

func (s *Store) AppendNoteFrame(ctx context.Context, id string, version int64, frame *notes.Frame, data []byte) (notes.Note, error) {
	if notes.ValidateFrame(frame) != nil || frame == nil || len(data) == 0 || len(data) > notes.MaxFrameBytes {
		return notes.Note{}, notes.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return notes.Note{}, err
	}
	defer tx.Rollback()
	note, err := scanNote(tx.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id=?`, id))
	if err != nil {
		return notes.Note{}, err
	}
	if note.UpdatedAt != version {
		return notes.Note{}, notes.ErrConflict
	}
	if strings.HasPrefix(note.ContentID, "memo:") || note.PositionSeconds == nil && len(note.SubtitleReferences) == 0 && frame.Source != notes.FrameSourcePlayer {
		return notes.Note{}, notes.ErrInvalid
	}
	if len(note.Frames) >= notes.MaxNoteFrames {
		return notes.Note{}, notes.ErrInvalid
	}
	for _, existing := range note.Frames {
		if existing.ID == frame.ID {
			return notes.Note{}, notes.ErrInvalid
		}
	}
	note.Frames = append(note.Frames, *frame)
	encoded, err := json.Marshal(note.Frames)
	if err != nil {
		return notes.Note{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO note_frames(note_id,frame_id,data) VALUES(?,?,?)`, id, frame.ID, data); err != nil {
		return notes.Note{}, err
	}
	note, err = scanNote(tx.QueryRowContext(ctx, `UPDATE notes SET frame=?,updated_at=MAX(?,updated_at+1) WHERE id=? RETURNING `+noteColumns, string(encoded), time.Now().Unix(), id))
	if err != nil {
		return notes.Note{}, err
	}
	if err = tx.Commit(); err != nil {
		return notes.Note{}, err
	}
	return note, nil
}

func (s *Store) DeleteNoteFrame(ctx context.Context, id string, version int64, frameID string) (notes.Note, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return notes.Note{}, err
	}
	defer tx.Rollback()
	note, err := scanNote(tx.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id=?`, id))
	if err != nil {
		return notes.Note{}, err
	}
	if note.UpdatedAt != version {
		return notes.Note{}, notes.ErrConflict
	}
	frames := make([]notes.Frame, 0, len(note.Frames))
	found := false
	for _, frame := range note.Frames {
		if frame.ID == frameID {
			found = true
			continue
		}
		frames = append(frames, frame)
	}
	if !found {
		return notes.Note{}, notes.ErrNotFound
	}
	encoded, err := json.Marshal(frames)
	if err != nil {
		return notes.Note{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM note_frames WHERE note_id=? AND frame_id=?`, id, frameID); err != nil {
		return notes.Note{}, err
	}
	note, err = scanNote(tx.QueryRowContext(ctx, `UPDATE notes SET frame=?,updated_at=MAX(?,updated_at+1) WHERE id=? RETURNING `+noteColumns, string(encoded), time.Now().Unix(), id))
	if err != nil {
		return notes.Note{}, err
	}
	if err = tx.Commit(); err != nil {
		return notes.Note{}, err
	}
	return note, nil
}

// Metadata, image bytes and the optimistic note version change atomically.
func (s *Store) SaveNoteFrame(ctx context.Context, id string, version int64, frame *notes.Frame, data []byte) (notes.Note, error) {
	if notes.ValidateFrame(frame) != nil || frame != nil && (len(data) == 0 || len(data) > notes.MaxFrameBytes) {
		return notes.Note{}, notes.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return notes.Note{}, err
	}
	defer tx.Rollback()
	note, err := scanNote(tx.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id=?`, id))
	if err != nil {
		return notes.Note{}, err
	}
	if note.UpdatedAt != version {
		return notes.Note{}, notes.ErrConflict
	}
	var metadata any
	if frame != nil {
		if strings.HasPrefix(note.ContentID, "memo:") || note.PositionSeconds == nil && len(note.SubtitleReferences) == 0 && frame.Source != notes.FrameSourcePlayer {
			return notes.Note{}, notes.ErrInvalid
		}
		encoded, err := json.Marshal(frame)
		if err != nil {
			return notes.Note{}, err
		}
		metadata = string(encoded)
		if _, err := tx.ExecContext(ctx, `DELETE FROM note_frames WHERE note_id=?`, id); err != nil {
			return notes.Note{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO note_frames(note_id,frame_id,data) VALUES(?,?,?)`, id, frame.ID, data); err != nil {
			return notes.Note{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM note_frames WHERE note_id=?`, id); err != nil {
			return notes.Note{}, err
		}
	}
	note, err = scanNote(tx.QueryRowContext(ctx, `UPDATE notes SET frame=?,updated_at=MAX(?,updated_at+1) WHERE id=? RETURNING `+noteColumns, metadata, time.Now().Unix(), id))
	if err != nil {
		return notes.Note{}, err
	}
	if err := tx.Commit(); err != nil {
		return notes.Note{}, err
	}
	return note, nil
}
