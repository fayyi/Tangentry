package httptransport

import "net/http"

func (h *Handler) registerSubscriptionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/subscription-inbox", h.feeds.inboxList)
	mux.HandleFunc("GET /api/subscription-inbox/{id}", h.feeds.inboxItem)
	mux.HandleFunc("GET /api/subscriptions", h.feeds.subscriptionList)
	mux.HandleFunc("GET /api/subscriptions/opml", h.feeds.subscriptionOPMLExport)
	mux.HandleFunc("POST /api/subscriptions/opml", h.feeds.subscriptionOPMLImport)
	mux.HandleFunc("POST /api/subscriptions", h.feeds.subscriptionCreate)
	mux.HandleFunc("POST /api/subscriptions/batch", h.feeds.subscriptionBatch)
	mux.HandleFunc("POST /api/subscriptions/youtube/resolve", h.feeds.subscriptionResolveYouTube)
	mux.HandleFunc("POST /api/subscriptions/resolve", h.feeds.subscriptionResolve)
	mux.HandleFunc("GET /api/subscriptions/{id}", h.feeds.subscriptionGet)
	mux.HandleFunc("PATCH /api/subscriptions/{id}", h.feeds.subscriptionRename)
	mux.HandleFunc("PUT /api/subscriptions/{id}/address", h.feeds.subscriptionAddress)
	mux.HandleFunc("DELETE /api/subscriptions/{id}", h.feeds.subscriptionDelete)
	mux.HandleFunc("POST /api/subscriptions/{id}/update", h.feeds.subscriptionUpdate)
	mux.HandleFunc("PUT /api/subscriptions/{id}/refresh-policy", h.feeds.subscriptionRefreshPolicy)
	mux.HandleFunc("PUT /api/subscriptions/{id}/group", h.feeds.subscriptionGroupAssign)
	mux.HandleFunc("GET /api/subscription-groups", h.feeds.subscriptionGroupList)
	mux.HandleFunc("POST /api/subscription-groups", h.feeds.subscriptionGroupCreate)
	mux.HandleFunc("PATCH /api/subscription-groups/{id}", h.feeds.subscriptionGroupRename)
	mux.HandleFunc("DELETE /api/subscription-groups/{id}", h.feeds.subscriptionGroupDelete)
	mux.HandleFunc("GET /api/favorite-folders", h.feeds.favoriteFolders)
	mux.HandleFunc("GET /api/favorite-sync", h.feeds.favoriteList)
	mux.HandleFunc("POST /api/favorite-sync", h.feeds.favoriteCreate)
	mux.HandleFunc("GET /api/favorite-sync/{id}", h.feeds.favoriteGet)
	mux.HandleFunc("PUT /api/favorite-sync/{id}", h.feeds.favoriteConfigure)
	mux.HandleFunc("DELETE /api/favorite-sync/{id}", h.feeds.favoriteRemove)
	mux.HandleFunc("POST /api/favorite-sync/{id}/sync", h.feeds.favoriteStart)
	mux.HandleFunc("POST /api/favorite-sync/{id}/cancel", h.feeds.favoriteCancel)
}
