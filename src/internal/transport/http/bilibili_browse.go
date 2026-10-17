package httptransport

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"tangentry/internal/discovery"
)

func (h *discoveryHandler) bilibiliBrowse(w http.ResponseWriter, r *http.Request) {
	if h.browse.BilibiliBrowser == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	page, area := 1, 1
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
	}
	if raw := r.URL.Query().Get("area"); raw != "" {
		area, err = strconv.Atoi(raw)
		if err != nil {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
	}
	select {
	case h.browsing <- struct{}{}:
		defer func() { <-h.browsing }()
	default:
		write(w, 429, apiError{"busy", "正在获取内容，请稍后再试。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := h.browse.BilibiliBrowser.Browse(ctx, r.PathValue("kind"), page, r.URL.Query().Get("cursor"), area)
	if err != nil {
		fail(w, err)
		return
	}
	if h.browse.RecommendationQueue != nil && len(result.Items) > 0 {
		_, err = h.browse.RecommendationQueue.Fetch(ctx, "bilibili", func(context.Context) (discovery.Feed, error) { return discovery.Feed{Items: result.Items}, nil })
		if err != nil {
			fail(w, err)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, result)
}
func (h *discoveryHandler) bilibiliProfile(w http.ResponseWriter, r *http.Request) {
	if h.browse.BilibiliBrowser == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	value, err := h.browse.BilibiliBrowser.Profile(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, value)
}
