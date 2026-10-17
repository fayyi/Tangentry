package httptransport

import (
	"net/http"

	"tangentry/internal/subscriptions"
)

func (h *feedsHandler) subscriptionRefreshPolicy(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	var input struct {
		Enabled         *bool `json:"enabled"`
		IntervalSeconds int64 `json:"intervalSeconds"`
	}
	if !decodeSubscription(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		write(w, 400, apiError{"invalid_refresh_policy", "请选择是否自动刷新，并设置 15 分钟至 7 天的间隔。"})
		return
	}
	source, err := h.feeds.Subscriptions.ConfigureRefresh(r.Context(), r.PathValue("id"), subscriptions.RefreshPolicy{Enabled: *input.Enabled, IntervalSeconds: input.IntervalSeconds})
	if err != nil {
		if err == subscriptions.ErrInvalid {
			write(w, 400, apiError{"invalid_refresh_policy", "RSS 与 YouTube 频道支持自动刷新，间隔为 15 分钟至 7 天。"})
		} else {
			fail(w, err)
		}
		return
	}
	write(w, 200, source)
}
