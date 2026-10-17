package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"tangentry/internal/notes"
)

func (h *notesHandler) noteSearch(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	query := r.URL.Query()
	stamp := func(name string) (int64, error) {
		if query.Get(name) == "" {
			return 0, nil
		}
		return strconv.ParseInt(query.Get(name), 10, 64)
	}
	from, fromErr := stamp("from")
	to, toErr := stamp("to")
	if fromErr != nil || toErr != nil {
		write(w, 400, apiError{"invalid_note_search", "请检查笔记日期筛选。"})
		return
	}
	if query.Has("pinnedOnly") && (len(query["pinnedOnly"]) != 1 || query.Get("pinnedOnly") != "true" && query.Get("pinnedOnly") != "false") {
		fail(w, notes.ErrInvalid)
		return
	}
	page, err := h.notes.Notes.SearchFiltered(r.Context(), query.Get("q"), query.Get("cursor"), notes.Filters{Tag: query.Get("tag"), From: from, To: to, PinnedOnly: query.Get("pinnedOnly") == "true"})
	if err != nil {
		if err == notes.ErrInvalid {
			write(w, 400, apiError{"invalid_note_search", "关键词最多 200 字，请检查标签、日期与分页地址。"})
		} else {
			fail(w, err)
		}
		return
	}
	write(w, http.StatusOK, page)
}

func (h *notesHandler) noteTags(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	items, err := h.notes.Notes.Tags(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"tags": items})
}

func (h *notesHandler) noteList(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	items, err := h.notes.Notes.List(r.Context(), r.URL.Query().Get("contentId"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"notes": items})
}

func decodeNote(w http.ResponseWriter, r *http.Request, value any) bool {
	return decodeNoteLimit(w, r, value, 128<<10)
}

func decodeNoteLimit(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 格式提交笔记。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, notes.ErrInvalid)
		return false
	}
	return true
}

func (h *notesHandler) noteCreate(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	var input notes.Input
	if !decodeNote(w, r, &input) {
		return
	}
	note, err := h.notes.Notes.Create(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusCreated, note)
}

func (h *notesHandler) noteEdit(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	var input notes.Edit
	if !decodeNote(w, r, &input) {
		return
	}
	note, err := h.notes.Notes.Edit(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}

func (h *notesHandler) noteDelete(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	var version []int64
	if r.URL.Query().Has("expectedUpdatedAt") {
		values := r.URL.Query()["expectedUpdatedAt"]
		if len(values) != 1 {
			fail(w, notes.ErrInvalid)
			return
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || value < 0 {
			fail(w, notes.ErrInvalid)
			return
		}
		version = append(version, value)
	}
	if err := h.notes.Notes.Delete(r.Context(), r.PathValue("id"), version...); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *notesHandler) notePin(w http.ResponseWriter, r *http.Request) {
	if h.notes.Notes == nil {
		write(w, 503, apiError{"notes_unavailable", "笔记暂时不可用。"})
		return
	}
	var input struct {
		Pinned *bool `json:"pinned"`
	}
	if !decodeNote(w, r, &input) {
		return
	}
	if input.Pinned == nil {
		fail(w, notes.ErrInvalid)
		return
	}
	note, err := h.notes.Notes.SetPinned(r.Context(), r.PathValue("id"), *input.Pinned)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, note)
}
