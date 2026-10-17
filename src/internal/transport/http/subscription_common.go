package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"tangentry/internal/subscriptions"
)

func (h *feedsHandler) subscriptionReady(w http.ResponseWriter) bool {
	if h.feeds.Subscriptions != nil {
		return true
	}
	write(w, 503, apiError{"subscriptions_unavailable", "订阅暂时不可用。"})
	return false
}

func decodeSubscription(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		write(w, 415, apiError{"invalid_input", "请使用 JSON 格式提交来源。"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, subscriptions.ErrInvalid)
		return false
	}
	return true
}
