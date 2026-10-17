package httptransport

import "net/http"

func (h *Handler) registerAIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ai/settings", h.ai.aiSettingsGet)
	mux.HandleFunc("PUT /api/ai/settings", h.ai.aiSettingsPut)
	mux.HandleFunc("POST /api/ai/check", h.ai.aiCheck)
	mux.HandleFunc("POST /api/ai/subtitles/inspect", h.ai.aiSubtitleInspect)
	mux.HandleFunc("POST /api/ai/subtitles/translate", h.ai.aiSubtitleTranslate)
	mux.HandleFunc("GET /api/ai/subtitles/{key}", h.ai.aiSubtitleCached)
	mux.HandleFunc("POST /api/ai/summaries/inspect", h.ai.aiSummaryInspect)
	mux.HandleFunc("POST /api/ai/summaries/generate", h.ai.aiSummaryGenerate)
	mux.HandleFunc("GET /api/ai/summaries/{key}", h.ai.aiSummaryCached)
	mux.HandleFunc("GET /api/transcription/models", h.transcription.modelList)
	mux.HandleFunc("POST /api/transcription/models/{id}/download", h.transcription.modelDownload)
	mux.HandleFunc("POST /api/transcription/model-downloads/{id}/cancel", h.transcription.modelDownloadCancel)
	mux.HandleFunc("GET /api/transcription/settings", h.transcription.transcriptionStatus)
	mux.HandleFunc("PUT /api/transcription/settings", h.transcription.transcriptionConfigure)
	mux.HandleFunc("POST /api/transcription/jobs", h.transcription.transcriptionStart)
	mux.HandleFunc("GET /api/transcription/jobs", h.transcription.transcriptionCurrent)
	mux.HandleFunc("GET /api/transcription/jobs/{id}", h.transcription.transcriptionGet)
	mux.HandleFunc("POST /api/transcription/jobs/{id}/cancel", h.transcription.transcriptionCancel)
}
