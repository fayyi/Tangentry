package httptransport

import (
	"encoding/json"
	"net/http"
	"tangentry/internal/discovery"
)

func (h *discoveryHandler) paperImport(w http.ResponseWriter, r *http.Request) {
	if h.browse.PaperImport == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	if r.Header.Get("Content-Type") != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 提交论文地址。"})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Query == "" {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	feed, err := h.browse.PaperImport(r.Context(), "paper", input.Query)
	if err != nil {
		fail(w, err)
		return
	}
	if len(feed.Items) != 1 {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	write(w, http.StatusOK, feed.Items[0])
}
