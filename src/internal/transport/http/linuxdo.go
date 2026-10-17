package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"tangentry/internal/discovery"
)

func (h *accountsHandler) linuxdoSessionCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.accounts.LinuxdoSession == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	state, err := h.accounts.LinuxdoSession(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, state)
}

func (h *accountsHandler) linuxdoLoginStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.accounts.LinuxdoLogin == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var value LinuxdoState
	var err error
	if r.URL.Query().Get("mode") == "browser" {
		value, err = h.accounts.LinuxdoLogin.StartBrowser(ctx)
	} else {
		value, err = h.accounts.LinuxdoLogin.Start(ctx)
	}
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *accountsHandler) linuxdoLoginComplete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.accounts.LinuxdoLogin == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	var input struct {
		Callback string `json:"callback"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<10)).Decode(&input) != nil {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.LinuxdoLogin.CompleteBrowser(ctx, r.PathValue("id"), input.Callback)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}
func (h *accountsHandler) linuxdoLoginPoll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.accounts.LinuxdoLogin == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.accounts.LinuxdoLogin.Poll(ctx, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}
func (h *accountsHandler) linuxdoLoginCancel(w http.ResponseWriter, r *http.Request) {
	if h.accounts.LinuxdoLogin != nil {
		h.accounts.LinuxdoLogin.Cancel(r.PathValue("id"))
	}
	w.WriteHeader(http.StatusNoContent)
}
