package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/agent"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
)

type gatewayProvider struct{}

func (gatewayProvider) Capabilities(context.Context) agent.CapabilityMatrix {
	return agent.CapabilityMatrix{ProtocolVersion: "1"}
}
func (gatewayProvider) Search(context.Context, string, string, string, bool) (agent.SearchResult, error) {
	return agent.SearchResult{}, nil
}
func (gatewayProvider) Content(context.Context, string, bool) (agent.ContentResult, error) {
	return agent.ContentResult{}, nil
}
func (gatewayProvider) Inbox(context.Context, subscriptions.InboxQuery) (agent.InboxResult, error) {
	return agent.InboxResult{}, nil
}
func (gatewayProvider) Subtitles(context.Context, string, string, bool) (agent.SubtitleResult, error) {
	return agent.SubtitleResult{}, nil
}
func (gatewayProvider) Notes(context.Context, string, string) ([]notes.Note, error) { return nil, nil }
func (gatewayProvider) Social(context.Context, string) (agent.SocialResult, error) {
	return agent.SocialResult{}, nil
}
func (gatewayProvider) CaptureFrames(context.Context, string, []float64) ([]agent.AgentImage, error) {
	return nil, nil
}

type gatewayRepo struct{}

func (gatewayRepo) CreateRun(context.Context, agent.Run) error              { return nil }
func (gatewayRepo) FinishRun(context.Context, string, string, string) error { return nil }
func (gatewayRepo) RecordReview(context.Context, agent.ReviewEvent) (agent.ReviewEvent, bool, error) {
	return agent.ReviewEvent{}, false, nil
}
func (gatewayRepo) AppendReport(context.Context, agent.Report) (agent.Report, error) {
	return agent.Report{}, nil
}
func (gatewayRepo) SaveNoteDraft(context.Context, agent.NoteDraft) (agent.NoteDraft, error) {
	return agent.NoteDraft{}, nil
}
func (gatewayRepo) RecordAudit(context.Context, agent.AuditEvent) error { return nil }
func (gatewayRepo) ListReviews(context.Context, string, int) ([]agent.ReviewEvent, error) {
	return nil, nil
}

func TestGatewayRequiresBearerAndScope(t *testing.T) {
	root := t.TempDir()
	g, err := StartWithScopes(agent.New(gatewayProvider{}, gatewayRepo{}), root, []string{agent.ScopeContentRead})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	connection := g.Connection()
	if info, err := os.Stat(filepath.Join(root, "agent", "connection.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("connection file mode: %v %v", info, err)
	}
	request := httptest.NewRequest(http.MethodGet, connection.Endpoint+"/agent/v1/capabilities", nil)
	request.Host = request.URL.Host
	response := httptest.NewRecorder()
	// Host is checked before bearer; both are intentional denial paths.
	NewHandler(agent.New(gatewayProvider{}, gatewayRepo{}), connection.Token, connection.Scopes, request.Host, connection.ExpiresAt).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", response.Code)
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response = httptest.NewRecorder()
	NewHandler(agent.New(gatewayProvider{}, gatewayRepo{}), connection.Token, connection.Scopes, request.Host, connection.ExpiresAt).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized status = %d", response.Code)
	}
	if _, err := ReadConnection(filepath.Join(root, "agent", "connection.json")); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayCloseRevokesTokenBeforeShutdown(t *testing.T) {
	service := agent.New(gatewayProvider{}, gatewayRepo{})
	root := t.TempDir()
	g, err := StartWithScopes(service, root, []string{agent.ScopeContentRead})
	if err != nil {
		t.Fatal(err)
	}
	connection := g.Connection()
	oldHandler := g.handler
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "connection.json")); !os.IsNotExist(err) {
		t.Fatalf("connection file was not removed: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, connection.Endpoint+"/agent/v1/capabilities", nil)
	request.Host = request.URL.Host
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	response := httptest.NewRecorder()
	oldHandler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("revoked handler status = %d", response.Code)
	}
}

func TestGatewayRespondsWithStableErrorEnvelope(t *testing.T) {
	service := agent.New(gatewayProvider{}, gatewayRepo{})
	handler := NewHandler(service, "token", []string{agent.ScopeContentRead}, "127.0.0.1:1234", time.Now().Add(time.Hour).Unix())
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234/agent/v1/not-a-route", nil)
	request.Host = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Header().Get("Content-Type"), "application/json") || !strings.Contains(response.Body.String(), `"code":"not_found"`) {
		t.Fatalf("stable error envelope = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestGatewayRejectsForeignHostOriginAndExpiredToken(t *testing.T) {
	service := agent.New(gatewayProvider{}, gatewayRepo{})
	validUntil := time.Now().Add(time.Hour).Unix()
	handler := NewHandler(service, "token", []string{agent.ScopeContentRead}, "127.0.0.1:1234", validUntil)

	foreignHost := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234/agent/v1/capabilities", nil)
	foreignHost.Host = "192.168.1.20:1234"
	foreignHost.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, foreignHost)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", response.Code)
	}

	foreignOrigin := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234/agent/v1/capabilities", nil)
	foreignOrigin.Host = "127.0.0.1:1234"
	foreignOrigin.Header.Set("Origin", "https://evil.example")
	foreignOrigin.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, foreignOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status = %d", response.Code)
	}

	expired := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1234/agent/v1/capabilities", nil)
	expired.Host = "127.0.0.1:1234"
	expired.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	NewHandler(service, "token", []string{agent.ScopeContentRead}, expired.Host, time.Now().Add(-time.Second).Unix()).ServeHTTP(response, expired)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expired token status = %d", response.Code)
	}
}

func TestGatewayDerivedFetchRequiresNetworkScope(t *testing.T) {
	service := agent.New(gatewayProvider{}, gatewayRepo{})
	handler := NewHandler(service, "token", []string{agent.ScopeDerived}, "example.test", time.Now().Add(time.Hour).Unix())
	body, _ := json.Marshal(map[string]any{"contentId": "youtube:abcdefghijk", "fetch": true})
	request := httptest.NewRequest(http.MethodPost, "http://example.test/agent/v1/summarize", bytes.NewReader(body))
	request.Host = "example.test"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("fetch without network scope status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPublishedFrameRequiresImageScopeAndExpires(t *testing.T) {
	service := agent.New(gatewayProvider{}, gatewayRepo{})
	h := NewHandler(service, "token", []string{agent.ScopeImageRead}, "127.0.0.1:1234", time.Now().Add(time.Hour).Unix()).(*handler)
	frames := h.publishImages([]agent.AgentImage{{Data: []byte("jpeg"), MIMEType: "image/jpeg", Position: 1.25}})
	if len(frames) != 1 || frames[0].Data != nil || frames[0].ResourceURL == "" || frames[0].ResourceExpiresAt <= time.Now().Unix() {
		t.Fatalf("published frame = %+v", frames)
	}

	request := httptest.NewRequest(http.MethodGet, frames[0].ResourceURL, nil)
	request.Host = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	NewHandler(service, "token", nil, request.Host, time.Now().Add(time.Hour).Unix()).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("frame without image scope status = %d", response.Code)
	}

	request.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "jpeg" || response.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("authorized frame response = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, frames[0].ResourceURL+"?token=ignored", nil)
	request.Host = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("frame URL with query status = %d", response.Code)
	}

	key := strings.TrimPrefix(frames[0].ResourceURL, "http://127.0.0.1:1234/agent/v1/frame/")
	h.imageMu.Lock()
	resource := h.images[key]
	resource.expiresAt = time.Now().Add(-time.Second)
	h.images[key] = resource
	h.imageMu.Unlock()
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expired frame status = %d", response.Code)
	}
}

func TestPublishImagesRejectsUnsupportedAndOversizedResources(t *testing.T) {
	h := NewHandler(agent.New(gatewayProvider{}, gatewayRepo{}), "token", []string{agent.ScopeImageRead}, "127.0.0.1:1234", time.Now().Add(time.Hour).Unix()).(*handler)
	frames := h.publishImages([]agent.AgentImage{
		{Data: []byte("text"), MIMEType: "text/html"},
		{Data: bytes.Repeat([]byte("x"), maxImageBytes+1), MIMEType: "image/jpeg"},
	})
	if len(frames) != 0 || len(h.images) != 0 {
		t.Fatalf("invalid frames published: %+v", frames)
	}
}

func TestReadConnectionRejectsSymlinkAndOversizedFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte(`{"endpoint":"http://127.0.0.1:1234","token":"`+strings.Repeat("a", 64)+`","version":"1","expiresAt":9999999999}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConnection(link); err == nil {
		t.Fatal("symlink connection file was accepted")
	}
	large := filepath.Join(root, "large")
	if err := os.WriteFile(large, bytes.Repeat([]byte("x"), maxConnectionFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadConnection(large); err == nil {
		t.Fatal("oversized connection file was accepted")
	}
}
