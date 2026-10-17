package httptransport

import "net/http"

// registerAccountRoutes contains credential and interactive login endpoints.
func (h *Handler) registerAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/cookies", h.accounts.cookieList)
	mux.HandleFunc("PUT /api/settings/cookies/{platform}", h.accounts.cookieSave)
	mux.HandleFunc("DELETE /api/settings/cookies/{platform}", h.accounts.cookieClear)
	mux.HandleFunc("POST /api/settings/bilibili/login", h.accounts.bilibiliLoginStart)
	mux.HandleFunc("POST /api/settings/bilibili/login/{id}", h.accounts.bilibiliLoginPoll)
	mux.HandleFunc("POST /api/settings/xiaoyuzhou/login", h.accounts.xiaoyuzhouLoginStart)
	mux.HandleFunc("POST /api/settings/xiaoyuzhou/login/{id}", h.accounts.xiaoyuzhouLoginPoll)
	mux.HandleFunc("POST /api/settings/linuxdo/session", h.accounts.linuxdoSessionCheck)
	mux.HandleFunc("POST /api/settings/linuxdo/login", h.accounts.linuxdoLoginStart)
	mux.HandleFunc("POST /api/settings/linuxdo/login/{id}", h.accounts.linuxdoLoginPoll)
	mux.HandleFunc("POST /api/settings/linuxdo/login/{id}/complete", h.accounts.linuxdoLoginComplete)
	mux.HandleFunc("DELETE /api/settings/linuxdo/login/{id}", h.accounts.linuxdoLoginCancel)
}
