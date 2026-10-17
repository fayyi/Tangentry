package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"tangentry/internal/articles"
	"tangentry/internal/netpolicy"
	"time"
)

func (h *libraryHandler) articleGet(w http.ResponseWriter, r *http.Request) {
	if h.library.Articles == nil {
		write(w, 503, apiError{"article_unavailable", "原站全文暂时不可用。"})
		return
	}
	if len(r.URL.Query()) != 0 {
		fail(w, articles.ErrInvalid)
		return
	}
	doc, err := h.library.Articles.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, doc)
}

func (h *libraryHandler) articleImage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) < 1 || len(r.URL.Query()) > 2 || r.URL.Query().Get("url") == "" {
		write(w, http.StatusBadRequest, apiError{"invalid_article_image", "图片地址无效。"})
		return
	}
	raw, err := netpolicy.URL(r.URL.Query().Get("url"))
	if err != nil {
		write(w, http.StatusBadRequest, apiError{"invalid_article_image", "图片地址无效。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	referer := ""
	if referrer := r.URL.Query().Get("referrer"); referrer != "" {
		if safeReferrer, err := netpolicy.URL(referrer); err == nil {
			if source, err := url.Parse(safeReferrer); err == nil {
				referer = source.Scheme + "://" + source.Host + "/"
			}
		}
	}
	res, err := h.fetchArticleImage(ctx, raw, referer)
	// Hotlink protection often accepts no Referer while rejecting a foreign one.
	if err == nil && referer != "" && (res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized) {
		res.Body.Close()
		res, err = h.fetchArticleImage(ctx, raw, "")
	}
	if err != nil {
		write(w, http.StatusBadGateway, apiError{"article_image_unavailable", "图片暂时无法读取。"})
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		write(w, http.StatusBadGateway, apiError{"article_image_unavailable", "图片暂时无法读取。"})
		return
	}
	declared, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	const maxImageBytes = 8 << 20
	if res.ContentLength > maxImageBytes {
		write(w, http.StatusRequestEntityTooLarge, apiError{"article_image_too_large", "图片超过 8 MB，暂时无法显示。"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		write(w, http.StatusRequestEntityTooLarge, apiError{"article_image_too_large", "图片超过 8 MB 或读取不完整，暂时无法显示。"})
		return
	}
	// Many CDNs label images application/octet-stream; trust the bytes, not the
	// label, and never pass through SVG, which can carry script.
	typ := declared
	if !strings.HasPrefix(typ, "image/") {
		typ, _, _ = mime.ParseMediaType(http.DetectContentType(data))
	}
	if !strings.HasPrefix(typ, "image/") || typ == "image/svg+xml" {
		write(w, http.StatusUnsupportedMediaType, apiError{"article_image_unavailable", "原站没有返回可显示的图片。"})
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=604800, immutable")
	_, _ = w.Write(data)
}

// Some image hosts refuse requests that do not look like a browser.
const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"

func (h *libraryHandler) fetchArticleImage(ctx context.Context, raw, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif,image/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return h.library.ArticleImageHTTP.Do(req)
}

func (h *libraryHandler) articleFetch(w http.ResponseWriter, r *http.Request) {
	if h.library.Articles == nil {
		write(w, 503, apiError{"article_unavailable", "原站全文暂时不可用。"})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		write(w, 415, apiError{"invalid_article", "请使用 JSON 提交全文请求。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		ExpectedRevision *int64 `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if len(r.URL.Query()) != 0 || decoder.Decode(&input) != nil || input.ExpectedRevision == nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, articles.ErrInvalid)
		return
	}
	doc, err := h.library.Articles.Fetch(r.Context(), r.PathValue("id"), *input.ExpectedRevision)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, doc)
}
