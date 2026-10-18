package httptransport

import (
	"net/http"
	"strconv"

	"tangentry/internal/discovery"
)

func (h *discoveryHandler) unifiedSearch(w http.ResponseWriter, r *http.Request) {
	if h.browse.UnifiedSearch == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	values := r.URL.Query()
	from, _ := strconv.ParseInt(values.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(values.Get("to"), 10, 64)
	result, err := h.browse.UnifiedSearch.Search(r.Context(), values.Get("q"), discovery.SearchOptions{Platform: values.Get("platform"), Kind: values.Get("kind"), From: from, To: to, Cursor: values.Get("cursor")})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *discoveryHandler) creator(w http.ResponseWriter, r *http.Request) {
	platform, id := r.PathValue("platform"), r.PathValue("id")
	if h.browse.UnifiedSearch == nil {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	page := r.URL.Query().Get("cursor")
	feed, err := h.browse.UnifiedSearch.Creator(r.Context(), platform, id, page)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, feed)
}
