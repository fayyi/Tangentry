package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"tangentry/internal/readstate"
)

func (h *notesHandler) readStateReady(w http.ResponseWriter) bool {
	if h.notes.ReadState != nil {
		return true
	}
	write(w, 503, apiError{"read_state_unavailable", "暂时无法读取已读状态。"})
	return false
}

func (h *notesHandler) readStateBatch(w http.ResponseWriter, r *http.Request) {
	if !h.readStateReady(w) {
		return
	}
	states, err := h.notes.ReadState.Batch(r.Context(), r.URL.Query()["contentId"])
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"states": states})
}

func (h *notesHandler) readStateSet(w http.ResponseWriter, r *http.Request) {
	if !h.readStateReady(w) {
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 提交已读状态。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var input struct {
		IDs  []string `json:"contentIds"`
		Read *bool    `json:"read"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.Read == nil {
		fail(w, readstate.ErrInvalid)
		return
	}
	states, err := h.notes.ReadState.Set(r.Context(), input.IDs, *input.Read)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"states": states})
}
