package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tangentry/internal/agent"
	"tangentry/internal/subscriptions"
)

type handler struct {
	service    *agent.Service
	token      string
	host       string
	expires    int64
	scopes     map[string]bool
	revoked    atomic.Bool
	imageMu    sync.Mutex
	images     map[string]imageResource
	imageBytes int64
}

const (
	imageResourceTTL  = time.Minute
	maxImageBytes     = 8 << 20
	maxImagePoolBytes = 64 << 20
	maxImagePoolItems = 36
)

type imageResource struct {
	data      []byte
	mimeType  string
	expiresAt time.Time
}

func NewHandler(service *agent.Service, token string, scopes []string, host string, expires int64) http.Handler {
	return newHandler(service, token, scopes, host, expires)
}

func newHandler(service *agent.Service, token string, scopes []string, host string, expires int64) *handler {
	allowed := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		allowed[scope] = true
	}
	return &handler{service: service, token: token, host: host, expires: expires, scopes: allowed, images: make(map[string]imageResource)}
}

func (h *handler) revoke() {
	if h != nil {
		h.revoked.Store(true)
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Host != h.host || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+h.host || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "forbidden_origin", "message": "请求来源不受支持。"})
		return
	}
	if h.revoked.Load() || h.expires <= time.Now().Unix() || h.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "gateway_unavailable", "message": "Agent Gateway 不可用。"})
		return
	}
	if !constantTokenMatch(h.token, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "unauthorized", "message": "需要有效的 Agent 令牌。"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/agent/v1/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": "Agent 路径不存在。"})
		return
	}
	ctx := r.Context()
	if runID := strings.TrimSpace(r.Header.Get("X-Tangentry-Agent-Run")); runID != "" {
		if !idempotentRunID(runID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_run_id", "message": "invalid Agent run id"})
			return
		}
		ctx = agent.WithRunID(ctx, runID)
	}
	if strings.HasPrefix(r.URL.Path, "/agent/v1/frame/") {
		if !h.require(w, agent.ScopeImageRead) {
			return
		}
		h.serveImage(w, r)
		return
	}
	switch r.URL.Path {
	case "/agent/v1/capabilities":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"code": "method_not_allowed", "message": "请求方法不受支持。"})
			return
		}
		writeJSON(w, http.StatusOK, h.service.Capabilities(ctx))
	case "/agent/v1/search":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input struct {
			Query    string `json:"query"`
			Platform string `json:"platform"`
			Cursor   string `json:"cursor"`
			Fetch    bool   `json:"fetch"`
		}
		if !h.decode(w, r, &input, 16<<10) {
			return
		}
		if input.Fetch && !h.require(w, agent.ScopeNetworkFetch) {
			return
		}
		result, err := h.service.Search(ctx, input.Query, input.Platform, input.Cursor, input.Fetch)
		h.respond(w, result, err)
	case "/agent/v1/content":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input struct {
			ID    string `json:"id"`
			Fetch bool   `json:"fetch"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		if input.Fetch && !h.require(w, agent.ScopeNetworkFetch) {
			return
		}
		result, err := h.service.Content(ctx, input.ID, input.Fetch)
		h.respond(w, result, err)
	case "/agent/v1/inbox":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input inboxRequest
		if !h.decode(w, r, &input, 16<<10) {
			return
		}
		result, err := h.service.Inbox(ctx, input.query())
		h.respond(w, result, err)
	case "/agent/v1/subtitles":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input struct {
			ID       string `json:"id"`
			Language string `json:"language"`
			Fetch    bool   `json:"fetch"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		if input.Fetch && !h.require(w, agent.ScopeNetworkFetch) {
			return
		}
		result, err := h.service.Subtitles(ctx, input.ID, input.Language, input.Fetch)
		h.respond(w, result, err)
	case "/agent/v1/social":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input struct {
			ID string `json:"id"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.Social(ctx, input.ID)
		h.respond(w, result, err)
	case "/agent/v1/frames":
		if !h.require(w, agent.ScopeImageRead) {
			return
		}
		var input struct {
			ID        string    `json:"id"`
			Positions []float64 `json:"positions"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.CaptureFrames(ctx, input.ID, input.Positions)
		if err == nil {
			result = h.publishImages(result)
		}
		h.respond(w, result, err)
	case "/agent/v1/notes":
		if !h.require(w, agent.ScopePrivateNotes) {
			return
		}
		var input struct {
			Query     string `json:"query"`
			ContentID string `json:"contentId"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.Notes(ctx, input.Query, input.ContentID)
		h.respond(w, map[string]any{"notes": result, "untrusted": true}, err)
	case "/agent/v1/list-notes":
		if !h.require(w, agent.ScopePrivateNotes) {
			return
		}
		var input struct {
			ContentID string `json:"contentId"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.Notes(ctx, "", input.ContentID)
		h.respond(w, map[string]any{"notes": result, "untrusted": true}, err)
	case "/agent/v1/summarize", "/agent/v1/translate", "/agent/v1/transcribe":
		if !h.require(w, agent.ScopeDerived) {
			return
		}
		var input agent.DerivedRequest
		if !h.decode(w, r, &input, 16<<10) {
			return
		}
		action := map[string]string{"/agent/v1/summarize": "summarize_content", "/agent/v1/translate": "translate_content", "/agent/v1/transcribe": "transcribe_content"}[r.URL.Path]
		if (input.AllowCloud || input.Fetch) && !h.require(w, agent.ScopeNetworkFetch) {
			return
		}
		result, err := h.service.StartDerived(ctx, action, input)
		h.respond(w, result, err)
	case "/agent/v1/operation":
		if !h.require(w, agent.ScopeDerived) {
			return
		}
		var input struct {
			ID string `json:"id"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.Operation(ctx, input.ID)
		h.respond(w, result, err)
	case "/agent/v1/operation/cancel":
		if !h.require(w, agent.ScopeDerived) {
			return
		}
		var input struct {
			ID string `json:"id"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		h.respond(w, map[string]bool{"cancelled": true}, h.service.CancelOperation(ctx, input.ID))
	case "/agent/v1/review":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input agent.ReviewInput
		if !h.decode(w, r, &input, 16<<10) {
			return
		}
		result, duplicate, err := h.service.RecordReview(ctx, input)
		h.respond(w, map[string]any{"event": result, "duplicate": duplicate}, err)
	case "/agent/v1/reviews":
		if !h.require(w, agent.ScopeContentRead) {
			return
		}
		var input struct {
			ContentID string `json:"contentId"`
			Limit     int    `json:"limit"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		if input.Limit == 0 {
			input.Limit = 40
		}
		result, err := h.service.Reviews(ctx, input.ContentID, input.Limit)
		h.respond(w, map[string]any{"events": result}, err)
	case "/agent/v1/report":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input agent.ReportInput
		if !h.decode(w, r, &input, 256<<10) {
			return
		}
		result, err := h.service.SaveReport(ctx, input)
		h.respond(w, result, err)
	case "/agent/v1/digest":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input agent.DigestInput
		if !h.decode(w, r, &input, 512<<10) {
			return
		}
		result, err := h.service.SaveDigest(ctx, input)
		h.respond(w, result, err)
	case "/agent/v1/reports":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input agent.ReportQuery
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.Reports(ctx, input)
		h.respond(w, map[string]any{"reports": result}, err)
	case "/agent/v1/note-draft":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input agent.NoteDraftInput
		if !h.decode(w, r, &input, 16<<10) {
			return
		}
		result, err := h.service.SaveNoteDraft(ctx, input)
		h.respond(w, result, err)
	case "/agent/v1/run":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input struct {
			Client  string `json:"client"`
			Trigger string `json:"trigger"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		result, err := h.service.CreateRun(ctx, input.Client, input.Trigger)
		h.respond(w, result, err)
	case "/agent/v1/run/finish":
		if !h.require(w, agent.ScopeAgentWrite) {
			return
		}
		var input struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Error string `json:"error"`
		}
		if !h.decode(w, r, &input, 4096) {
			return
		}
		h.respond(w, map[string]bool{"finished": true}, h.service.FinishRun(ctx, input.ID, input.State, input.Error))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": "Agent 路径不存在。"})
	}
}

func idempotentRunID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '.' && char != '_' && char != ':' && char != '-' {
			return false
		}
	}
	return true
}

func (h *handler) publishImages(images []agent.AgentImage) []agent.AgentImage {
	now := time.Now()
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	for key, image := range h.images {
		if !now.Before(image.expiresAt) {
			h.imageBytes -= int64(len(image.data))
			delete(h.images, key)
		}
	}
	result := make([]agent.AgentImage, 0, len(images))
	for _, image := range images {
		if len(image.Data) == 0 || len(image.Data) > maxImageBytes || !isImageMIMEType(image.MIMEType) {
			continue
		}
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			continue
		}
		key := hex.EncodeToString(raw[:])
		for len(h.images) >= maxImagePoolItems || h.imageBytes+int64(len(image.Data)) > maxImagePoolBytes {
			h.evictOldestImage()
		}
		expiresAt := now.Add(imageResourceTTL)
		h.images[key] = imageResource{data: append([]byte(nil), image.Data...), mimeType: image.MIMEType, expiresAt: expiresAt}
		h.imageBytes += int64(len(image.Data))
		image.Data = nil
		image.ResourceURL = "http://" + h.host + "/agent/v1/frame/" + key
		image.ResourceExpiresAt = expiresAt.Unix()
		result = append(result, image)
	}
	return result
}

func (h *handler) evictOldestImage() {
	var oldestKey string
	var oldestExpiry time.Time
	for key, image := range h.images {
		if oldestKey == "" || image.expiresAt.Before(oldestExpiry) {
			oldestKey, oldestExpiry = key, image.expiresAt
		}
	}
	if oldestKey == "" {
		return
	}
	h.imageBytes -= int64(len(h.images[oldestKey].data))
	delete(h.images, oldestKey)
}

func (h *handler) serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"code": "method_not_allowed", "message": "请求方法不受支持。"})
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/agent/v1/frame/")
	if len(key) != 32 || r.URL.RawQuery != "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": "图片资源不存在或已过期。"})
		return
	}
	if _, err := hex.DecodeString(key); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": "图片资源不存在或已过期。"})
		return
	}
	h.imageMu.Lock()
	image, ok := h.images[key]
	if ok && !time.Now().Before(image.expiresAt) {
		h.imageBytes -= int64(len(image.data))
		delete(h.images, key)
		ok = false
	}
	h.imageMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": "图片资源不存在或已过期。"})
		return
	}
	w.Header().Set("Content-Type", image.mimeType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(image.data)))
	_, _ = w.Write(image.data)
}

func isImageMIMEType(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

type inboxRequest struct {
	Query    string `json:"query"`
	Platform string `json:"platform"`
	Kind     string `json:"kind"`
	Read     string `json:"read"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
}

func (r inboxRequest) query() subscriptions.InboxQuery {
	if r.Limit == 0 {
		r.Limit = 50
	}
	return subscriptions.InboxQuery{Query: r.Query, Platform: r.Platform, Kind: r.Kind, Read: r.Read, Limit: r.Limit, Cursor: r.Cursor}
}

func (h *handler) require(w http.ResponseWriter, scope string) bool {
	if !h.scopes[scope] {
		writeJSON(w, http.StatusForbidden, map[string]any{"code": "scope_required", "message": "当前连接未启用所需权限。", "scope": scope})
		return false
	}
	return true
}

func (h *handler) decode(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if err := decodeJSON(w, r, target, limit); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_input", "message": "请检查请求 JSON 与参数。"})
		return false
	}
	return true
}

func (h *handler) respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		status := http.StatusBadRequest
		code := "request_failed"
		message := "Agent 请求未能完成。"
		if errors.Is(err, agent.ErrInvalid) {
			code, message = "invalid_input", "请检查请求 JSON 与参数。"
		}
		if errors.Is(err, agent.ErrUnsupported) {
			status, code, message = http.StatusNotImplemented, "unsupported", "当前能力不可用。"
		}
		if errors.Is(err, agent.ErrConflict) {
			code, message = "conflict", "该 Agent 记录已存在。"
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status, code, message = http.StatusGatewayTimeout, "cancelled_or_timed_out", "操作已取消或超时。"
		}
		writeJSON(w, status, map[string]string{"code": code, "message": message})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
