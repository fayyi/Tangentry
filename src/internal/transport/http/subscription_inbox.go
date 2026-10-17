package httptransport

import "net/http"
import "tangentry/internal/subscriptions"
import "strconv"

func (h *feedsHandler) inboxList(w http.ResponseWriter, r *http.Request) {
	if h.feeds.SubscriptionInbox == nil {
		write(w, 503, apiError{"inbox_unavailable", "历史收件箱暂时不可用。"})
		return
	}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 {
			fail(w, subscriptions.ErrInboxInvalid)
			return
		}
		switch key {
		case "q", "sourceId", "groupId", "kind", "read", "cursor", "limit":
		default:
			fail(w, subscriptions.ErrInboxInvalid)
			return
		}
	}
	limit := subscriptions.InboxPageSize
	if q.Has("limit") {
		parsed, err := strconv.Atoi(q.Get("limit"))
		if err != nil || strconv.Itoa(parsed) != q.Get("limit") || parsed <= 0 {
			fail(w, subscriptions.ErrInboxInvalid)
			return
		}
		limit = parsed
	}
	page, err := h.feeds.SubscriptionInbox.List(r.Context(), subscriptions.InboxQuery{Query: q.Get("q"), SourceID: q.Get("sourceId"), GroupID: q.Get("groupId"), Kind: q.Get("kind"), Read: q.Get("read"), Cursor: q.Get("cursor"), Limit: limit})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, page)
}
func (h *feedsHandler) inboxItem(w http.ResponseWriter, r *http.Request) {
	if h.feeds.SubscriptionInbox == nil {
		write(w, 503, apiError{"inbox_unavailable", "历史收件箱暂时不可用。"})
		return
	}
	item, err := h.feeds.SubscriptionInbox.Item(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, item)
}
