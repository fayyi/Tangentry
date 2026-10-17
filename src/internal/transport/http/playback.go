package httptransport

import (
	"io"
	"net/http"
	"time"
)

func (h *mediaHandler) playbackOpen(w http.ResponseWriter, r *http.Request) {
	if h.media.Playback == nil {
		write(w, 503, apiError{"playback_unavailable", "在线播放暂不可用。"})
		return
	}
	value, err := h.media.Playback.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}
func (h *mediaHandler) playbackRelease(w http.ResponseWriter, r *http.Request) {
	if h.media.Playback != nil {
		h.media.Playback.Release(r.PathValue("id"))
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *mediaHandler) playbackMedia(w http.ResponseWriter, r *http.Request) {
	if h.media.Playback == nil {
		write(w, 503, apiError{"playback_unavailable", "在线播放暂不可用。"})
		return
	}
	response, release, err := h.media.Playback.Fetch(r.Context(), r.PathValue("id"), r.Method, r.Header.Get("Range"))
	if err != nil {
		fail(w, err)
		return
	}
	defer release()
	defer response.Body.Close()
	for _, name := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", "inline")
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.WriteHeader(response.StatusCode)
	if r.Method == http.MethodHead || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return
	}
	// Renew only this media response's write deadline while bytes make progress.
	// Ordinary APIs retain the server's bounded 40-second write timeout.
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return
			}
			return
		}
	}
}
