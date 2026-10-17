package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tangentry/internal/notes"
)

func (s *Store) CreateNote(ctx context.Context, note notes.Note) (notes.Note, error) {
	return s.createNote(ctx, note, nil)
}

func (s *Store) CreateNoteWithFrame(ctx context.Context, note notes.Note, data []byte) (notes.Note, error) {
	frame := note.Frame
	if len(note.Frames) > 0 {
		frame = &note.Frames[0]
	}
	if frame == nil || notes.ValidateFrame(frame) != nil || len(data) == 0 || len(data) > notes.MaxFrameBytes {
		return notes.Note{}, notes.ErrInvalid
	}
	note.Frame = frame
	note.Frames = []notes.Frame{*frame}
	return s.CreateNoteWithFrames(ctx, note, []*notes.Frame{frame}, [][]byte{data})
}

func (s *Store) CreateNoteWithFrames(ctx context.Context, note notes.Note, frames []*notes.Frame, data [][]byte) (notes.Note, error) {
	if len(frames) == 0 || len(frames) != len(data) || len(frames) > notes.MaxNoteFrames {
		return notes.Note{}, notes.ErrInvalid
	}
	note.Frames = make([]notes.Frame, len(frames))
	for i, frame := range frames {
		if frame == nil || notes.ValidateFrame(frame) != nil || len(data[i]) == 0 || len(data[i]) > notes.MaxFrameBytes {
			return notes.Note{}, notes.ErrInvalid
		}
		note.Frames[i] = *frame
	}
	note.Frame = frames[0]
	return s.createNote(ctx, note, data)
}

func (s *Store) createNote(ctx context.Context, note notes.Note, frameDataList [][]byte) (notes.Note, error) {
	frameValue := note.Frame
	if len(note.Frames) > 0 {
		frameValue = &note.Frames[0]
	}
	if frameValue != nil && len(frameDataList) != len(note.Frames) {
		return notes.Note{}, notes.ErrInvalid
	}
	anchor, err := marshalNullable(note.TextAnchor)
	if err != nil {
		return notes.Note{}, err
	}
	pdfAnchor, err := marshalNullable(note.PDFAnchor)
	if err != nil {
		return notes.Note{}, err
	}
	tags, err := json.Marshal(note.Tags)
	if err != nil {
		return notes.Note{}, err
	}
	references, err := json.Marshal(note.SubtitleReferences)
	if err != nil {
		return notes.Note{}, err
	}
	var frame any
	if frameValue != nil {
		data, err := json.Marshal(note.Frames)
		if err != nil {
			return notes.Note{}, err
		}
		frame = string(data)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return notes.Note{}, err
	}
	defer tx.Rollback()
	if note.CreationHash != "" {
		if previous, exists, err := findNoteCreation(ctx, tx, note.ID, note.CreationHash); err != nil || exists {
			return previous, err
		}
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM notes WHERE id=?`, note.ID).Scan(&existing)
		if err == nil {
			return notes.Note{}, notes.ErrCreationConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return notes.Note{}, err
		}
	}
	saved, err := scanNote(tx.QueryRowContext(ctx, `INSERT INTO notes (`+noteColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) RETURNING `+noteColumns, note.ID, note.ContentID, note.Body, note.PositionSeconds, note.CreatedAt, note.UpdatedAt, anchor, pdfAnchor, string(tags), note.Pinned, string(references), frame))
	if err != nil {
		return notes.Note{}, err
	}
	if frameValue != nil {
		for index, frame := range note.Frames {
			if _, err := tx.ExecContext(ctx, `INSERT INTO note_frames(note_id,frame_id,data) VALUES(?,?,?)`, note.ID, frame.ID, frameDataList[index]); err != nil {
				return notes.Note{}, err
			}
		}
	}
	if note.CreationHash != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO note_creations(id,fingerprint,note_id) VALUES(?,?,?)`, note.ID, note.CreationHash, note.ID); err != nil {
			return notes.Note{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return notes.Note{}, err
	}
	return saved, nil
}

func marshalNullable(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}
