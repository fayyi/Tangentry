package playback

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"tangentry/internal/discovery"
)

func TestDiagnosticSanitizersRemoveSensitiveURLAndStderrData(t *testing.T) {
	page := "https://user:pass@www.douyin.com:443/aweme/v1/web/aweme/detail/?msToken=fixture&x=1#frag"
	if got := SanitizeURL(&page); got != nil {
		t.Fatalf("userinfo URL was retained: %q", *got)
	}
	page = "https://www.douyin.com/aweme/v1/web/aweme/detail/?msToken=fixture&x=1#frag"
	if got := SanitizeURL(&page); got == nil || *got != "www.douyin.com/aweme/v1/web/aweme/detail/" {
		t.Fatalf("sanitized URL = %v", got)
	}
	contentType := "Application/JSON; charset=utf-8"
	if got := SanitizeContentType(&contentType); got == nil || *got != "application/json" {
		t.Fatalf("sanitized content type = %v", got)
	}
	stderr := "WARNING: ignored\nERROR: HTTP 403 https://fixture.example/path?token=secret Cookie: cookie-secret a_bogus=sign-secret Authorization: Bearer bearer-secret abcdefghijklmnopqrstuvwxyzABCDEF 1234567890123456"
	got := SanitizeStderr(&stderr)
	if got == nil || !strings.Contains(*got, "ERROR: HTTP 403 fixture.example/path") {
		t.Fatalf("sanitized stderr = %v", got)
	}
	for _, secret := range []string{"secret", "cookie-secret", "sign-secret", "bearer-secret", "Authorization:", "Bearer", "abcdefghijklmnopqrstuvwxyzABCDEF", "1234567890123456", "?token="} {
		if strings.Contains(*got, secret) {
			t.Fatalf("sanitized stderr leaked %q: %s", secret, *got)
		}
	}
}

func TestDiagnosticURLRejectsUnsafePortsAndOversizedPaths(t *testing.T) {
	for _, raw := range []string{"https://fixture.example:8443/path", "https://fixture.example/" + strings.Repeat("x", 1100)} {
		if got := SanitizeURL(&raw); got != nil {
			t.Fatalf("unsafe URL was retained: %q", *got)
		}
	}
}

func TestDiagnosticURLRedactsSensitivePathSegmentsAndCapsDouyinCDNPath(t *testing.T) {
	page := "https://www.douyin.com/video/7340916925458779430/abcdefghijklmnopqrstuvwxyzABCDEF/detail"
	got := SanitizeURL(&page)
	if got == nil || *got != "www.douyin.com/video/7340916925458779430/<redacted>/detail" {
		t.Fatalf("redacted page path = %v", got)
	}
	cdn := "https://v1.douyinvod.com/video/segment/abcdefghijklmnopqrstuvwxyzABCDEF/signed.mp4?signature=fixture"
	got = SanitizeURL(&cdn)
	if got == nil || *got != "v1.douyinvod.com/video/segment" {
		t.Fatalf("redacted CDN path = %v", got)
	}
}

func TestDiagnosticStageSanitizesAndSerializesNullableMetadata(t *testing.T) {
	page := "https://www.douyin.com/aweme/v1/web/aweme/detail/?a_bogus=fixture"
	typeValue := "application/json; charset=utf-8"
	stage := SanitizeDiagnosticStage(DiagnosticStage{Stage: "webview_detail_fetch", Outcome: OutcomeHTTPError, PageURL: &page, ContentType: &typeValue})
	encoded, err := json.Marshal(stage)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, value := range []string{"www.douyin.com/aweme/v1/web/aweme/detail/", "application/json", `"http_status":null`, `"ytdlp_version":null`, `"ytdlp_exit_code":null`} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %q in %s", value, text)
		}
	}
	if strings.Contains(text, "a_bogus") || strings.Contains(text, "fixture") {
		t.Fatalf("unsafe metadata in %s", text)
	}
}

func TestDiagnosticErrorPreservesErrorChainAndRestrictedClassification(t *testing.T) {
	trace := NewDiagnosticTrace()
	restricted := &discovery.PlatformError{Restricted: true, Message: "fixture"}
	wrapped := WrapDiagnostic(restricted, trace, "抖音视频解析失败")
	var platform *discovery.PlatformError
	if !errors.Is(wrapped, discovery.ErrUnavailable) || !errors.As(wrapped, &platform) || !platform.Restricted || OutcomeForError(wrapped) != OutcomeRestricted {
		t.Fatalf("error chain was not preserved: %v", wrapped)
	}
	if !errors.Is(WrapDiagnostic(context.DeadlineExceeded, trace, "超时"), context.DeadlineExceeded) {
		t.Fatal("deadline classification was lost")
	}
}

func TestDiagnosticOutcomeAndSummaryKeepWebViewAndYTDLPStages(t *testing.T) {
	status, exitCode := 403, 1
	stderr := "ERROR: HTTP 403 https://fixture.example/detail?msToken=private"
	trace := NewDiagnosticTrace()
	trace.Add(DiagnosticStage{Stage: "webview_detail_fetch", Outcome: OutcomeHTTPError, HTTPStatus: &status})
	trace.Add(DiagnosticStage{Stage: "ytdlp", Outcome: OutcomeException, YTDLPExitCode: &exitCode, YTDLPStderrFirstErrorRedacted: &stderr})
	stages := trace.Snapshot().Stages
	if len(stages) != 2 || !strings.Contains(DiagnosticSummary(stages), "HTTP 403") || !strings.Contains(DiagnosticSummary(stages), "yt-dlp 退出码 1") || strings.Contains(DiagnosticSummary(stages), "msToken") {
		t.Fatalf("summary lost safe stages: %#v %s", stages, DiagnosticSummary(stages))
	}
}

func TestFinalOutcomeUsesSuccessfulFallbackInsteadOfEarlierWebViewFailure(t *testing.T) {
	stages := []DiagnosticStage{
		{Stage: "webview_detail_fetch", Outcome: OutcomeHTTPError},
		{Stage: "resolver_fallback", Outcome: OutcomeOK},
		{Stage: "ytdlp", Outcome: OutcomeOK},
		{Stage: "source_validate", Outcome: OutcomeOK},
	}
	if got := FinalOutcome(nil, stages); got != OutcomeOK {
		t.Fatalf("successful fallback final outcome = %s", got)
	}
	stages = append(stages, DiagnosticStage{Stage: "session_media_fetch", Outcome: OutcomeHTTPError})
	if got := FinalOutcome(nil, stages); got != OutcomeHTTPError {
		t.Fatalf("media error final outcome = %s", got)
	}
}

func TestPlaybackOpenErrorOutcomePrefersPlaybackOpenFailure(t *testing.T) {
	stages := []DiagnosticStage{
		{Stage: "webview_media_extract", Outcome: OutcomeNoURL},
		{Stage: "resolver_fallback", Outcome: OutcomeOK},
		{Stage: "ytdlp", Outcome: OutcomeOK},
		{Stage: "source_validate", Outcome: OutcomeOK},
		{Stage: "playback_open", Outcome: OutcomeException, Reason: Reason("concurrency_limit")},
	}
	if got := FinalOutcome(ErrBusy, stages); got != OutcomeException {
		t.Fatalf("busy open final outcome = %s", got)
	}
}

func TestDiagnosticOutcomeMappingCoversEveryAllowedOutcome(t *testing.T) {
	tests := []struct {
		name string
		want DiagnosticOutcome
	}{
		{name: "ok", want: OutcomeOK},
		{name: "restricted", want: OutcomeRestricted},
		{name: "timeout", want: OutcomeTimeout},
		{name: "too_large", want: OutcomeTooLarge},
		{name: "no_url", want: OutcomeNoURL},
		{name: "http_error", want: OutcomeHTTPError},
		{name: "exception", want: OutcomeException},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !validOutcome(DiagnosticOutcome(test.name)) || test.want != DiagnosticOutcome(test.name) {
				t.Fatalf("outcome %q is not allowed", test.name)
			}
		})
	}
}
