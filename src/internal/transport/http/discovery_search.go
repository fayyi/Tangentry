package httptransport

import (
	"net/http"
	"strconv"

	"tangentry/internal/discovery"
)

func (h *discoveryHandler) search(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
		page = parsed
	}
	feed, err := h.browse.Search.Search(r.Context(), r.URL.Query().Get("q"), page)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, feed)
}
