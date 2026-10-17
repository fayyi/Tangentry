// Package notes manages user-authored reading notes without saving or downloading their source.
package notes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"tangentry/internal/content"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid note")
var ErrNotFound = errors.New("note not found")
var ErrConflict = errors.New("note changed since editing started")
var ErrCreationConflict = errors.New("note creation changed")
var ErrCreationDeleted = errors.New("created note was deleted")

type Note struct {
	CreationHash       string              `json:"-"`
	ID                 string              `json:"id"`
	ContentID          string              `json:"contentId"`
	Body               string              `json:"body"`
	PositionSeconds    *float64            `json:"positionSeconds"`
	TextAnchor         *TextAnchor         `json:"textAnchor"`
	PDFAnchor          *PDFAnchor          `json:"pdfAnchor"`
	SubtitleReferences []SubtitleReference `json:"subtitleReferences,omitempty"`
	// Frames contains every captured still in creation order. Frame is retained
	// as a compatibility alias for clients that only understand one screenshot.
	Frames    []Frame       `json:"frames"`
	Frame     *Frame        `json:"frame,omitempty"`
	Tags      []string      `json:"tags"`
	CreatedAt int64         `json:"createdAt"`
	UpdatedAt int64         `json:"updatedAt"`
	Pinned    bool          `json:"pinned"`
	Source    *ExportSource `json:"source,omitempty"`
}

type Input struct {
	CreateID           string              `json:"createId,omitempty"`
	ContentID          string              `json:"contentId"`
	Body               string              `json:"body"`
	PositionSeconds    *float64            `json:"positionSeconds"`
	TextAnchor         *TextAnchor         `json:"textAnchor"`
	PDFAnchor          *PDFAnchor          `json:"pdfAnchor"`
	SubtitleReferences []SubtitleReference `json:"subtitleReferences"`
	Tags               []string            `json:"tags"`
}

type Edit struct {
	ExpectedUpdatedAt *int64   `json:"expectedUpdatedAt"`
	Body              string   `json:"body"`
	PositionSeconds   *float64 `json:"positionSeconds"`
	// Omission preserves the existing anchor; explicit null detaches it.
	TextAnchor         json.RawMessage `json:"textAnchor"`
	PDFAnchor          json.RawMessage `json:"pdfAnchor"`
	SubtitleReferences json.RawMessage `json:"subtitleReferences"`
	// Omission preserves tags; an explicit empty array removes them.
	Tags *[]string `json:"tags"`
}

type Repository interface {
	ListNotes(context.Context, string) ([]Note, error)
	SearchNotes(context.Context, SearchQuery) ([]Note, error)
	NoteTags(context.Context) ([]TagCount, error)
	CreateNote(context.Context, Note) (Note, error)
	EditNote(context.Context, string, Edit, int64) (Note, error)
	DeleteNote(context.Context, string, ...int64) error
	SetNotePinned(context.Context, string, bool) (Note, error)
}

type Service struct{ repository Repository }

func (s *Service) SetPinned(ctx context.Context, id string, pinned bool) (Note, error) {
	if !noteIDPattern.MatchString(id) {
		return Note{}, ErrInvalid
	}
	return s.repository.SetNotePinned(ctx, id, pinned)
}

func NewService(repository Repository) *Service { return &Service{repository: repository} }

// IDs belong to content, not saved_content rows. New media types can extend this boundary later.
var memoIDPattern = regexp.MustCompile(`^memo:[a-f0-9]{32}$`)

func validContentID(id string) bool { return content.ValidID(id) || memoIDPattern.MatchString(id) }

var noteIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Service) List(ctx context.Context, contentID string) ([]Note, error) {
	if !validContentID(contentID) {
		return nil, ErrInvalid
	}
	return s.repository.ListNotes(ctx, contentID)
}

func validate(body string, position *float64) error {
	if body == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > 8000 {
		return ErrInvalid
	}
	if position != nil && (math.IsNaN(*position) || math.IsInf(*position, 0) || *position < 0 || *position > 7*24*3600) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) Create(ctx context.Context, input Input) (Note, error) {
	note, err := prepareNote(input)
	if err != nil {
		return Note{}, err
	}
	return s.repository.CreateNote(ctx, note)
}

func prepareNote(input Input) (Note, error) {
	if input.CreateID != "" && !noteIDPattern.MatchString(input.CreateID) {
		return Note{}, ErrInvalid
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.ContentID != "" && !validContentID(input.ContentID) {
		return Note{}, ErrInvalid
	}
	if err := validate(input.Body, input.PositionSeconds); err != nil {
		return Note{}, err
	}
	if err := validateAnchor(input.TextAnchor); err != nil {
		return Note{}, err
	}
	if err := validatePDFAnchor(input.PDFAnchor); err != nil {
		return Note{}, err
	}
	if input.TextAnchor != nil && input.PDFAnchor != nil {
		return Note{}, ErrInvalid
	}
	if err := ValidateSubtitleReferences(input.SubtitleReferences); err != nil {
		return Note{}, err
	}
	if len(input.SubtitleReferences) > 0 && (input.TextAnchor != nil || input.PDFAnchor != nil) {
		return Note{}, ErrInvalid
	}
	if (input.ContentID == "" || strings.HasPrefix(input.ContentID, "memo:")) && (input.PositionSeconds != nil || input.TextAnchor != nil || input.PDFAnchor != nil || len(input.SubtitleReferences) > 0) {
		return Note{}, ErrInvalid
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return Note{}, err
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Note{}, err
	}
	now := time.Now().Unix()
	id := hex.EncodeToString(bytes[:])
	hash := ""
	if input.CreateID != "" {
		id = input.CreateID
		input.Tags = tags
		payload, err := json.Marshal(input)
		if err != nil {
			return Note{}, err
		}
		sum := sha256.Sum256(payload)
		hash = hex.EncodeToString(sum[:])
	}
	if input.ContentID == "" {
		input.ContentID = "memo:" + id
	}
	return Note{CreationHash: hash, ID: id, ContentID: input.ContentID, Body: input.Body, PositionSeconds: input.PositionSeconds, TextAnchor: input.TextAnchor, PDFAnchor: input.PDFAnchor, SubtitleReferences: input.SubtitleReferences, Tags: tags, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Service) Edit(ctx context.Context, id string, input Edit) (Note, error) {
	if input.ExpectedUpdatedAt != nil && *input.ExpectedUpdatedAt < 0 {
		return Note{}, ErrInvalid
	}
	input.Body = strings.TrimSpace(input.Body)
	if !noteIDPattern.MatchString(id) {
		return Note{}, ErrInvalid
	}
	if err := validate(input.Body, input.PositionSeconds); err != nil {
		return Note{}, err
	}
	if input.Tags != nil {
		tags, err := normalizeTags(*input.Tags)
		if err != nil {
			return Note{}, err
		}
		input.Tags = &tags
	}
	if len(input.TextAnchor) > 0 {
		anchor, err := decodeAnchor(input.TextAnchor)
		if err != nil {
			return Note{}, err
		}
		input.TextAnchor, err = json.Marshal(anchor)
		if err != nil {
			return Note{}, err
		}
	}
	if len(input.PDFAnchor) > 0 {
		anchor, err := decodePDFAnchor(input.PDFAnchor)
		if err != nil {
			return Note{}, err
		}
		input.PDFAnchor, err = json.Marshal(anchor)
		if err != nil {
			return Note{}, err
		}
	}
	if len(input.SubtitleReferences) > 0 {
		references, err := decodeSubtitleReferences(input.SubtitleReferences)
		if err != nil {
			return Note{}, err
		}
		input.SubtitleReferences, err = json.Marshal(references)
		if err != nil {
			return Note{}, err
		}
	}
	return s.repository.EditNote(ctx, id, input, time.Now().Unix())
}

func (s *Service) Delete(ctx context.Context, id string, version ...int64) error {
	if !noteIDPattern.MatchString(id) {
		return ErrInvalid
	}
	if len(version) > 1 || len(version) == 1 && version[0] < 0 {
		return ErrInvalid
	}
	return s.repository.DeleteNote(ctx, id, version...)
}
