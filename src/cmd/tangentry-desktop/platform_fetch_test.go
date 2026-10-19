package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"tangentry/internal/app"
	"tangentry/internal/platformfetch"
)

func TestPlatformPageWindowPositionUsesAllDisplayBounds(t *testing.T) {
	displays := []mygo.Display{
		{Label: "Built-in", Bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1728, Height: 1117}},
		{Label: "Left", Bounds: mygo.Rectangle{X: -1920, Y: -120, Width: 1920, Height: 1080}},
		{Label: "Above", Bounds: mygo.Rectangle{X: 100, Y: -900, Width: 1200, Height: 900}},
	}
	x, y := platformPageWindowPosition(displays, 1024, 768)
	planned := mygo.Rectangle{X: x, Y: y, Width: 1024, Height: 768}
	for _, display := range displays {
		if rectanglesOverlap(planned, display.Bounds) {
			t.Fatalf("planned bridge window overlaps %s: %+v", display.Label, planned)
		}
	}
	if x > -1920-1024-platformPageWindowGap || y > -900-768-platformPageWindowGap {
		t.Fatalf("position does not clear union bounds: x=%d y=%d", x, y)
	}
}

func TestPlatformPageWindowScreenReportsVisibleAndOffscreen(t *testing.T) {
	displays := []mygo.Display{{Label: "Main", ID: 1, Bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1000, Height: 800}}}
	if got := platformPageWindowScreen(mygo.Rectangle{X: 100, Y: 100, Width: 20, Height: 20}, displays); got != "Main" {
		t.Fatalf("visible screen = %q", got)
	}
	if got := platformPageWindowScreen(mygo.Rectangle{X: -200, Y: -200, Width: 20, Height: 20}, displays); got != "屏幕外" {
		t.Fatalf("offscreen screen = %q", got)
	}
	if !platformPageWindowNeedsFallback(mygo.Rectangle{X: 100, Y: 100, Width: 20, Height: 20}, displays) {
		t.Fatal("visible bridge window did not request concealment fallback")
	}
	if platformPageWindowNeedsFallback(mygo.Rectangle{X: -200, Y: -200, Width: 20, Height: 20}, displays) {
		t.Fatal("offscreen bridge window unexpectedly requested fallback")
	}
}

func TestValidatePlatformPath(t *testing.T) {
	for _, raw := range []string{"/latest.json", "/top.json?page=2", "/aweme/v2/web/module/feed/?refresh_index=1"} {
		if err := validatePlatformPath(raw); err != nil {
			t.Fatalf("valid path %q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "https://linux.do/latest.json", "//evil.example/latest.json", "/../latest.json", "/x/../latest.json", "/x\\evil", "/x\r\n"} {
		if err := validatePlatformPath(raw); err == nil {
			t.Fatalf("invalid path %q accepted", raw)
		}
	}
}

func TestPlatformPageOriginAndCloudflareRecognition(t *testing.T) {
	for _, test := range []struct {
		platform, origin string
	}{
		{"linuxdo", "https://linux.do/"},
		{"douyin", "https://www.douyin.com/"},
		{"xiaohongshu", "https://www.xiaohongshu.com/"},
	} {
		origin, ok := platformPageOrigin(test.platform)
		if !ok || origin != test.origin {
			t.Fatalf("origin(%q) = %q, %v", test.platform, origin, ok)
		}
	}
	if _, ok := platformPageOrigin("other"); ok {
		t.Fatal("unsupported platform accepted")
	}
	for _, body := range []string{"<title>Just a moment...</title>", "cf-chl-abc", "challenge-platform", "Cloudflare"} {
		if !isCloudflarePage(httpStatusOK, body, "text/html; charset=utf-8") {
			t.Fatalf("challenge body not recognized: %q", body)
		}
	}
	if isCloudflarePage(403, "json") || isCloudflarePage(200, `{"ok":true}`) || isCloudflarePage(httpStatusOK, "<title>Just a moment...</title>") {
		t.Fatal("status/body challenge recognition is incorrect")
	}
}

func TestPlatformFetchScriptHonorsMethodBodyAndLimits(t *testing.T) {
	code, err := platformFetchScript("/feed.json", app.PlatformPageRequest{Method: "POST", Headers: map[string]string{"Accept": "application/json"}, Body: "a=1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"/feed.json", "method:\"POST\"", "body:\"a=1\"", "AbortController", "tooLarge", "content-length"} {
		if !strings.Contains(code, part) {
			t.Fatalf("script does not contain %q: %s", part, code)
		}
	}
}

func TestDouyinFeedCaptureScriptHooksPageRequests(t *testing.T) {
	for _, part := range []string{
		"__tangentryDouyinFeed",
		"XMLHttpRequest.prototype.open",
		"XMLHttpRequest.prototype.send",
		"window.fetch",
		"responseType === 'json'",
		"aweme\\/(?:v1|v2)\\/",
	} {
		if !strings.Contains(douyinFeedCaptureScript, part) {
			t.Fatalf("capture script does not contain %q", part)
		}
	}
	for _, part := range []string{"pageUrl", "ready"} {
		if !strings.Contains(douyinFeedScrollScript, part) || !strings.Contains(douyinFeedReadScript, part) {
			t.Fatalf("capture execution scripts do not contain %q", part)
		}
	}
	for _, part := range []string{"window.scrollTo", "splice(0, 32)"} {
		code := douyinFeedScrollScript + douyinFeedReadScript
		if !strings.Contains(code, part) {
			t.Fatalf("capture execution script does not contain %q: %s", part, code)
		}
	}
	if !strings.Contains(douyinDomReadScript, "performance.getEntriesByType('resource')") || !strings.Contains(douyinDomReadScript, "fetchHookInstalled") || !strings.Contains(douyinDomReadScript, `a[href*="/video/"]`) {
		t.Fatal("DOM capture script lacks diagnostics or card selectors")
	}
	if !strings.Contains(douyinRecommendNextScript, "ArrowDown") || !strings.Contains(douyinRecommendNextScript, "WheelEvent") {
		t.Fatal("recommendation fallback does not advance the page")
	}
}

func TestDouyinVideoMediaScriptUsesBoundedPageContextReads(t *testing.T) {
	code := douyinVideoMediaScript("6961737553342991651")
	for _, part := range []string{"credentials:'include'", "getReader()", "reader.cancel()", "tooLarge", "AbortController", "captcha", "风控", "status_msg", "video"} {
		if !strings.Contains(code, part) {
			t.Fatalf("video media script does not contain %q", part)
		}
	}
	if !strings.Contains(code, "!response.body && !length") {
		t.Fatal("video media script does not reject unbounded responses without a body")
	}
}

func TestDouyinDetailFetchOutcomeDistinguishesFailureKinds(t *testing.T) {
	tests := []struct {
		name, contentType, wantOutcome, wantReason string
		status                                     int
		restricted, timedOut, tooLarge             bool
		networkError, jsonInvalid, hasURL          bool
	}{
		{name: "403", status: 403, contentType: "application/json", wantOutcome: "http_error", wantReason: "http_status"},
		{name: "verification", status: 200, contentType: "application/json", restricted: true, wantOutcome: "restricted", wantReason: "business_verification"},
		{name: "non-json", status: 200, contentType: "text/html", wantOutcome: "exception", wantReason: "non_json"},
		{name: "timeout", timedOut: true, wantOutcome: "timeout", wantReason: "timeout"},
		{name: "oversized", tooLarge: true, wantOutcome: "too_large", wantReason: "response_too_large"},
		{name: "network", networkError: true, wantOutcome: "exception", wantReason: "network_error"},
		{name: "invalid json", contentType: "application/json", jsonInvalid: true, wantOutcome: "exception", wantReason: "invalid_json"},
		{name: "no url", contentType: "application/json", wantOutcome: "no_url", wantReason: "media_not_found"},
		{name: "success", status: 200, contentType: "application/json", hasURL: true, wantOutcome: "ok", wantReason: "detail_success"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, reason := douyinDetailFetchOutcome(test.status, test.contentType, test.restricted, test.timedOut, test.tooLarge, test.networkError, test.jsonInvalid, test.hasURL)
			if outcome != test.wantOutcome || reason != test.wantReason {
				t.Fatalf("outcome = %s/%s, want %s/%s", outcome, reason, test.wantOutcome, test.wantReason)
			}
		})
	}
}

func TestSafePageURLStripsQueryAndRejectsUnsafeAuthorities(t *testing.T) {
	for _, raw := range []string{"https://passport.douyin.com/login?next=fixture", "https://user:pass@fixture.example/private", "https://fixture.example:8443/private"} {
		got := safePageURL(raw)
		if raw == "https://passport.douyin.com/login?next=fixture" {
			if got == nil || *got != "passport.douyin.com/login" {
				t.Fatalf("safe page URL = %v", got)
			}
		} else if got != nil {
			t.Fatalf("unsafe page URL retained: %q", *got)
		}
	}
}

func TestSearchDOMReadScriptMapsAuthorLinks(t *testing.T) {
	for _, part := range []string{"authorIDFrom", "/user\\/profile", "authorID", "cards"} {
		if !strings.Contains(searchDomReadScript, part) {
			t.Fatalf("search DOM script does not contain %q", part)
		}
	}
}

func TestSearchPageURLValidatesPlatformQueryAndCursor(t *testing.T) {
	tests := []struct {
		platform string
		query    string
		cursor   string
		want     string
	}{
		{"douyin", "猫 / 狗", "", "https://www.douyin.com/search/%E7%8C%AB%20%2F%20%E7%8B%97"},
		{"xiaohongshu", "旅行笔记", "2", "https://www.xiaohongshu.com/search_result?keyword=%E6%97%85%E8%A1%8C%E7%AC%94%E8%AE%B0&page=2"},
	}
	for _, test := range tests {
		got, err := searchPageURL(test.platform, test.query, test.cursor)
		if err != nil || got != test.want {
			t.Fatalf("searchPageURL(%q, %q, %q) = %q, %v; want %q", test.platform, test.query, test.cursor, got, err, test.want)
		}
	}
	for _, test := range [][3]string{
		{"", "query", ""}, {"other", "query", ""}, {"douyin", "", ""}, {"douyin", "query", "0"}, {"douyin", "query", "1001"}, {"douyin", strings.Repeat("x", 201), ""}, {"douyin", "query\x00", ""},
	} {
		if got, err := searchPageURL(test[0], test[1], test[2]); err == nil || got != "" {
			t.Fatalf("invalid searchPageURL(%q, %q, %q) = %q, %v", test[0], test[1], test[2], got, err)
		}
	}
}

func TestEvalWithRetryRetriesDestroyedPageContext(t *testing.T) {
	calls := 0
	value, err := evalWithRetry(context.Background(), func(context.Context, string) (any, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("Completion handler for function call is no longer reachable")
		}
		return "ok", nil
	}, "return true")
	if err != nil || value != "ok" || calls != 3 {
		t.Fatalf("retry result=%v err=%v calls=%d", value, err, calls)
	}
}

func TestEvalWithRetryDoesNotRetryUnrelatedErrors(t *testing.T) {
	calls := 0
	_, err := evalWithRetry(context.Background(), func(context.Context, string) (any, error) {
		calls++
		return nil, errors.New("syntax error")
	}, "return true")
	if err == nil || calls != 1 {
		t.Fatalf("unrelated error calls=%d err=%v", calls, err)
	}
}

func TestDecodeDouyinCaptureResultParsesDOMCardsAndResources(t *testing.T) {
	var result douyinCaptureResult
	raw := map[string]any{
		"pageUrl":            "https://www.douyin.com/jingxuan",
		"title":              "精选",
		"ready":              "complete",
		"cards":              []map[string]any{{"id": "6961737553342991651", "title": "测试"}},
		"resources":          []map[string]any{{"path": "/aweme/v1/feed/", "initiatorType": "fetch"}},
		"fetchHookInstalled": true,
		"xhrHookInstalled":   false,
	}
	if err := decodeDouyinCaptureResult(raw, &result); err != nil || len(result.Cards) != 1 || len(result.ResourcePaths) != 1 || result.ResourcePaths[0] != "/aweme/v1/feed/" || !result.FetchHookInstalled || result.XHRHookInstalled {
		t.Fatalf("decoded capture=%+v err=%v", result, err)
	}
}

func TestDecodeDouyinCaptureResultKeepsSafeFeedProjectionMetadata(t *testing.T) {
	var result douyinCaptureResult
	raw := map[string]any{
		"responses": []map[string]any{{"status": 200, "contentType": "application/json", "apiStatus": 0, "parsed": true, "hasMore": true, "cards": []map[string]any{{"id": "6961737553342991651", "title": "页面投影"}}}},
	}
	if err := decodeDouyinCaptureResult(raw, &result); err != nil || !result.FeedParsed || !result.FeedHasMore || len(result.FeedCards) != 1 || result.FeedCards[0].Title != "页面投影" {
		t.Fatalf("feed projection metadata lost: %+v err=%v", result, err)
	}
}

func TestSanitizeDouyinCaptureResponseDropsQueryAndLimitsMetadata(t *testing.T) {
	response := sanitizeDouyinCaptureResponse(douyinCaptureResponse{Status: 200, APIStatus: 2154, Parsed: true, HasMore: true, URL: "https://www.douyin.com/aweme/v2/web/module/feed/?msToken=fixture", ContentType: strings.Repeat("x", 300)})
	if strings.Contains(response.URL, "msToken") || len(response.ContentType) > 256 || response.APIStatus != 2154 || !response.Parsed || !response.HasMore {
		t.Fatalf("capture metadata was not sanitized: %+v", response)
	}
}

func TestDecodeDouyinCaptureResultUsesLatestHookState(t *testing.T) {
	var result douyinCaptureResult
	if err := decodeDouyinCaptureResult(map[string]any{"fetchHookInstalled": true, "xhrHookInstalled": true}, &result); err != nil {
		t.Fatal(err)
	}
	if err := decodeDouyinCaptureResult(map[string]any{"fetchHookInstalled": false, "xhrHookInstalled": false}, &result); err != nil {
		t.Fatal(err)
	}
	if result.FetchHookInstalled || result.XHRHookInstalled {
		t.Fatalf("hook state was not refreshed: %+v", result)
	}
}

func TestMergeDOMCardsDeduplicatesAndCompletesMissingFields(t *testing.T) {
	merged := mergeDOMCards([]platformfetch.DOMCard{{ID: "1", Title: "保留标题"}}, []platformfetch.DOMCard{
		{ID: "1", ThumbnailURL: "https://example.invalid/cover.jpg", Author: "作者", Duration: 10},
		{ID: "2", Title: "新卡片"},
	})
	if len(merged) != 2 || merged[0].Title != "保留标题" || merged[0].Author != "作者" || merged[0].Duration != 10 || merged[1].Title != "新卡片" {
		t.Fatalf("DOM merge failed: %+v", merged)
	}
}

func TestWaitCapturePollHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitCapturePoll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v", err)
	}
}

func TestWaitForDouyinCaptureWaitsForStableCardsAndCancellation(t *testing.T) {
	reads := 0
	started := time.Now()
	changed, err := waitForDouyinCapture(context.Background(), 2*time.Second, douyinCaptureCounts{}, false, func() (douyinCaptureCounts, error) {
		reads++
		if reads < 2 {
			return douyinCaptureCounts{dom: reads}, nil
		}
		return douyinCaptureCounts{dom: 2}, nil
	})
	if err != nil || !changed || reads < 4 || time.Since(started) < douyinCaptureStable {
		t.Fatalf("capture did not wait for stability: changed=%v err=%v reads=%d", changed, err, reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForDouyinCapture(ctx, time.Second, douyinCaptureCounts{}, false, func() (douyinCaptureCounts, error) { return douyinCaptureCounts{}, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture cancellation err=%v", err)
	}
}

func TestPlatformFetchRequestAndResponseLimits(t *testing.T) {
	request := app.PlatformPageRequest{Method: "", Body: strings.Repeat("x", 256<<10)}
	if err := validatePlatformFetchRequest(&request); err != nil || request.Method != "GET" {
		t.Fatalf("default request rejected: %v, method=%q", err, request.Method)
	}
	request = app.PlatformPageRequest{Method: "PUT"}
	if err := validatePlatformFetchRequest(&request); err == nil {
		t.Fatal("unsupported method accepted")
	}
	request = app.PlatformPageRequest{Method: "POST", Body: strings.Repeat("x", 256<<10+1)}
	if err := validatePlatformFetchRequest(&request); err == nil {
		t.Fatal("oversized request accepted")
	}
	if err := validatePlatformFetchResponse(platformFetchResult{TooLarge: true}); err == nil || !strings.Contains(err.Error(), "过大或无法限制大小") {
		t.Fatal("oversized response marker accepted")
	}
	if err := validatePlatformFetchResponse(platformFetchResult{Body: strings.Repeat("x", platformFetchMaxBody+1)}); err == nil {
		t.Fatal("oversized response body accepted")
	}
	for _, request := range []app.PlatformPageRequest{
		{Method: "GET", Headers: map[string]string{"X-Test": "line\nfeed"}},
		{Method: "GET", Headers: map[string]string{"": "value"}},
		{Method: "GET", Headers: map[string]string{"X-Test": strings.Repeat("x", 8193)}},
	} {
		if err := validatePlatformFetchRequest(&request); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
}

func TestPlatformPageURLAllowed(t *testing.T) {
	for _, test := range []struct {
		platform, raw string
		allowed       bool
	}{
		{"linuxdo", "https://linux.do/", true},
		{"linuxdo", "https://www.linux.do/login", false},
		{"linuxdo", "https://challenges.cloudflare.com/", false},
		{"douyin", "https://www.douyin.com/", true},
		{"douyin", "https://douyin.com/", false},
		{"douyin", "https://passport.douyin.com/", false},
		{"douyin", "https://www.douyin.com.evil.example/", false},
		{"xiaohongshu", "https://www.xiaohongshu.com/", true},
		{"xiaohongshu", "https://xiaohongshu.com/", false},
		{"xiaohongshu", "https://edith.xiaohongshu.com/", false},
	} {
		if got := platformPageURLAllowed(test.raw, test.platform); got != test.allowed {
			t.Fatalf("platformPageURLAllowed(%q, %q) = %v, want %v", test.raw, test.platform, got, test.allowed)
		}
	}
}

const httpStatusOK = 200
