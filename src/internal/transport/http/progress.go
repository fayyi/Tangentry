package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"tangentry/internal/readingprogress"
)

func (h *notesHandler) progressBatch(w http.ResponseWriter, r *http.Request) {
	if h.notes.Progress == nil {
		write(w, 503, apiError{"progress_unavailable", "阅读进度暂时不可用。"})
		return
	}
	values, err := h.notes.Progress.Batch(r.Context(), r.URL.Query()["contentId"])
	if err != nil {
		if err == readingprogress.ErrInvalid {
			write(w, 400, apiError{"invalid_progress", "请提交 1–100 个有效视频内容 ID。"})
		} else {
			fail(w, err)
		}
		return
	}
	write(w, 200, map[string]any{"items": values})
}

func (h *notesHandler) progressGet(w http.ResponseWriter, r *http.Request) {
	if h.notes.Progress == nil {
		write(w, 503, apiError{"progress_unavailable", "阅读进度暂时不可用。"})
		return
	}
	value, err := h.notes.Progress.Get(r.Context(), r.URL.Query().Get("contentId"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"progress": value})
}
func (h *notesHandler) progressSave(w http.ResponseWriter, r *http.Request) {
	if h.notes.Progress == nil {
		write(w, 503, apiError{"progress_unavailable", "阅读进度暂时不可用。"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 格式提交阅读进度。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var input struct {
		PositionSeconds *float64 `json:"positionSeconds"`
		DurationSeconds *float64 `json:"durationSeconds"`
		Revision        *int64   `json:"revision"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.PositionSeconds == nil || input.DurationSeconds == nil || input.Revision == nil {
		fail(w, readingprogress.ErrInvalid)
		return
	}
	value, err := h.notes.Progress.Save(r.Context(), r.PathValue("id"), readingprogress.Input{PositionSeconds: *input.PositionSeconds, DurationSeconds: *input.DurationSeconds, Revision: *input.Revision})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, value)
}
