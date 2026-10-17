package httptransport

import (
	"net/http"

	"tangentry/internal/subscriptions"
)

func (h *feedsHandler) subscriptionList(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	items, err := h.feeds.Subscriptions.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"sources": items})
}

func (h *feedsHandler) subscriptionGet(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	detail, err := h.feeds.Subscriptions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, detail)
}

func (h *feedsHandler) subscriptionCreate(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input subscriptions.Input
	if !decodeSubscription(w, r, &input) {
		return
	}
	source, created, err := h.feeds.Subscriptions.Create(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	write(w, status, map[string]any{"source": source, "created": created})
}

func (h *feedsHandler) subscriptionRename(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input subscriptions.Edit
	if !decodeSubscription(w, r, &input) {
		return
	}
	source, err := h.feeds.Subscriptions.Rename(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, source)
}

func (h *feedsHandler) subscriptionAddress(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input subscriptions.AddressEdit
	if len(r.URL.Query()) != 0 || !decodeSubscription(w, r, &input) {
		if len(r.URL.Query()) != 0 {
			fail(w, subscriptions.ErrInvalid)
		}
		return
	}
	source, err := h.feeds.Subscriptions.EditAddress(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, source)
}

func (h *feedsHandler) subscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	detail, err := h.feeds.Subscriptions.Update(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, detail)
}

func (h *feedsHandler) subscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	if err := h.feeds.Subscriptions.Delete(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
