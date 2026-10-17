package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"tangentry/internal/favoritesync"
)

func (h *feedsHandler) favoriteReady(w http.ResponseWriter) bool {
	if h.feeds.Favorites == nil {
		write(w, 503, apiError{"favorite_sync_unavailable", "收藏同步暂时不可用。"})
		return false
	}
	return true
}
func (h *feedsHandler) favoriteFolders(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	folders, err := h.feeds.Favorites.Folders(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"folders": folders})
}
func (h *feedsHandler) favoriteList(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	sources, err := h.feeds.Favorites.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	// Polling summaries never retransmit cached media or the transfer journal.
	items := make([]map[string]any, 0, len(sources))
	for _, s := range sources {
		items = append(items, map[string]any{"folderId": s.FolderID, "title": s.Title, "policy": s.Policy, "createdAt": s.CreatedAt, "lastAttemptAt": s.LastAttemptAt, "lastSuccessAt": s.LastSuccessAt, "excluded": s.Excluded, "message": s.Message, "active": s.Active, "itemCount": len(s.Items), "pendingCount": len(s.Pending)})
	}
	write(w, 200, map[string]any{"sources": items})
}
func (h *feedsHandler) favoriteGet(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	source, err := h.feeds.Favorites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, source)
}
func decodeFavorite(w http.ResponseWriter, r *http.Request, input any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 提交收藏同步配置。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, favoritesync.ErrInvalid)
		return false
	}
	return true
}
func (h *feedsHandler) favoriteCreate(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	var input favoritesync.Input
	if !decodeFavorite(w, r, &input) {
		return
	}
	source, err := h.feeds.Favorites.Create(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 201, source)
}
func (h *feedsHandler) favoriteConfigure(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	var policy favoritesync.Policy
	if !decodeFavorite(w, r, &policy) {
		return
	}
	source, err := h.feeds.Favorites.Configure(r.Context(), r.PathValue("id"), policy)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, source)
}
func (h *feedsHandler) favoriteRemove(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	if err := h.feeds.Favorites.Remove(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *feedsHandler) favoriteStart(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	source, err := h.feeds.Favorites.Start(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 202, source)
}
func (h *feedsHandler) favoriteCancel(w http.ResponseWriter, r *http.Request) {
	if !h.favoriteReady(w) {
		return
	}
	if err := h.feeds.Favorites.Cancel(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	write(w, 202, map[string]bool{"cancelRequested": true})
}
