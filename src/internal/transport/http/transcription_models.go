package httptransport

import (
	"net/http"

	"tangentry/internal/transcription"
)

func (h *transcriptionHandler) modelList(w http.ResponseWriter, r *http.Request) {
	if h.ai.ModelDownloads == nil {
		write(w, 200, map[string]any{"models": []transcription.ModelOption{}, "job": nil, "available": false})
		return
	}
	models, job, available := h.ai.ModelDownloads.Status()
	write(w, 200, map[string]any{"models": models, "job": job, "available": available})
}

func (h *transcriptionHandler) modelDownload(w http.ResponseWriter, r *http.Request) {
	if h.ai.ModelDownloads == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	job, err := h.ai.ModelDownloads.Start(r.PathValue("id"))
	if err != nil {
		if err == transcription.ErrBusy {
			write(w, 409, apiError{"model_download_busy", "已有模型正在下载，请等待完成或取消。"})
			return
		}
		fail(w, err)
		return
	}
	write(w, 202, job)
}

func (h *transcriptionHandler) modelDownloadCancel(w http.ResponseWriter, r *http.Request) {
	if h.ai.ModelDownloads == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	if err := h.ai.ModelDownloads.Cancel(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
