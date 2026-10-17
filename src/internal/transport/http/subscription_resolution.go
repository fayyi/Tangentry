package httptransport

import "net/http"

func (h *feedsHandler) subscriptionResolveYouTube(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	channel, err := h.feeds.Subscriptions.ResolveYouTube(r.Context(), input.Query)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, channel)
}

func (h *feedsHandler) subscriptionResolve(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	result, err := h.feeds.Subscriptions.Resolve(r.Context(), input.Query)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
