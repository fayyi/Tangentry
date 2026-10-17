package notes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/jpeg"
	"math"
	"time"
)

func (s *Frames) Capture(ctx context.Context, id string, input FrameInput) (Note, error) {
	if !noteIDPattern.MatchString(id) || !frameDownloadID.MatchString(input.DownloadID) || input.ExpectedUpdatedAt < 0 ||
		math.IsNaN(input.PositionSeconds) || math.IsInf(input.PositionSeconds, 0) || input.PositionSeconds < 0 || input.PositionSeconds > 604800 {
		return Note{}, ErrInvalid
	}
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		return Note{}, ErrFrameBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	note, err := s.repository.GetNote(ctx, id)
	if err != nil {
		return Note{}, err
	}
	if note.UpdatedAt != input.ExpectedUpdatedAt {
		return Note{}, ErrConflict
	}
	if note.PositionSeconds == nil && len(note.SubtitleReferences) == 0 && note.TextAnchor == nil && note.PDFAnchor == nil {
		return Note{}, ErrInvalid
	}
	frame, data, err := s.extract(ctx, note.ContentID, input.DownloadID, input.PositionSeconds)
	if err != nil {
		return Note{}, err
	}
	return s.repository.AppendNoteFrame(ctx, id, input.ExpectedUpdatedAt, frame, data)
}

func (s *Frames) extract(ctx context.Context, contentID, downloadID string, position float64) (*Frame, []byte, error) {
	file, err := s.file(ctx, contentID, downloadID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, ErrFrameSource
	}
	defer file.Close()
	data, err := s.capture(ctx, file.Name(), position)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return nil, nil, ErrFrameUnavailable
	}
	size, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, nil, ErrFrameUnavailable
	}
	frame, err := newFrame(size.Width, size.Height, Frame{DownloadID: downloadID, PositionSeconds: position, Approximate: true})
	if err != nil {
		return nil, nil, ErrFrameUnavailable
	}
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		return nil, nil, ErrFrameUnavailable
	}
	return frame, data, nil
}

func (s *Frames) Create(ctx context.Context, input FrameNoteInput) (Note, error) {
	note, err := prepareNote(input.Note)
	if err != nil {
		return Note{}, err
	}
	if note.CreationHash == "" || note.PositionSeconds == nil && len(note.SubtitleReferences) == 0 && note.TextAnchor == nil && note.PDFAnchor == nil ||
		!frameDownloadID.MatchString(input.Capture.DownloadID) || input.Capture.PositionSeconds == nil {
		return Note{}, ErrInvalid
	}
	position := *input.Capture.PositionSeconds
	if !validFramePosition(position) {
		return Note{}, ErrInvalid
	}
	data, err := json.Marshal(input.Capture)
	if err != nil {
		return Note{}, err
	}
	sum := sha256.Sum256(append([]byte(note.CreationHash), data...))
	note.CreationHash = hex.EncodeToString(sum[:])
	if previous, exists, err := s.repository.FindNoteCreation(ctx, note.ID, note.CreationHash); err != nil || exists {
		return previous, err
	}
	select {
	case s.active <- struct{}{}:
		defer func() { <-s.active }()
	default:
		return Note{}, ErrFrameBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	frame, image, err := s.extract(ctx, note.ContentID, input.Capture.DownloadID, position)
	if err != nil {
		return Note{}, err
	}
	note.Frames = []Frame{*frame}
	note.Frame = frame
	return s.repository.CreateNoteWithFrame(ctx, note, image)
}

func newFrame(width, height int, frame Frame) (*Frame, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return nil, err
	}
	frame.ID = hex.EncodeToString(identity[:])
	frame.Width = width
	frame.Height = height
	frame.CreatedAt = time.Now().Unix()
	if ValidateFrame(&frame) != nil {
		return nil, ErrInvalid
	}
	return &frame, nil
}
