package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/transcription"
)

func TestSpeechRetryHintParsesOnlyBoundedDurations(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, sample := range []struct{ header, want string }{
		{"30", "平台建议至少等待 30 秒。"}, {"61", "平台建议至少等待 2 分钟。"},
		{"3601", "平台建议至少等待 2 小时。"},
		{now.Add(90 * time.Second).Format(http.TimeFormat), "平台建议至少等待 2 分钟。"},
		{"", ""}, {"0", ""}, {"-1", ""}, {"99999999999999999999", ""},
		{"604801", ""}, {"private-token", ""}, {strings.Repeat("a", 129), ""},
		{now.Add(-time.Minute).Format(http.TimeFormat), ""},
	} {
		if got := speechRetryHint(sample.header, now); got != sample.want {
			t.Errorf("header %q: got %q, want %q", sample.header, got, sample.want)
		}
	}
}

func TestMAIRateLimitReportsStageWithoutRetryingOrLeaking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		path, stage string
		count       int
	}{
		{"/api/trpc/conversations.create", "建立会话", 1},
		{"/api/trpc/conversations.addAudioPromptAndInitResponse", "上传片段", 2},
		{"/api/chat/stream", "读取转录结果", 3},
	} {
		t.Run(target.stage, func(t *testing.T) {
			calls, cleanup := 0, 0
			engine := &Transcriber{CloudHTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/trpc/conversations.delete" {
					cleanup++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
				}
				calls++
				if r.URL.Path == target.path {
					return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader("private-session-data"))}, nil
				}
				body := `[{"result":{"data":{"conversation":{"id":"fixture-conversation","title":"fixture"}}}}]`
				if r.URL.Path == "/api/trpc/conversations.addAudioPromptAndInitResponse" {
					body = `[{"result":{"data":{"modelMessage":{"id":"fixture-model"}}}}]`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			_, err := engine.maiRequest(context.Background(), transcription.Config{MAICookie: "session=private"}, path)
			if err == nil || !strings.Contains(err.Error(), target.stage) || !strings.Contains(err.Error(), "2 分钟") || strings.Contains(err.Error(), "private") || calls != target.count {
				t.Fatal(err, calls)
			}
			wantCleanup := 1
			if target.count == 1 {
				wantCleanup = 0
			}
			if cleanup != wantCleanup {
				t.Fatal("cleanup count", cleanup)
			}
		})
	}
}
