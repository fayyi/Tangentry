package httptransport

import "net/http"

// registerToolRoutes owns local media-tool status and maintenance endpoints.
func (h *Handler) registerToolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tools", h.media.tools)
	mux.HandleFunc("POST /api/tools/yt-dlp/update", h.media.toolUpdate)
}
