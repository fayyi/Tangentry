package httptransport

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"tangentry/internal/subscriptions"
)

func (h *feedsHandler) subscriptionOPMLExport(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	body, err := h.feeds.Subscriptions.ExportOPML(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tangentry-subscriptions.opml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *feedsHandler) subscriptionOPMLImport(w http.ResponseWriter, r *http.Request) {
	if !h.subscriptionReady(w) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/xml" && mediaType != "text/xml" && mediaType != "application/octet-stream") {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请上传 OPML 或 XML 文件。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil || strings.TrimSpace(string(body)) == "" {
		fail(w, subscriptions.ErrInvalid)
		return
	}
	result, err := h.feeds.Subscriptions.ImportOPML(r.Context(), body)
	if err != nil {
		if errors.Is(err, subscriptions.ErrInvalid) {
			write(w, http.StatusBadRequest, apiError{"invalid_input", "OPML 文件无效，或超过 500 个来源。请检查后重新导入。"})
			return
		}
		fail(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
