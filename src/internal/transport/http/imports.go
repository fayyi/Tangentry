package httptransport

import (
	"errors"
	"net/http"
	"tangentry/internal/discovery"
)

func (h *discoveryHandler) importSearch(w http.ResponseWriter, r *http.Request) {
	if h.browse.Imports == nil {
		fail(w, discovery.ErrUnavailable)
		return
	}
	if r.URL.Query().Get("page") != "" && r.URL.Query().Get("page") != "1" {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	select {
	case h.importing <- struct{}{}:
		defer func() { <-h.importing }()
	default:
		write(w, 429, apiError{"source_busy", "已有内容正在获取，请稍后重试。"})
		return
	}
	feed, err := h.browse.Imports(r.Context(), r.PathValue("source"), r.URL.Query().Get("q"))
	if err != nil {
		if r.PathValue("source") == "xiaohongshu" && errors.Is(err, discovery.ErrInvalidQuery) {
			write(w, 400, apiError{"invalid_xiaohongshu_link", "请输入小红书单条笔记完整地址或分享链接；当前不支持用户主页与列表。"})
			return
		}
		if r.PathValue("source") == "douyin" && errors.Is(err, discovery.ErrInvalidQuery) {
			write(w, 400, apiError{"invalid_douyin_link", "请输入抖音单视频 /video/ 地址、modal_id 地址或有效分享链接；当前不支持作者主页与图集。"})
			return
		}
		fail(w, err)
		return
	}
	write(w, 200, feed)
}
