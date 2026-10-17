package httptransport

import "net/http"

// registerPodcastRoutes keeps podcast and local-media transcript acquisition
// separate from generic subtitle storage and playback routes.
func (h *Handler) registerPodcastRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/subtitles/podcast", h.podcast.podcastTranscript)
	mux.HandleFunc("GET /api/subtitles/local-media", h.podcast.podcastTranscript)
}
