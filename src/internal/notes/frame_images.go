package notes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/jpeg"
	"strconv"
	"strings"
)

// playerFrame validates a reader capture. The bytes must be a complete JPEG;
// nothing is fetched or transcoded on the server.
func playerFrame(image FrameImage) (*Frame, []byte, error) {
	if image.PositionSeconds == nil || !validFramePosition(*image.PositionSeconds) || len(image.Data) == 0 {
		return nil, nil, ErrInvalid
	}
	if len(image.Data) > MaxFrameBytes {
		return nil, nil, ErrFrameImage
	}
	size, err := jpeg.DecodeConfig(bytes.NewReader(image.Data))
	if err != nil {
		return nil, nil, ErrFrameImage
	}
	if _, err := jpeg.Decode(bytes.NewReader(image.Data)); err != nil {
		return nil, nil, ErrFrameImage
	}
	frame, err := newFrame(size.Width, size.Height, Frame{PositionSeconds: *image.PositionSeconds, Source: FrameSourcePlayer})
	if err != nil || ValidateFrame(frame) != nil {
		return nil, nil, ErrFrameImage
	}
	return frame, image.Data, nil
}

func (s *Frames) Remove(ctx context.Context, id string, version int64) (Note, error) {
	if !noteIDPattern.MatchString(id) || version < 0 {
		return Note{}, ErrInvalid
	}
	return s.repository.SaveNoteFrame(ctx, id, version, nil, nil)
}

func (s *Frames) Image(ctx context.Context, id, frameID string) ([]byte, error) {
	if !noteIDPattern.MatchString(id) || !noteIDPattern.MatchString(frameID) {
		return nil, ErrInvalid
	}
	return s.repository.NoteFrameData(ctx, id, frameID)
}

// CreateWithImage saves a note together with a still captured in the reader.
// Retries with the same createId and image return the first saved note.
func (s *Frames) CreateWithImage(ctx context.Context, input ImageNoteInput) (Note, error) {
	if len(input.Images) == 0 && len(input.Image.Data) != 0 {
		input.Images = []FrameImage{input.Image}
	}
	return s.CreateWithImages(ctx, input)
}

// CreateWithImages creates the note and all reader captures in one transaction.
func (s *Frames) CreateWithImages(ctx context.Context, input ImageNoteInput) (Note, error) {
	note, err := prepareNote(input.Note)
	if err != nil {
		return Note{}, err
	}
	if note.CreationHash == "" || strings.HasPrefix(note.ContentID, "memo:") {
		return Note{}, ErrInvalid
	}
	if len(input.Images) == 0 || len(input.Images) > MaxNoteFrames {
		return Note{}, ErrInvalid
	}
	frames := make([]*Frame, 0, len(input.Images))
	datas := make([][]byte, 0, len(input.Images))
	fingerprint := []byte(note.CreationHash)
	for _, image := range input.Images {
		frame, data, err := playerFrame(image)
		if err != nil {
			return Note{}, err
		}
		frames = append(frames, frame)
		datas = append(datas, data)
		fingerprint = append(fingerprint, data...)
		fingerprint = append(fingerprint, []byte(strconv.FormatFloat(frame.PositionSeconds, 'f', -1, 64))...)
	}
	sum := sha256.Sum256(fingerprint)
	note.CreationHash = hex.EncodeToString(sum[:])
	if previous, exists, err := s.repository.FindNoteCreation(ctx, note.ID, note.CreationHash); err != nil || exists {
		return previous, err
	}
	note.Frames = make([]Frame, len(frames))
	for i, frame := range frames {
		note.Frames[i] = *frame
	}
	note.Frame = frames[0]
	return s.repository.CreateNoteWithFrames(ctx, note, frames, datas)
}

// AttachImage replaces an existing note's still with a reader capture.
func (s *Frames) AttachImage(ctx context.Context, id string, version int64, image FrameImage) (Note, error) {
	if !noteIDPattern.MatchString(id) || version < 0 {
		return Note{}, ErrInvalid
	}
	frame, data, err := playerFrame(image)
	if err != nil {
		return Note{}, err
	}
	note, err := s.repository.GetNote(ctx, id)
	if err != nil {
		return Note{}, err
	}
	if strings.HasPrefix(note.ContentID, "memo:") {
		return Note{}, ErrInvalid
	}
	return s.repository.AppendNoteFrame(ctx, id, version, frame, data)
}

// AppendImage adds a still to an existing note without replacing earlier captures.
func (s *Frames) AppendImage(ctx context.Context, id string, version int64, image FrameImage) (Note, error) {
	return s.AttachImage(ctx, id, version, image)
}

// RemoveOne deletes one screenshot while preserving the rest.
func (s *Frames) RemoveOne(ctx context.Context, id string, version int64, frameID string) (Note, error) {
	if !noteIDPattern.MatchString(id) || !noteIDPattern.MatchString(frameID) || version < 0 {
		return Note{}, ErrInvalid
	}
	return s.repository.DeleteNoteFrame(ctx, id, version, frameID)
}
