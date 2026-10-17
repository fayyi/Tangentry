package httptransport

import (
	"net/http"

	"tangentry/internal/subtitles"
)

func (h *mediaHandler) subtitleRead(w http.ResponseWriter, r *http.Request) {
	if h.media.Subtitles == nil {
		fail(w, subtitles.ErrTools)
		return
	}
	doc, err := h.media.Subtitles.Read(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	if h.media.SavedSubtitles != nil {
		if err := h.media.SavedSubtitles.Capture(r.Context(), "bilibili:"+doc.VideoID, doc); err != nil {
			fail(w, err)
			return
		}
	}
	write(w, http.StatusOK, doc)
}

func (h *mediaHandler) youtubeSubtitleRead(w http.ResponseWriter, r *http.Request) {
	if h.media.Subtitles == nil {
		fail(w, subtitles.ErrTools)
		return
	}
	doc, err := h.media.Subtitles.ReadYouTube(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("language"))
	if err != nil {
		fail(w, err)
		return
	}
	if h.media.SavedSubtitles != nil {
		if err := h.media.SavedSubtitles.Capture(r.Context(), "youtube:"+doc.VideoID, doc); err != nil {
			fail(w, err)
			return
		}
	}
	write(w, http.StatusOK, doc)
}
