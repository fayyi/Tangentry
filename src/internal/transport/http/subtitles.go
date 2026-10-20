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
	query, language := r.URL.Query().Get("q"), r.URL.Query().Get("language")
	var doc subtitles.Document
	var err error
	if language != "" {
		doc, err = h.media.Subtitles.ReadLanguage(r.Context(), query, language)
	} else {
		doc, err = h.media.Subtitles.Read(r.Context(), query)
	}
	if err != nil {
		fail(w, err)
		return
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
	write(w, http.StatusOK, doc)
}
