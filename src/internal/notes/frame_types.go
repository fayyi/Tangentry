package notes

import (
	"context"
	"errors"
	"os"
)

const MaxFrameBytes = 1 << 20
const MaxNoteFrames = 10

// FrameSourcePlayer marks a still captured by the reader from the frame being
// displayed. It has no download relationship and its time is the player clock.
const FrameSourcePlayer = "player"

var ErrFrameBusy = errors.New("note frame busy")
var ErrFrameUnavailable = errors.New("note frame unavailable")
var ErrFrameSource = errors.New("note frame source unavailable")
var ErrFrameImage = errors.New("note frame image invalid")

// PositionSeconds is the requested seek time, not a claim of exact frame PTS.
type Frame struct {
	ID              string  `json:"id"`
	DownloadID      string  `json:"downloadId"`
	PositionSeconds float64 `json:"positionSeconds"`
	Approximate     bool    `json:"approximate"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	CreatedAt       int64   `json:"createdAt"`
	Source          string  `json:"source,omitempty"`
}

type FrameInput struct {
	DownloadID        string  `json:"downloadId"`
	PositionSeconds   float64 `json:"positionSeconds"`
	ExpectedUpdatedAt int64   `json:"expectedUpdatedAt"`
}

type FrameSpec struct {
	DownloadID      string   `json:"downloadId"`
	PositionSeconds *float64 `json:"positionSeconds"`
}

type FrameNoteInput struct {
	Note    Input     `json:"note"`
	Capture FrameSpec `json:"capture"`
}

// FrameImage is a JPEG captured in the reader; JSON carries the bytes as base64.
type FrameImage struct {
	Data            []byte   `json:"data"`
	PositionSeconds *float64 `json:"positionSeconds"`
}

type ImageNoteInput struct {
	Note   Input        `json:"note"`
	Image  FrameImage   `json:"image,omitempty"`
	Images []FrameImage `json:"images,omitempty"`
}

type FrameRepository interface {
	GetNote(context.Context, string) (Note, error)
	SaveNoteFrame(context.Context, string, int64, *Frame, []byte) (Note, error)
	AppendNoteFrame(context.Context, string, int64, *Frame, []byte) (Note, error)
	DeleteNoteFrame(context.Context, string, int64, string) (Note, error)
	NoteFrameData(context.Context, string, string) ([]byte, error)
	FindNoteCreation(context.Context, string, string) (Note, bool, error)
	CreateNoteWithFrame(context.Context, Note, []byte) (Note, error)
	CreateNoteWithFrames(context.Context, Note, []*Frame, [][]byte) (Note, error)
}

// FrameFile validates the content/download relationship and returns only a
// completed local file. Capturing never resolves a remote URL or downloads it.
type FrameFile func(context.Context, string, string) (*os.File, error)
type FrameCapture func(context.Context, string, float64) ([]byte, error)

type Frames struct {
	repository FrameRepository
	file       FrameFile
	capture    FrameCapture
	active     chan struct{}
}

func NewFrames(repository FrameRepository, file FrameFile, capture FrameCapture) *Frames {
	return &Frames{repository: repository, file: file, capture: capture, active: make(chan struct{}, 1)}
}
