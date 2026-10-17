package httptransport

import (
	"bytes"
	"net/http"
	"strconv"
	"time"

	"tangentry/internal/notes"
)

type noteFrameCaptureInput struct {
	DownloadID        string   `json:"downloadId"`
	PositionSeconds   *float64 `json:"positionSeconds"`
	ExpectedUpdatedAt *int64   `json:"expectedUpdatedAt"`
}

// decodeNoteFrameInput keeps the frame capture contract explicit for both the
// first-frame and append endpoints: both require an exact playback position
// and optimistic-concurrency version before touching the local video file.
func decodeNoteFrameInput(w http.ResponseWriter, r *http.Request) (notes.FrameInput, bool) {
	var input noteFrameCaptureInput
	if !decodeNote(w, r, &input) {
		return notes.FrameInput{}, false
	}
	if input.PositionSeconds == nil || input.ExpectedUpdatedAt == nil {
		fail(w, notes.ErrInvalid)
		return notes.FrameInput{}, false
	}
	return notes.FrameInput{DownloadID: input.DownloadID, PositionSeconds: *input.PositionSeconds, ExpectedUpdatedAt: *input.ExpectedUpdatedAt}, true
}

func (h *notesHandler) captureNoteFrame(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	input, ok := decodeNoteFrameInput(w, r)
	if !ok {
		return
	}
	note, err := h.notes.NoteFrames.Capture(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteFrameCapture(w http.ResponseWriter, r *http.Request) {
	h.captureNoteFrame(w, r)
}

func (h *notesHandler) noteFrameAppend(w http.ResponseWriter, r *http.Request) {
	h.captureNoteFrame(w, r)
}

func (h *notesHandler) noteWithFrame(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	var input notes.FrameNoteInput
	if !decodeNote(w, r, &input) {
		return
	}
	note, err := h.notes.NoteFrames.Create(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusCreated, note)
}

// Allow up to ten 1 MiB JPEGs after base64 expansion, plus JSON and note text.
const noteImageRequestLimit = 16 << 20

func (h *notesHandler) noteWithImage(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	var input notes.ImageNoteInput
	if !decodeNoteLimit(w, r, &input, noteImageRequestLimit) {
		return
	}
	note, err := h.notes.NoteFrames.CreateWithImage(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusCreated, note)
}

func (h *notesHandler) noteFrameImageAttach(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	var input struct {
		notes.FrameImage
		ExpectedUpdatedAt *int64 `json:"expectedUpdatedAt"`
	}
	if !decodeNoteLimit(w, r, &input, noteImageRequestLimit) {
		return
	}
	if input.ExpectedUpdatedAt == nil {
		fail(w, notes.ErrInvalid)
		return
	}
	note, err := h.notes.NoteFrames.AttachImage(r.Context(), r.PathValue("id"), *input.ExpectedUpdatedAt, input.FrameImage)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteFrameImageAppend(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	var input struct {
		notes.FrameImage
		ExpectedUpdatedAt *int64 `json:"expectedUpdatedAt"`
	}
	if !decodeNoteLimit(w, r, &input, noteImageRequestLimit) {
		return
	}
	if input.ExpectedUpdatedAt == nil {
		fail(w, notes.ErrInvalid)
		return
	}
	note, err := h.notes.NoteFrames.AppendImage(r.Context(), r.PathValue("id"), *input.ExpectedUpdatedAt, input.FrameImage)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteFrameOneRemove(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("expectedUpdatedAt"), 10, 64)
	if err != nil {
		fail(w, notes.ErrInvalid)
		return
	}
	note, err := h.notes.NoteFrames.RemoveOne(r.Context(), r.PathValue("id"), version, r.PathValue("frameID"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteFrameRemove(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	var input struct {
		ExpectedUpdatedAt *int64 `json:"expectedUpdatedAt"`
	}
	if !decodeNote(w, r, &input) {
		return
	}
	if input.ExpectedUpdatedAt == nil {
		fail(w, notes.ErrInvalid)
		return
	}
	note, err := h.notes.NoteFrames.Remove(r.Context(), r.PathValue("id"), *input.ExpectedUpdatedAt)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteFrameImage(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteFrames == nil {
		fail(w, notes.ErrFrameUnavailable)
		return
	}
	data, err := h.notes.NoteFrames.Image(r.Context(), r.PathValue("id"), r.PathValue("frameID"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "frame.jpg", time.Time{}, bytes.NewReader(data))
}
