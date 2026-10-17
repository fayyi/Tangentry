package httptransport

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"

	"tangentry/internal/discovery"
)

func (h *discoveryHandler) topicsReady(w http.ResponseWriter) bool {
	if h.browse.DiscoveryTopics != nil {
		return true
	}
	fail(w, discovery.ErrUnavailable)
	return false
}

func (h *discoveryHandler) topicList(w http.ResponseWriter, r *http.Request) {
	if !h.topicsReady(w) {
		return
	}
	items, err := h.browse.DiscoveryTopics.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"topics": items})
}

func (h *discoveryHandler) topicCreate(w http.ResponseWriter, r *http.Request) {
	h.topicMutate(w, r, "create", "")
}
func (h *discoveryHandler) topicUpdate(w http.ResponseWriter, r *http.Request) {
	h.topicMutate(w, r, "update", r.PathValue("id"))
}

func (h *discoveryHandler) topicMutate(w http.ResponseWriter, r *http.Request, operation, id string) {
	if !h.topicsReady(w) {
		return
	}
	if kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || kind != "application/json" {
		write(w, http.StatusUnsupportedMediaType, apiError{"invalid_input", "请使用 JSON 保存主题规则。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var input discovery.TopicRuleEdit
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, discovery.ErrInvalidQuery)
		return
	}
	if operation == "create" {
		value, err := h.browse.DiscoveryTopics.Create(r.Context(), input)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusCreated, value)
		return
	}
	value, err := h.browse.DiscoveryTopics.Update(r.Context(), id, input)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, value)
}

func (h *discoveryHandler) topicDelete(w http.ResponseWriter, r *http.Request) {
	if !h.topicsReady(w) {
		return
	}
	revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil {
		var input struct {
			Revision int64 `json:"revision"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if decoder.Decode(&input) != nil {
			fail(w, discovery.ErrInvalidQuery)
			return
		}
		revision = input.Revision
	}
	if err := h.browse.DiscoveryTopics.Delete(r.Context(), r.PathValue("id"), revision); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
