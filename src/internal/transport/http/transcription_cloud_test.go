package httptransport

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/filesystem"
	"testing"

	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

type cloudSettingsFixture struct{}

func (cloudSettingsFixture) Status(c transcription.Config) transcription.Status {
	return transcription.Status{Config: c, Ready: true}
}
func (cloudSettingsFixture) Transcribe(context.Context, transcription.Config, transcription.Audio, int, func(string)) (subtitles.Track, error) {
	panic("HTTP settings must not start transcription")
}

func TestTranscriptionCloudHTTPSecretsAndConsent(t *testing.T) {
	service, err := transcription.NewWithStore(filesystem.NewTranscriptionSettings(filepath.Join(t.TempDir(), "settings.json")), transcription.Config{Backend: "whisper.cpp", Binary: "fixture-cli", Model: "fixture.bin", Language: "zh"}, cloudSettingsFixture{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := New(nil, nil, nil, Options{Transcription: service})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := send("PUT", "/api/transcription/settings", `{"backend":"siliconflow","language":"zh","siliconflowApiKey":"explicit-private-key","groqApiKey":"explicit-groq-secret","siliconflowSegmentSeconds":30}`)
	if response.Code != 200 || strings.Contains(response.Body.String(), "explicit-") || !strings.Contains(response.Body.String(), `"siliconflowKeyConfigured":true`) {
		t.Fatal(response.Code, response.Body.String())
	}
	response = send("GET", "/api/transcription/settings", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "explicit-") {
		t.Fatal("GET exposed secret")
	}
	for _, body := range []string{
		`{"contentId":"rss:` + strings.Repeat("a", 64) + `","preview":true}`,
		`{"contentId":"rss:` + strings.Repeat("a", 64) + `","preview":true,"uploadConsent":true,"backend":"groq"}`,
	} {
		if response := send("POST", "/api/transcription/jobs", body); response.Code != 400 {
			t.Fatal("unconsented provider accepted", response.Code)
		}
	}
	for _, body := range []string{
		`{"backend":"groq","language":"zh","groqModel":"unsupported"}`,
		`{"backend":"cloudflare","language":"zh","cloudflareAccountId":"../other-account"}`,
		`{"backend":"siliconflow","language":"zh","siliconflowApiKey":"header\ninjection"}`,
		`{"backend":"siliconflow","language":"zh","siliconflowSegmentSeconds":1}`,
	} {
		if response := send("PUT", "/api/transcription/settings", body); response.Code != 400 {
			t.Fatal("invalid cloud setting accepted", response.Code)
		}
	}
	response = send("PUT", "/api/transcription/settings", `{"backend":"siliconflow","language":"zh","siliconflowApiKey":""}`)
	if response.Code != 200 || strings.Contains(response.Body.String(), `"siliconflowKeyConfigured":true`) || service.Configuration().GroqAPIKey != "explicit-groq-secret" {
		t.Fatal("clear removed unrelated credentials")
	}
}
