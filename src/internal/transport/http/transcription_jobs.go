package httptransport

import (
	"net/http"

	"tangentry/internal/transcription"
)

func (h *transcriptionHandler) transcriptionStatus(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	write(w, 200, h.ai.Transcription.Status())
}

func (h *transcriptionHandler) transcriptionConfigure(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	var value transcription.SettingsInput
	if !decodeTranscription(w, r, &value) {
		return
	}
	status, err := h.ai.Transcription.ConfigureSettings(value)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, status)
}

func (h *transcriptionHandler) transcriptionStart(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	var value struct {
		ContentID     string `json:"contentId"`
		Preview       bool   `json:"preview"`
		UploadConsent bool   `json:"uploadConsent"`
		Backend       string `json:"backend"`
	}
	if !decodeTranscription(w, r, &value) {
		return
	}
	job, err := h.ai.Transcription.StartWithUploadConsent(r.Context(), value.ContentID, value.Preview, value.UploadConsent, value.Backend)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 202, job)
}

func (h *transcriptionHandler) transcriptionCurrent(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	write(w, 200, map[string]any{"job": h.ai.Transcription.Current(r.URL.Query().Get("contentId"))})
}

func (h *transcriptionHandler) transcriptionGet(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	job, err := h.ai.Transcription.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, job)
}

func (h *transcriptionHandler) transcriptionCancel(w http.ResponseWriter, r *http.Request) {
	if h.ai.Transcription == nil {
		fail(w, transcription.ErrNotReady)
		return
	}
	if err := h.ai.Transcription.Cancel(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
