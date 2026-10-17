package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"tangentry/internal/discovery"
	"time"
)

var recommendationSessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func (h *discoveryHandler) recommendationSettingsRead(w http.ResponseWriter, r *http.Request) {
	if h.browse.RecommendationConfig == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	values, err := h.browse.RecommendationConfig.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]any{"sources": values})
}

func (h *discoveryHandler) recommendationSettingsSave(w http.ResponseWriter, r *http.Request) {
	if h.browse.RecommendationConfig == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 保存推荐设置。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var value struct {
		Enabled *bool `json:"enabled"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF || value.Enabled == nil {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	if err := h.browse.RecommendationConfig.Save(r.Context(), r.PathValue("source"), *value.Enabled); err != nil {
		fail(w, err)
		return
	}
	h.recommendationSettingsRead(w, r)
}

func (h *discoveryHandler) recommendationAllowed(w http.ResponseWriter, r *http.Request, source string) bool {
	if source == "subscriptions" || h.browse.RecommendationConfig == nil {
		return true
	}
	enabled, err := h.browse.RecommendationConfig.Enabled(r.Context(), source)
	if err != nil {
		fail(w, err)
		return false
	}
	if !enabled {
		write(w, 409, apiError{"recommendations_disabled", "此来源已停止获取推荐，已存内容和订阅不受影响。"})
		return false
	}
	return true
}

func (h *discoveryHandler) recommendationPending(w http.ResponseWriter, r *http.Request) {
	if h.browse.RecommendationQueue == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
	}
	page, err := h.browse.RecommendationQueue.PendingPage(r.Context(), r.PathValue("source"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, page)
}

func (h *discoveryHandler) recommendationMark(w http.ResponseWriter, r *http.Request) {
	if h.browse.RecommendationQueue == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 提交浏览标记。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		ContentID string `json:"contentId"`
		Opened    *bool  `json:"opened"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.Opened == nil {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	if err := h.browse.RecommendationQueue.Mark(r.Context(), r.PathValue("source"), input.ContentID, *input.Opened); err != nil {
		fail(w, err)
		return
	}
	write(w, 200, map[string]bool{"saved": true})
}

func (h *discoveryHandler) recommendationRead(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 10000 {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
	}
	if source != "subscriptions" && !discovery.RecommendationPlatform(source) {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	if h.browse.Recommendations == nil && h.browse.RecommendationPage == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	if !h.recommendationAllowed(w, r, source) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()
	select {
	case h.browsing <- struct{}{}:
		defer func() { <-h.browsing }()
	case <-ctx.Done():
		write(w, 429, apiError{"recommendations_busy", "推荐正在获取，请稍后重试。"})
		return
	}
	// A queued request must also respect a switch turned off while it waited.
	if !h.recommendationAllowed(w, r, source) {
		return
	}
	if session := r.Header.Get("X-Recommendation-Session"); session != "" {
		if !recommendationSessionPattern.MatchString(session) {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
		ctx = discovery.WithRecommendationSession(ctx, session)
	}
	var result discovery.Feed
	var err error
	if h.browse.RecommendationPage != nil {
		result, err = h.browse.RecommendationPage(ctx, source, page)
	} else {
		result, err = h.browse.Recommendations(ctx, source)
	}
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, result)
}
