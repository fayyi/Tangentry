package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"tangentry/internal/discovery"
)

func (h *discoveryHandler) linuxdoPosts(w http.ResponseWriter, r *http.Request) {
	if h.browse.LinuxdoPosts == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("topic"))
	if id == "" {
		write(w, http.StatusBadRequest, apiError{"invalid_topic", "话题地址无效。"})
		return
	}
	values := r.URL.Query()["post_ids[]"]
	if len(values) == 0 {
		values = r.URL.Query()["post_ids"]
	}
	if len(values) == 0 || len(values) > 50 {
		write(w, http.StatusBadRequest, apiError{"invalid_post_ids", "楼层请求无效。"})
		return
	}
	ids := make([]int64, 0, len(values))
	for _, raw := range values {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			write(w, http.StatusBadRequest, apiError{"invalid_post_ids", "楼层请求无效。"})
			return
		}
		ids = append(ids, id)
	}
	discussion, err := h.browse.LinuxdoPosts(r.Context(), id, ids)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, discussion)
}
