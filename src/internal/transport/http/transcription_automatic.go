package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"tangentry/internal/automaticsubtitles"
)

func (h *transcriptionHandler) automaticCurrent(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		write(w, 200, map[string]any{"session": nil})
		return
	}
	write(w, 200, map[string]any{"session": h.ai.Automatic.Current(r.URL.Query().Get("contentId"))})
}

func (h *transcriptionHandler) automaticStart(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	var input struct {
		ContentID  string  `json:"contentId"`
		Language   string  `json:"language"`
		Capability string  `json:"capability"`
		Start      float64 `json:"startSeconds"`
		Duration   float64 `json:"durationSeconds"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input) != nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Start(r.Context(), input.ContentID, input.Language, input.Capability, input.Start, input.Duration)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusAccepted, value)
}

func (h *transcriptionHandler) automaticGet(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *transcriptionHandler) automaticDraft(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Draft(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *transcriptionHandler) automaticPlayback(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	var input automaticsubtitles.Playback
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input) != nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Playback(r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *transcriptionHandler) automaticAudio(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	const maxAudioBody = 512 << 10
	if r.ContentLength > maxAudioBody {
		write(w, http.StatusRequestEntityTooLarge, apiError{"automatic_audio_limit", "实时音频片段超过大小限制，请缩短片段后重试。"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAudioBody))
	if err != nil {
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			write(w, http.StatusRequestEntityTooLarge, apiError{"automatic_audio_limit", "实时音频片段超过大小限制，请缩短片段后重试。"})
			return
		}
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	if len(data) == 0 {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	var input struct {
		StartSeconds float64 `json:"startSeconds"`
		SampleRate   int     `json:"sampleRate"`
		Channels     int     `json:"channels"`
		Final        bool    `json:"final"`
	}
	if raw := r.Header.Get("X-Audio-Metadata"); raw != "" {
		if json.Unmarshal([]byte(raw), &input) != nil {
			fail(w, automaticsubtitles.ErrInvalid)
			return
		}
	}
	value, err := h.ai.Automatic.AppendAudio(r.PathValue("id"), automaticsubtitles.AudioChunk{Data: data, StartSeconds: input.StartSeconds, SampleRate: input.SampleRate, Channels: input.Channels, Final: input.Final})
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusAccepted, value)
}

func (h *transcriptionHandler) automaticFinalize(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Finalize(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *transcriptionHandler) automaticPause(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	value, err := h.ai.Automatic.Pause(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, value)
}

func (h *transcriptionHandler) automaticCancel(w http.ResponseWriter, r *http.Request) {
	if h.ai.Automatic == nil {
		fail(w, automaticsubtitles.ErrInvalid)
		return
	}
	if err := h.ai.Automatic.Cancel(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
