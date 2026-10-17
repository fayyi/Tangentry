package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"tangentry/internal/notes"
)

func (h *notesHandler) noteExport(w http.ResponseWriter, r *http.Request) {
	if h.notes.NoteExports == nil {
		write(w, 503, apiError{"note_export_unavailable", "笔记导出暂时不可用。"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		write(w, 415, apiError{"invalid_note_export", "请使用 JSON 提交笔记导出条件。"})
		return
	}
	var input notes.ExportInput
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if len(r.URL.Query()) != 0 || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		write(w, 400, apiError{"invalid_note_export", "请检查关键词、标签、日期与笔记来源。"})
		return
	}
	result, err := h.notes.NoteExports.Export(r.Context(), input)
	if err != nil {
		if errors.Is(err, notes.ErrInvalid) {
			write(w, 400, apiError{"invalid_note_export", "请检查关键词、标签、日期与笔记来源。"})
			return
		}
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
	w.Header().Set("X-Tangentry-Notes-Count", strconv.Itoa(result.Count))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}
