package httptransport

import (
	"encoding/json"
	"io"
	"net/http"

	"tangentry/internal/discovery"
)

func (h *mediaHandler) compatibilityOpen(w http.ResponseWriter, r *http.Request) {
	if h.media.Compatibility == nil {
		write(w, 503, apiError{"compatibility_unavailable", "兼容播放暂不可用。"})
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		write(w, 400, apiError{"invalid_playback", "兼容播放请求无效。"})
		return
	}
	locator := ""
	if ref, err := discovery.ParseXiaohongshu(body.URL); err == nil && ref.ID != "" {
		locator = ref.URL
	} else if ref, err := discovery.ParseDouyin(body.URL); err == nil && ref.ID != "" {
		locator = ref.URL
	} else {
		write(w, 400, apiError{"invalid_playback", "请使用小红书笔记或抖音视频的完整地址。"})
		return
	}
	job, err := h.media.Compatibility.Open(locator)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 202, job)
}

func (h *mediaHandler) compatibilityGet(w http.ResponseWriter, r *http.Request) {
	if h.media.Compatibility == nil {
		write(w, 503, apiError{"compatibility_unavailable", "兼容播放暂不可用。"})
		return
	}
	job, err := h.media.Compatibility.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, job)
}

func (h *mediaHandler) compatibilityMedia(w http.ResponseWriter, r *http.Request) {
	if h.media.Compatibility == nil {
		write(w, 503, apiError{"compatibility_unavailable", "兼容播放暂不可用。"})
		return
	}
	file, err := h.media.Compatibility.File(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		write(w, 502, apiError{"compatibility_unavailable", "兼容视频暂不可用。"})
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "playback.mp4", info.ModTime(), file)
}

func (h *mediaHandler) compatibilityRelease(w http.ResponseWriter, r *http.Request) {
	if h.media.Compatibility != nil {
		h.media.Compatibility.Release(r.PathValue("id"))
	}
	w.WriteHeader(204)
}
