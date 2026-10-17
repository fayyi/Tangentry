package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"tangentry/internal/content"
)

var imageVersionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (h *discoveryHandler) xiaohongshuDetailRead(w http.ResponseWriter, r *http.Request) {
	if h.browse.XiaohongshuDetail == nil {
		write(w, 200, map[string]any{"item": nil, "complete": false})
		return
	}
	item, complete, err := h.browse.XiaohongshuDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if item.ID == "" {
		write(w, 200, map[string]any{"item": nil, "complete": false})
		return
	}
	write(w, 200, map[string]any{"item": item, "complete": complete})
}

func (h *discoveryHandler) xiaohongshuDetailRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if !strings.HasPrefix(id, "xiaohongshu:") || !content.ValidID(id) || decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(body.URL) == "" {
		fail(w, content.ErrInvalid)
		return
	}
	if h.browse.XiaohongshuRefresh == nil {
		write(w, 503, apiError{"xiaohongshu_unavailable", "笔记读取暂不可用。"})
		return
	}
	select {
	case h.importing <- struct{}{}:
		defer func() { <-h.importing }()
	default:
		write(w, 429, apiError{"import_busy", "正在读取其他内容，请稍后重试。"})
		return
	}
	item, err := h.browse.XiaohongshuRefresh(r.Context(), id, body.URL)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, item)
}

func (h *discoveryHandler) xiaohongshuImageRead(w http.ResponseWriter, r *http.Request) {
	id, version := r.PathValue("id"), r.PathValue("version")
	if !strings.HasPrefix(id, "xiaohongshu:") || !content.ValidID(id) || !imageVersionPattern.MatchString(version) {
		fail(w, content.ErrInvalid)
		return
	}
	if h.browse.XiaohongshuImages == nil || h.browse.XiaohongshuImageItems == nil {
		fail(w, content.ErrNotFound)
		return
	}
	items, err := h.browse.XiaohongshuImageItems(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	find := func(item content.Item) string {
		if item.ID != id {
			return ""
		}
		for _, raw := range append([]string{item.ThumbnailURL, item.AuthorAvatarURL}, imageURLs(item.Images)...) {
			if raw != "" && h.imageVersion(raw) == version {
				return raw
			}
		}
		return ""
	}
	raw := ""
	for _, item := range items {
		if raw = find(item); raw != "" {
			break
		}
	}
	if raw == "" {
		fail(w, content.ErrNotFound)
		return
	}
	data, err := h.browse.XiaohongshuImages.Read(r.Context(), raw)
	if err != nil {
		write(w, 502, apiError{"xiaohongshu_image_unavailable", "图片暂不可用，请更新分享链接或图集。"})
		return
	}
	w.Header().Set("Content-Type", data.Type)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+version+`"`)
	w.Write(data.Bytes)
}

func (h *discoveryHandler) imageVersion(raw string) string {
	if h.browse.XiaohongshuImageVersion != nil {
		return h.browse.XiaohongshuImageVersion(raw)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func imageURLs(images []content.Image) []string {
	urls := make([]string, 0, len(images))
	for _, image := range images {
		urls = append(urls, image.URL)
	}
	return urls
}
