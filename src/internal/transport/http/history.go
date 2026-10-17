package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"tangentry/internal/history"
)

func (h *notesHandler) historyReady(w http.ResponseWriter) bool {
	if h.notes.History != nil {
		return true
	}
	write(w, 503, apiError{"history_unavailable", "暂时无法读取历史记录。"})
	return false
}

func historyBody(w http.ResponseWriter, r *http.Request, value any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 提交历史记录。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, history.ErrInvalid)
		return false
	}
	return true
}

func (h *notesHandler) viewingList(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	entries, err := h.notes.History.Viewing(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"entries": entries})
}

// viewingRecord stores one visit or a browser history upload; invalid
// entries are skipped and the response counts what was kept.
func (h *notesHandler) viewingRecord(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	var input struct {
		Entries []history.Entry `json:"entries"`
	}
	if !historyBody(w, r, &input) {
		return
	}
	kept, err := h.notes.History.Record(r.Context(), input.Entries)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"kept": kept})
}

func (h *notesHandler) viewingRemove(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	if err := h.notes.History.RemoveViewing(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *notesHandler) viewingClear(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	if err := h.notes.History.ClearViewing(r.Context()); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *notesHandler) searchList(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	records, err := h.notes.History.Searches(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"records": records})
}

func (h *notesHandler) searchAdd(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	var input struct {
		Records []history.Search `json:"records"`
	}
	if !historyBody(w, r, &input) {
		return
	}
	kept, err := h.notes.History.AddSearches(r.Context(), input.Records)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"kept": kept})
}

func (h *notesHandler) searchRemove(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	if err := h.notes.History.RemoveSearch(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *notesHandler) searchClear(w http.ResponseWriter, r *http.Request) {
	if !h.historyReady(w) {
		return
	}
	if err := h.notes.History.ClearSearches(r.Context()); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
