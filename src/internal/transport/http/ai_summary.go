package httptransport

import (
	"errors"
	"net/http"

	"tangentry/internal/ai"
)

func (h *aiHandler) aiSummaryInspect(w http.ResponseWriter, r *http.Request) {
	if !h.aiReady(w) {
		return
	}
	var input ai.SummaryInput
	if !decodeAIRequest(w, r, &input, ai.MaxInputBytes) {
		return
	}
	result, err := h.ai.AI.SummaryInspect(r.Context(), input)
	if err != nil {
		summaryFail(w, err)
		return
	}
	write(w, 200, result)
}

func (h *aiHandler) aiSummaryGenerate(w http.ResponseWriter, r *http.Request) {
	if !h.aiReady(w) {
		return
	}
	var input ai.SummaryInput
	if !decodeAIRequest(w, r, &input, ai.MaxInputBytes) {
		return
	}
	if err := setAILongWriteDeadline(w); err != nil && !errors.Is(err, http.ErrNotSupported) {
		summaryFail(w, ai.ErrProvider)
		return
	}
	result, err := h.ai.AI.SummaryGenerate(r.Context(), input)
	if err != nil {
		summaryFail(w, err)
		return
	}
	write(w, 200, result)
}

func (h *aiHandler) aiSummaryCached(w http.ResponseWriter, r *http.Request) {
	if !h.aiReady(w) {
		return
	}
	if len(r.URL.Query()) != 0 {
		summaryFail(w, ai.ErrInvalid)
		return
	}
	result, err := h.ai.AI.SummaryCached(r.Context(), r.PathValue("key"))
	if err != nil {
		summaryFail(w, err)
		return
	}
	write(w, 200, result)
}

func (h *aiHandler) aiChat(w http.ResponseWriter, r *http.Request) {
	if !h.aiReady(w) {
		return
	}
	var input ai.ChatRequest
	if !decodeAIRequest(w, r, &input, 512<<10) {
		return
	}
	result, err := h.ai.AI.Chat(r.Context(), input)
	if err != nil {
		summaryFail(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *aiHandler) aiChatHistory(w http.ResponseWriter, r *http.Request) {
	if !h.aiReady(w) {
		return
	}
	if len(r.URL.Query()) != 1 || r.URL.Query().Get("contentId") == "" {
		summaryFail(w, ai.ErrInvalid)
		return
	}
	result, err := h.ai.AI.ChatHistory(r.Context(), r.URL.Query().Get("contentId"))
	if err != nil {
		summaryFail(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
