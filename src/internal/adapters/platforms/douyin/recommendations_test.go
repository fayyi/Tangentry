package douyin

import (
	"context"
	"embed"
	"errors"
	"io"
	"net/http"
	"strings"
	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
	"testing"
)

// Synthetic, credential-free response fixture for the supported nested shape.
//
//go:embed testdata/recommendations.json
var douyinFixtures embed.FS

func TestRecommendationMappingAndCookieIsolation(t *testing.T) {
	calls := 0
	c := &Client{Cookie: func(raw string) string {
		if raw != "https://www.douyin.com/aweme/v2/web/module/feed/" {
			t.Fatal(raw)
		}
		return "sessionid=fixture"
	}, UserAgent: func(string) string { return "Mozilla/5.0 (fixture)" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Hostname() != "www.douyin.com" || r.Header.Get("Cookie") != "sessionid=fixture" || r.Header.Get("User-Agent") != "Mozilla/5.0 (fixture)" || r.URL.Query().Get("refresh_index") != "2" || r.URL.Query().Get("pull_type") != "2" {
			t.Fatal("invalid request")
		}
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(`{"status_code":0,"has_more":1,"aweme_list":[{"aweme_id":"6961737553342991651","desc":"推荐测试","create_time":123,"author":{"uid":"12345","nickname":"测试作者"},"video":{"duration":12345,"cover":{"url_list":["javascript:bad","https://p3.douyinpic.com/cover.jpg"]}}},{"aweme_id":"6961737553342991651","desc":"重复"},{"aweme_id":"invalid","desc":"错误"}]}`))}, nil
	})}}
	feed, err := c.Recommendations(context.Background(), 2)
	if err != nil || calls != 1 || len(feed.Items) != 1 || !feed.HasMore || feed.Items[0].DurationSeconds != 12 || feed.Items[0].ThumbnailURL != "https://p3.douyinpic.com/cover.jpg" {
		t.Fatal(feed, calls, err)
	}
}

func TestRecommendationRejectionsNeverRetryOrFollowRedirects(t *testing.T) {
	for _, body := range []string{"", "blocked", `<html>login</html>`, `{"status_code":8}`, `{"status_code":0}`, `{"status_code":0,"aweme_list":[{"aweme_id":"bad"}]}`} {
		calls := 0
		c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		_, err := c.Recommendations(context.Background(), 1)
		var platform *discovery.PlatformError
		if !errors.As(err, &platform) || !platform.Restricted || calls != 1 {
			t.Fatal(calls, err)
		}
	}
	calls := 0
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://other.example/"}}, Request: r, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	if _, err := c.Recommendations(context.Background(), 1); err == nil || calls != 1 {
		t.Fatal(calls, err)
	}
	c.Cookie = nil
	if _, err := c.Recommendations(context.Background(), 1); err == nil || calls != 1 {
		t.Fatal("anonymous request attempted", err)
	}
}

func TestRecommendationCloudflareChallengeRequestsRevalidation(t *testing.T) {
	for _, body := range []string{
		`<!doctype html><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1"></script>`,
		`<html><body>cf-chl-xxxxxxxx cloudflare</body></html>`,
	} {
		c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		_, err := c.Recommendations(context.Background(), 1)
		var platform *discovery.PlatformError
		if !errors.As(err, &platform) || !platform.Restricted || !strings.Contains(platform.Message, "重新导入浏览器 Cookie") {
			t.Fatalf("challenge was not mapped to revalidation: %v", err)
		}
	}
}

func TestRecommendationRateLimitOffersInAppVerification(t *testing.T) {
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Request: r, Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
	})}}
	_, err := c.Recommendations(context.Background(), 1)
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || !platform.Restricted || !strings.Contains(platform.Message, "应用内") || !strings.Contains(platform.Message, "重新验证") {
		t.Fatalf("rate limit did not offer actionable verification: %v", err)
	}
}

func TestRecommendationDiagnosticsRedactTokensAndDescribeResponse(t *testing.T) {
	body := `{"message":"captcha token abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"}`
	diagnostic := describePageResponse(http.StatusOK, body, false, "text/html", "https://www.douyin.com/aweme/v2/web/module/feed/?msToken=secret")
	if !strings.Contains(diagnostic, "HTTP 200") || !strings.Contains(diagnostic, "text/html") || !strings.Contains(diagnostic, "https://www.douyin.com/aweme/v2/web/module/feed/") {
		t.Fatalf("diagnostic missing context: %s", diagnostic)
	}
	if strings.Contains(diagnostic, "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.Contains(diagnostic, "msToken=secret") {
		t.Fatalf("diagnostic leaked token: %s", diagnostic)
	}
}

func TestRecommendationVerificationResponseIsRestricted(t *testing.T) {
	c := &Client{Page: pageFetcherFunc(func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
		return platformfetch.Response{Status: http.StatusOK, Body: `<html><title>登录</title><body>captcha verify</body></html>`, ContentType: "text/html", URL: "https://passport.douyin.com/login"}, nil
	}), Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(*http.Request) (*http.Response, error) {
		t.Fatal("restricted page response should stop before cookie fallback")
		return nil, nil
	})}}
	_, err := c.Recommendations(context.Background(), 1)
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || !platform.Restricted || !strings.Contains(platform.Message, "需要在应用内完成抖音验证") {
		t.Fatalf("verification response was not mapped to restricted: %v", err)
	}
}

func TestRecommendationCaptureLoginPageIncludesFinalLocation(t *testing.T) {
	c := &Client{
		Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
			return platformfetch.FeedCaptureResponse{PageURL: "https://passport.douyin.com/login?next=feed", Title: "登录抖音", Ready: "complete"}, nil
		}),
		Cookie: func(string) string { t.Fatal("login page should stop before cookie fallback"); return "" },
	}
	_, err := c.Recommendations(context.Background(), 1)
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || !platform.Restricted || !strings.Contains(platform.Message, "passport.douyin.com/login") || !strings.Contains(platform.Message, "登录抖音") || !strings.Contains(platform.Message, "需要在应用内完成抖音验证") {
		t.Fatalf("login page diagnostics missing: %v", err)
	}
}

func TestMapDOMCardsDeduplicatesAndMapsSafeFields(t *testing.T) {
	feed := mapDOMCards([]platformfetch.DOMCard{
		{ID: "6961737553342991651", URL: "https://www.douyin.com/video/6961737553342991651", Title: "精选一", Author: "作者", ThumbnailURL: "https://p3.douyinpic.com/a.jpg", LikeCount: 12, Duration: 31},
		{ID: "6961737553342991651", Title: "重复"},
		{ID: "bad", Title: "无效"},
		{ID: "6961737553342991652", Title: "精选二", ThumbnailURL: "javascript:bad", LikeCount: -1},
	}, 1)
	if len(feed.Items) != 2 || feed.Items[0].ID != "douyin:6961737553342991651" || feed.Items[0].LikeCount != 12 || feed.Items[0].DurationSeconds != 31 || feed.Items[1].ThumbnailURL != "" || feed.Items[1].LikeCount != 0 {
		t.Fatalf("DOM cards not mapped safely: %+v", feed)
	}
}

func TestRecommendationMappingUsesAvailableHTTPSCoverVariants(t *testing.T) {
	body := `{"status_code":0,"aweme_list":[{"aweme_id":"6961737553342991651","desc":"动态封面","video":{"duration":1000,"cover":{"url_list":["http://insecure.example/cover.jpg"]},"origin_cover":{"urlList":["https://p3.douyinpic.com/origin.jpg?sign=fixture"]}}},{"aweme_id":"6961737553342991652","desc":"不安全封面","video":{"cover":{"url_list":["javascript:bad","https://user:pass@p3.douyinpic.com/bad.jpg"]}}}]}`
	feed, err := decodeRecommendations([]byte(body), 1, nil)
	if err != nil || len(feed.Items) != 2 {
		t.Fatalf("valid feed did not map: %+v %v", feed, err)
	}
	if got := feed.Items[0].ThumbnailURL; got != "https://p3.douyinpic.com/origin.jpg?sign=fixture" {
		t.Fatalf("available secure cover variant lost: %q", got)
	}
	if got := feed.Items[1].ThumbnailURL; got != "" {
		t.Fatalf("unsafe cover URL was retained: %q", got)
	}
}

func TestRecommendationCaptureUsesPageDecodedFeedProjection(t *testing.T) {
	c := &Client{Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
		return platformfetch.FeedCaptureResponse{
			Responses:   []platformfetch.FeedResponse{{Status: http.StatusOK, ContentType: "application/x-protobuf; charset=utf-8", URL: "https://www.douyin.com/aweme/v2/web/module/feed/"}},
			FeedCards:   []platformfetch.DOMCard{{ID: "6961737553342991651", Title: "页面已解码", Author: "作者", Duration: 12}},
			FeedHasMore: true,
		}, nil
	}), Cookie: func(string) string { t.Fatal("cookie fallback should not run"); return "" }}
	feed, err := c.Recommendations(context.Background(), 1)
	if err != nil || len(feed.Items) != 1 || !feed.HasMore || feed.Items[0].Title != "页面已解码" {
		t.Fatalf("decoded projection was not used: %+v %v", feed, err)
	}
}

func TestBinaryCaptureDiagnosticsDoNotIncludeBody(t *testing.T) {
	c := &Client{Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
		return platformfetch.FeedCaptureResponse{Responses: []platformfetch.FeedResponse{{Status: http.StatusOK, ContentType: "application/x-protobuf", URL: "https://www.douyin.com/aweme/v2/web/module/feed/"}}}, nil
	}), Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Request: r, Body: io.NopCloser(strings.NewReader(`{"status_code":2154,"aweme_list":[],"has_more":1}`))}, nil
	})}}
	_, err := c.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "暂不支持的格式") || strings.Contains(err.Error(), "binary-secret-token") || strings.Contains(err.Error(), "R$") || strings.Contains(err.Error(), "未找到抖音登录 Cookie") {
		t.Fatalf("binary body leaked in diagnostics: %v", err)
	}
}

func TestBinaryCaptureWithoutCookieReportsUnsupportedFormat(t *testing.T) {
	c := &Client{Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
		return platformfetch.FeedCaptureResponse{Responses: []platformfetch.FeedResponse{{Status: http.StatusOK, ContentType: "application/x-protobuf", URL: "https://www.douyin.com/aweme/v2/web/module/feed/"}}}, nil
	})}
	_, err := c.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "暂不支持的格式") || strings.Contains(err.Error(), "未找到抖音登录 Cookie") {
		t.Fatalf("binary page was misclassified without cookie: %v", err)
	}
}

func TestPageDecodedEmptyFeedExplainsNoRecommendations(t *testing.T) {
	c := &Client{Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
		return platformfetch.FeedCaptureResponse{
			Responses:  []platformfetch.FeedResponse{{Status: http.StatusOK, APIStatus: 2154, Parsed: true, ContentType: "application/json", URL: "https://www.douyin.com/aweme/v2/web/module/feed/"}},
			FeedParsed: true,
		}, nil
	}), Cookie: func(string) string { t.Fatal("cookie fallback should not run"); return "" }}
	_, err := c.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "2154") || strings.Contains(err.Error(), "Cookie") {
		t.Fatalf("empty page feed was not classified safely: %v", err)
	}
}

func TestCookieBinaryResponseIsSkippedWithMetadataOnlyDiagnostic(t *testing.T) {
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Request: r, Header: http.Header{"Content-Type": {"application/x-protobuf; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader("R$binary-secret-token"))}, nil
	})}}
	_, err := c.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "非 JSON") || strings.Contains(err.Error(), "binary-secret-token") || strings.Contains(err.Error(), "R$") {
		t.Fatalf("cookie binary response was not safely diagnosed: %v", err)
	}
}

func TestCaptureDiagnosticsIncludeResourcesAndHookStateWithoutQuerySecrets(t *testing.T) {
	diagnostic := describeCaptureResources([]string{"/aweme/v1/feed/", "/aweme/v2/module/?token=abcdefghijklmnopqrstuvwxyz0123456789"}, []string{"fetch", "xmlhttprequest"}, true, false)
	if !strings.Contains(diagnostic, "/aweme/v1/feed/(fetch)") || !strings.Contains(diagnostic, "fetch 钩子=true") || !strings.Contains(diagnostic, "XHR 钩子=false") || strings.Contains(diagnostic, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("capture diagnostic invalid or leaked token: %s", diagnostic)
	}
}

func TestVerificationURLClassification(t *testing.T) {
	for _, raw := range []string{
		"https://www.douyin.com/passport/login?next=%2Fjingxuan",
		"https://www.douyin.com/verify/slide",
		"https://www.douyin.com/?byted_acrawler=1",
	} {
		if !looksLikeVerificationURL(raw) {
			t.Fatalf("verification URL not recognized: %s", raw)
		}
	}
	if looksLikeVerificationURL("https://www.douyin.com/jingxuan") {
		t.Fatal("normal recommendation URL classified as verification")
	}
}

func TestRecommendationCaptureResponseDiagnosticsIncludeHTTPContext(t *testing.T) {
	c := &Client{
		Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
			return platformfetch.FeedCaptureResponse{
				PageURL: "https://www.douyin.com/jingxuan",
				Title:   "精选",
				Ready:   "complete",
				Responses: []platformfetch.FeedResponse{{
					Status:      http.StatusForbidden,
					URL:         "https://www.douyin.com/aweme/v1/feed/?msToken=secret",
					ContentType: "text/html",
				}},
			}, nil
		}),
		Cookie: nil,
	}
	_, err := c.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "Content-Type") || !strings.Contains(err.Error(), "text/html") || strings.Contains(err.Error(), "msToken=secret") || strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("capture response diagnostics incomplete or leaked: %v", err)
	}
}

func TestRecommendationNestedPayloadAndAuthorAvatar(t *testing.T) {
	raw, err := douyinFixtures.ReadFile("testdata/recommendations.json")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}}
	feed, err := c.Recommendations(context.Background(), 1)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].LikeCount != 12 || feed.Items[0].AuthorAvatarURL == "" {
		t.Fatalf("nested response not mapped: %+v %v", feed, err)
	}
}

func TestResolveVideoMediaFiltersRestrictedAndExpiredCandidates(t *testing.T) {
	for name, response := range map[string]platformfetch.VideoMediaResponse{
		"restricted": {Restricted: true, URL: "https://fixture.douyinvod.com/video.mp4"},
		"expired":    {URL: "https://fixture.douyinvod.com/video.mp4?deadline=1"},
		"unsafe":     {URL: "https://evil.example/video.mp4"},
	} {
		t.Run(name, func(t *testing.T) {
			client := &Client{Page: videoMediaPage{resolve: func(context.Context, string) (platformfetch.VideoMediaResponse, error) {
				return response, nil
			}}}
			_, err := client.ResolveVideoMedia(context.Background(), "6961737553342991651")
			if err == nil {
				t.Fatal("unsafe media response accepted")
			}
			var restricted *discovery.PlatformError
			if name == "restricted" && (!errors.As(err, &restricted) || !restricted.Restricted) {
				t.Fatalf("restricted response lost classification: %v", err)
			}
		})
	}
}

func TestPlaybackURLAcceptsSharedCDNsAndUpgradesHTTP(t *testing.T) {
	if !PlaybackURL("http://v26-web.douyinvod.com/video.mp4") || !PlaybackURL("https://media.zjcdn.com/video.mp4") || !PlaybackURL("https://media.douyinvod.net/video.mp4") {
		t.Fatal("shared Douyin CDN URL was rejected")
	}
	if PlaybackURL("https://media.example.com/video.mp4") || PlaybackURL("https://media.zjcdn.com/video.mp4?x-expires=1") {
		t.Fatal("unsafe or expired URL was accepted")
	}
}

func TestCaptureBusinessStatusRejectsVerificationResponse(t *testing.T) {
	page := captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
		return platformfetch.FeedCaptureResponse{FeedParsed: true, Responses: []platformfetch.FeedResponse{{Status: http.StatusOK, APIStatus: 401, Parsed: true, ContentType: "application/json", URL: "https://www.douyin.com/aweme/v2/web/module/feed/"}}}, nil
	})
	client := &Client{Page: page}
	_, err := client.Recommendations(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "验证") {
		t.Fatalf("business verification response error = %v", err)
	}
}

type videoMediaPage struct {
	resolve func(context.Context, string) (platformfetch.VideoMediaResponse, error)
}

func (f videoMediaPage) ResolveVideoMedia(ctx context.Context, id string) (platformfetch.VideoMediaResponse, error) {
	return f.resolve(ctx, id)
}
func (videoMediaPage) Fetch(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
	return platformfetch.Response{}, errors.New("unused")
}

func TestRecommendationUsesPageBridgeBeforeCookieFallback(t *testing.T) {
	calls := 0
	c := &Client{
		Page: pageFetcherFunc(func(_ context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
			calls++
			if platform != "douyin" || !strings.HasPrefix(path, "/aweme/v2/web/module/feed/?") || request.Method != http.MethodPost || request.Body != "encoded_pre_item_ids=" {
				t.Fatalf("unexpected page request: %s %s %+v", platform, path, request)
			}
			return platformfetch.Response{Status: http.StatusOK, Body: `{"status_code":0,"has_more":0,"aweme_list":[{"aweme_id":"6961737553342991651","desc":"页面推荐","video":{"duration":1000}}]}`}, nil
		}),
		Cookie: func(string) string { return "sessionid=fallback" },
		HTTP: &http.Client{Transport: trip(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP fallback used despite page bridge")
			return nil, nil
		})},
	}
	feed, err := c.Recommendations(context.Background(), 1)
	if err != nil || calls != 1 || len(feed.Items) != 1 {
		t.Fatalf("page bridge recommendation failed: %+v calls=%d err=%v", feed, calls, err)
	}
}

func TestRecommendationCaptureUsesPageOwnedFeedAndReportsRedirect(t *testing.T) {
	c := &Client{
		Page: captureFetcherFunc(func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error) {
			return platformfetch.FeedCaptureResponse{
				PageURL: "https://passport.douyin.com/login",
				Ready:   "complete",
				Responses: []platformfetch.FeedResponse{{
					Status: http.StatusOK,
					URL:    "https://www.douyin.com/aweme/v1/web/tab/feed/",
					Parsed: true,
				}},
				FeedCards: []platformfetch.DOMCard{{ID: "6961737553342991651", Title: "页面自带推荐", Duration: 2}},
			}, nil
		}),
		Cookie: func(string) string { t.Fatal("cookie fallback should not run"); return "" },
	}
	feed, err := c.Recommendations(context.Background(), 1)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Title != "页面自带推荐" {
		t.Fatalf("captured feed was not used: %+v %v", feed, err)
	}
}

func TestRecommendationCaptureMergesResponsesAndCapsBridgePages(t *testing.T) {
	var capturedPages int
	c := &Client{
		Page: captureFetcherFunc(func(_ context.Context, _ string, pages int) (platformfetch.FeedCaptureResponse, error) {
			capturedPages = pages
			return platformfetch.FeedCaptureResponse{Responses: []platformfetch.FeedResponse{
				{Status: http.StatusOK, Parsed: true},
				{Status: http.StatusOK, Parsed: true},
			}, FeedCards: []platformfetch.DOMCard{{ID: "6961737553342991651", Title: "第一条"}, {ID: "6961737553342991651", Title: "重复"}, {ID: "6961737553342991652", Title: "第二条"}}, FeedHasMore: true}, nil
		}),
		Cookie: func(string) string { t.Fatal("cookie fallback should not run"); return "" },
	}
	feed, err := c.Recommendations(context.Background(), 10)
	if err != nil || capturedPages != maxDouyinCapturePages || len(feed.Items) != 2 || !feed.HasMore || feed.Items[0].Title != "第一条" || feed.Items[1].Title != "第二条" {
		t.Fatalf("merged capture failed: pages=%d feed=%+v err=%v", capturedPages, feed, err)
	}
}

func TestRecommendationMobileFallback(t *testing.T) {
	seenHosts := make([]string, 0, 2)
	c := &Client{Cookie: func(string) string { return "sessionid=fixture" }, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/aweme/v2/web/module/feed/" {
			return &http.Response{StatusCode: http.StatusForbidden, Request: r, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		}
		seenHosts = append(seenHosts, r.URL.Hostname())
		if r.URL.Hostname() == "api5-normal-c-hl.amemv.com" {
			return &http.Response{StatusCode: http.StatusForbidden, Request: r, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		}
		if r.URL.Path != "/aweme/v1/feed/" || r.Method != http.MethodGet || !strings.Contains(r.Header.Get("User-Agent"), "Android") {
			t.Fatalf("unexpected mobile fallback request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Request: r, Body: io.NopCloser(strings.NewReader(`{"status_code":0,"has_more":0,"aweme_list":[{"aweme_id":"6961737553342991651","desc":"移动推荐","video":{"duration":3000}}]}`))}, nil
	})}}
	feed, err := c.Recommendations(context.Background(), 1)
	if err != nil || len(feed.Items) != 1 || feed.SourceTitle != "抖音移动端推荐" || len(seenHosts) != 2 || seenHosts[0] != "api5-normal-c-hl.amemv.com" || seenHosts[1] != "aweme.snssdk.com" {
		t.Fatalf("mobile fallback failed: hosts=%v feed=%+v err=%v", seenHosts, feed, err)
	}
}

type pageFetcherFunc func(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error)

func (f pageFetcherFunc) Fetch(ctx context.Context, platform, path string, request platformfetch.Request) (platformfetch.Response, error) {
	return f(ctx, platform, path, request)
}

type captureFetcherFunc func(context.Context, string, int) (platformfetch.FeedCaptureResponse, error)

func (f captureFetcherFunc) CaptureFeed(ctx context.Context, platform string, pages int) (platformfetch.FeedCaptureResponse, error) {
	return f(ctx, platform, pages)
}

func (captureFetcherFunc) Fetch(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
	return platformfetch.Response{}, errors.New("unexpected regular page fetch")
}
