package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"tangentry/internal/playback"
	"time"
)

func (h *mediaHandler) playbackOpen(w http.ResponseWriter, r *http.Request) {
	if h.media.Playback == nil {
		write(w, 503, apiError{"playback_unavailable", "在线播放暂不可用。"})
		return
	}
	if !playback.ValidBilibiliID(r.PathValue("id")) {
		fail(w, playback.ErrInvalid)
		return
	}
	service, ok := h.media.Playback.(interface {
		OpenBilibili(context.Context, string) (playback.Session, error)
	})
	if !ok {
		// Older test and server adapters only expose the shared Open method.
		// The concrete playback.Service implements the platform-specific method,
		// so production routes remain isolated without breaking those adapters.
		h.openPlayback(w, r, h.media.Playback.Open)
		return
	}
	h.openPlayback(w, r, service.OpenBilibili)
}
func (h *mediaHandler) douyinPlaybackOpen(w http.ResponseWriter, r *http.Request) {
	service, ok := h.media.Playback.(interface {
		OpenDouyin(context.Context, string) (playback.Session, error)
	})
	if !ok {
		write(w, 503, apiError{"playback_unavailable", "抖音在线播放暂不可用。"})
		return
	}
	h.openDouyinPlayback(w, r, service.OpenDouyin)
}

func (h *mediaHandler) openDouyinPlayback(w http.ResponseWriter, r *http.Request, open func(context.Context, string) (playback.Session, error)) {
	if h.media.Playback == nil {
		write(w, 503, apiError{"playback_unavailable", "抖音在线播放暂不可用。"})
		return
	}
	trace := playback.NewDiagnosticTrace()
	ctx := playback.ContextWithDiagnostics(r.Context(), trace)
	reason := "request_sent"
	trace.Add(playback.DiagnosticStage{Stage: "frontend_request", Outcome: playback.OutcomeOK, Reason: &reason})
	session, err := open(ctx, r.PathValue("id"))
	if err != nil {
		failDouyinPlayback(w, err, true)
		return
	}
	status := http.StatusOK
	trace.Add(playback.DiagnosticStage{Stage: "frontend_request", Outcome: playback.OutcomeOK, HTTPStatus: &status, Reason: &reason})
	write(w, 200, session)
}

func (h *mediaHandler) playbackDiagnostics(w http.ResponseWriter, r *http.Request) {
	reporter, ok := h.media.Playback.(interface {
		ReportFrontendMedia(string, int, string, int) playback.AttemptDiagnostics
	})
	if !ok {
		write(w, http.StatusNotFound, apiError{"playback_expired", "播放诊断已不可用。"})
		return
	}
	var input struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Retries int    `json:"retries"`
		Ready   bool   `json:"ready"`
		Failed  bool   `json:"failed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		write(w, http.StatusBadRequest, apiError{"invalid_playback_diagnostic", "媒体错误信息无效。"})
		return
	}
	if input.Ready {
		if finalizer, ok := h.media.Playback.(interface {
			ReportMediaReady(string) playback.AttemptDiagnostics
		}); ok {
			write(w, http.StatusOK, finalizer.ReportMediaReady(r.PathValue("id")))
			return
		}
	}
	if input.Failed {
		if failureReporter, ok := h.media.Playback.(interface {
			ReportFrontendMediaFailure(string, int, string, int) playback.AttemptDiagnostics
		}); ok {
			write(w, http.StatusOK, failureReporter.ReportFrontendMediaFailure(r.PathValue("id"), input.Code, input.Message, input.Retries))
			return
		}
	}
	write(w, http.StatusOK, reporter.ReportFrontendMedia(r.PathValue("id"), input.Code, input.Message, input.Retries))
}
func (h *mediaHandler) openPlayback(w http.ResponseWriter, r *http.Request, open func(context.Context, string) (playback.Session, error)) {
	if h.media.Playback == nil {
		write(w, 503, apiError{"playback_unavailable", "在线播放暂不可用。"})
		return
	}
	value, err := open(r.Context(), r.PathValue("id"))
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
		var diagnostic *playback.DiagnosticError
		if errors.As(err, &diagnostic) {
			failDouyinPlayback(w, err, false)
			return
		}
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
	var bytesRead int64
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			bytesRead += int64(n)
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				if reporter, ok := h.media.Playback.(interface {
					ReportMediaStreamFailure(string, int, string, int64, error) playback.AttemptDiagnostics
				}); ok {
					reporter.ReportMediaStreamFailure(r.PathValue("id"), response.StatusCode, response.Header.Get("Content-Type"), bytesRead, err)
				}
				return
			}
			if err := controller.Flush(); err != nil {
				if reporter, ok := h.media.Playback.(interface {
					ReportMediaStreamFailure(string, int, string, int64, error) playback.AttemptDiagnostics
				}); ok {
					reporter.ReportMediaStreamFailure(r.PathValue("id"), response.StatusCode, response.Header.Get("Content-Type"), bytesRead, err)
				}
				return
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return
			}
			if reporter, ok := h.media.Playback.(interface {
				ReportMediaStreamFailure(string, int, string, int64, error) playback.AttemptDiagnostics
			}); ok {
				reporter.ReportMediaStreamFailure(r.PathValue("id"), response.StatusCode, response.Header.Get("Content-Type"), bytesRead, readErr)
			}
			return
		}
	}
}
