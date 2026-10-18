package httptransport

import "net/http"

// registerCoreRoutes contains protocol-wide health checks only.
func (h *Handler) registerCoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/preferences/{key}", h.preferenceGet)
	mux.HandleFunc("PUT /api/preferences/{key}", h.preferencePut)
	mux.HandleFunc("GET /api/settings/theme", h.themeGet)
	mux.HandleFunc("PUT /api/settings/theme", h.themePut)
}
