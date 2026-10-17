package httptransport

import "net/http"

func (h *Handler) registerDiscoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/discovery/bilibili", h.discovery.search)
	mux.HandleFunc("GET /api/discovery/{source}", h.discovery.importSearch)
	mux.HandleFunc("GET /api/recommendations/{source}", h.discovery.recommendationRead)
	mux.HandleFunc("GET /api/recommendations/{source}/pending", h.discovery.recommendationPending)
	mux.HandleFunc("PUT /api/recommendations/{source}/marks", h.discovery.recommendationMark)
	mux.HandleFunc("GET /api/settings/recommendations", h.discovery.recommendationSettingsRead)
	mux.HandleFunc("PUT /api/settings/recommendations/{source}", h.discovery.recommendationSettingsSave)
	mux.HandleFunc("GET /api/bilibili/browse/{kind}", h.discovery.bilibiliBrowse)
	mux.HandleFunc("GET /api/bilibili/profile", h.discovery.bilibiliProfile)
	mux.HandleFunc("GET /api/xiaohongshu/details/{id}", h.discovery.xiaohongshuDetailRead)
	mux.HandleFunc("POST /api/xiaohongshu/details/{id}/refresh", h.discovery.xiaohongshuDetailRefresh)
	mux.HandleFunc("GET /api/xiaohongshu/images/{id}/{version}", h.discovery.xiaohongshuImageRead)
}
