package httptransport

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"tangentry/internal/content"
)

// list returns library cards: kind, platform and q filter, limit and offset
// page. Without limit every matching card is returned.
func (h *libraryHandler) list(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	query := content.Query{Kind: values.Get("kind"), Platform: values.Get("platform"), Q: values.Get("q")}
	valid := true
	for name, target := range map[string]*int{"limit": &query.Limit, "offset": &query.Offset} {
		if raw := values.Get(name); raw != "" {
			number, err := strconv.Atoi(raw)
			valid = valid && err == nil
			*target = number
		}
	}
	if !valid || !content.ValidQuery(query) {
		write(w, http.StatusBadRequest, apiError{"invalid_library_query", "资料库筛选条件无效，每页最多 200 条。"})
		return
	}
	page, err := h.library.Content.Page(r.Context(), query)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, page)
}

func (h *libraryHandler) libraryItem(w http.ResponseWriter, r *http.Request) {
	item, err := h.library.Content.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, item)
}

func (h *libraryHandler) save(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 格式提交内容。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var item content.Item
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil {
		fail(w, content.ErrInvalid)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, content.ErrInvalid)
		return
	}
	saved, err := h.library.Content.Save(r.Context(), item)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, saved)
}

func (h *libraryHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.library.Content.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
