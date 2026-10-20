package douyin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
	"tangentry/internal/playback"
	"time"
)

// The desktop bridge executes this request in douyin.com's page context, so
// the site's own security SDK can sign it. Headless/server builds use the
// explicit Cookie fallback below; signatures are never emulated.
func (c *Client) Recommendations(ctx context.Context, page int) (discovery.Feed, error) {
	if page < 1 || page > 10000 {
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
	if c.HTTP == nil {
		if c.Page == nil {
			return discovery.Feed{}, discovery.ErrUnavailable
		}
	}
	const endpoint = "https://www.douyin.com/aweme/v2/web/module/feed/"
	query := url.Values{"device_platform": {"webapp"}, "aid": {"6383"}, "channel": {"channel_pc_web"}, "module_id": {"3003101"}, "count": {"20"}, "refresh_index": {strconv.Itoa(page)}, "refer_type": {"10"}, "pull_type": {"0"}, "use_lite_type": {"2"}}
	query.Set("update_version_code", "170400")
	query.Set("pc_client_type", "1")
	query.Set("browser_language", "zh-CN")
	query.Set("browser_platform", "MacIntel")
	query.Set("browser_name", "Chrome")
	query.Set("browser_version", "136.0.0.0")
	query.Set("engine_name", "Blink")
	query.Set("engine_version", "136.0.0.0")
	query.Set("os_name", "Mac OS")
	query.Set("os_version", "10.15.7")
	query.Set("platform", "PC")
	if page > 1 {
		query.Set("pull_type", "2")
	}
	diagnostics := make([]string, 0, 4)
	if capture, ok := c.Page.(platformfetch.FeedCaptureFetcher); ok && page <= maxDouyinCapturePages {
		captured, err := capture.CaptureFeed(ctx, "douyin", page)
		if err != nil {
			diagnostics = append(diagnostics, "页面抓取执行失败："+err.Error())
		} else {
			if captured.PageURL != "" && !isDouyinPageURL(captured.PageURL) {
				diagnostics = append(diagnostics, "页面被重定向到 "+safeDiagnosticURL(captured.PageURL))
			}
			if title := strings.TrimSpace(captured.Title); title != "" && looksLikeLoginOrChallenge(title) {
				diagnostics = append(diagnostics, "页面标题显示为“"+redactDiagnostic(title)+"”")
			}
			if feed := mapFeedCards(captured.FeedCards, page); len(feed.Items) > 0 {
				feed.HasMore = captured.FeedHasMore
				return feed, nil
			}
			if feed := mapDOMCards(captured.DOMCards, page); len(feed.Items) > 0 {
				feed.SourceTitle = "抖音精选"
				return feed, nil
			}
			if captured.FeedParsed {
				if status := captureAPIStatus(captured.Responses); status == 2154 {
					return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐（业务状态 2154），请稍后重试。"}
				} else if status != 0 {
					return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: fmt.Sprintf("抖音推荐接口返回业务状态 %d，请在应用内打开抖音完成验证，点击重新验证后重试。", status)}
				}
				return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐，请稍后重试。"}
			}
			captureRestricted := false
			for _, response := range captured.Responses {
				if response.TooLarge {
					return discovery.Feed{}, &discovery.PlatformError{Message: "应用内抖音页面响应过大或无法限制大小，请稍后重试。"}
				}
				if response.Status != http.StatusOK {
					diagnostics = append(diagnostics, "页面 feed 响应不可用："+describeNonJSONPageResponse(response.Status, false, response.ContentType, response.URL))
					captureRestricted = captureRestricted || response.Status == http.StatusForbidden || response.Status == http.StatusTooManyRequests
					continue
				}
				if response.APIStatus != 0 {
					if response.APIStatus == 2154 {
						return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐（业务状态 2154），请稍后重试。"}
					} else {
						diagnostics = append(diagnostics, fmt.Sprintf("页面 feed 返回业务状态 %d，可能需要登录或验证", response.APIStatus))
					}
				}
				if response.HasMore {
					captured.FeedHasMore = true
				}
				if !isJSONContentType(response.ContentType) {
					diagnostics = append(diagnostics, "页面 feed 响应不是 JSON（已安全跳过 protobuf/二进制内容）："+describeNonJSONPageResponse(response.Status, false, response.ContentType, response.URL))
					continue
				}
				if response.Parsed {
					diagnostics = append(diagnostics, "页面 feed 已解析但未形成字段投影")
				}
			}
			if captureRestricted {
				return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: "抖音推荐需要在应用内完成抖音验证或稍后重试：" + joinDiagnostics(diagnostics, "请在应用内打开抖音完成验证，点击重新验证后重试；无 WebView 时请重新导入浏览器 Cookie。")}
			}
			diagnostics = append(diagnostics, describeCapturePage(captured.PageURL, captured.Title, captured.Ready))
			diagnostics = append(diagnostics, describeCaptureResources(captured.ResourcePaths, captured.InitiatorTypes, captured.FetchHookInstalled, captured.XHRHookInstalled))
			diagnostics = append(diagnostics, describeCaptureDOM(captured.DOM))
			if (captured.PageURL != "" && (!isDouyinPageURL(captured.PageURL) || looksLikeVerificationURL(captured.PageURL))) || looksLikeLoginOrChallenge(captured.Title) {
				return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: "抖音推荐需要在应用内完成抖音验证：" + joinDiagnostics(diagnostics, "页面停留在登录或验证页，请在应用内打开抖音完成验证，点击重新验证后重试。")}
			}
			if len(captured.Responses) == 0 && len(captured.FeedCards) == 0 && len(captured.DOMCards) == 0 {
				diagnostics = append(diagnostics, "页面未捕获到推荐 feed 请求（页面状态："+captured.Ready+"）")
			}
		}
	}
	if len(diagnostics) > 0 && hasUnsupportedCapture(diagnostics) {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音页面返回了暂不支持的格式或页面尚未加载出推荐：" + joinDiagnostics(diagnostics, "请稍后重试；若页面持续为空，请在应用内打开抖音完成验证后重试。")}
	}
	if c.Page != nil {
		response, err := c.Page.Fetch(ctx, "douyin", "/aweme/v2/web/module/feed/?"+query.Encode(), platformfetch.Request{
			Method: http.MethodPost,
			Headers: map[string]string{
				"Referer":         "https://www.douyin.com/",
				"Content-Type":    "application/x-www-form-urlencoded",
				"Accept":          "application/json, text/plain, */*",
				"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
			},
			Body: "encoded_pre_item_ids=",
		})
		if err == nil {
			if response.Status == http.StatusOK && !response.Restricted && !looksLikeChallenge([]byte(response.Body)) {
				if !isJSONContentType(response.ContentType) {
					diagnostics = append(diagnostics, "页面 fetch 响应不是 JSON（已安全跳过 protobuf/二进制内容）："+describeNonJSONPageResponse(response.Status, response.Restricted, response.ContentType, response.URL))
				} else if response.Body == "" {
					diagnostics = append(diagnostics, "页面 fetch 响应为空："+describePageResponse(response.Status, "", response.Restricted, response.ContentType, response.URL))
				} else {
					if recommendationAPIStatus([]byte(response.Body)) == 2154 {
						return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐（业务状态 2154），请稍后重试。"}
					}
					feed, decodeErr := decodeRecommendations([]byte(response.Body), page, nil)
					if decodeErr == nil && len(feed.Items) > 0 {
						return feed, nil
					}
					if decodeErr == nil && !feed.HasMore {
						return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐，请稍后重试。"}
					}
					if decodeErr != nil {
						diagnostics = append(diagnostics, "页面 fetch JSON 解析失败："+decodeErr.Error()+"；"+describePageResponse(response.Status, response.Body, response.Restricted, response.ContentType, response.URL))
					}
				}
			} else {
				body := response.Body
				if !isJSONContentType(response.ContentType) {
					body = ""
				}
				diagnostics = append(diagnostics, describePageResponse(response.Status, body, response.Restricted, response.ContentType, response.URL))
				if response.Restricted || looksLikeChallenge([]byte(response.Body)) || looksLikeLoginOrChallenge(response.Body) || response.Status == http.StatusForbidden || response.Status == http.StatusTooManyRequests {
					return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: "抖音推荐需要在应用内完成抖音验证或稍后重试：" + joinDiagnostics(diagnostics, "请在应用内打开抖音完成验证，点击重新验证后重试；无 WebView 时请重新导入浏览器 Cookie。")}
				}
			}
		} else {
			diagnostics = append(diagnostics, "页面 fetch 执行失败："+err.Error())
		}
	}
	if hasUnsupportedCapture(diagnostics) {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音页面返回了暂不支持的格式或页面尚未加载出推荐：" + joinDiagnostics(diagnostics, "请稍后重试；若页面持续为空，请在应用内打开抖音完成验证后重试。")}
	}
	cookie := ""
	if c.Cookie != nil {
		cookie = c.Cookie(endpoint)
	}
	if cookie == "" {
		return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "未找到抖音登录 Cookie；请在应用内打开抖音完成登录或验证后重试。")}
	}
	if c.HTTP == nil {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "当前运行环境没有可用的网络客户端。")}
	}
	query = url.Values{"device_platform": {"webapp"}, "aid": {"6383"}, "channel": {"channel_pc_web"}, "module_id": {"3003101"}, "count": {"20"}, "refresh_index": {strconv.Itoa(page)}, "refer_type": {"10"}, "pull_type": {"0"}, "use_lite_type": {"2"}}
	query.Set("update_version_code", "170400")
	query.Set("pc_client_type", "1")
	query.Set("browser_language", "zh-CN")
	query.Set("browser_platform", "MacIntel")
	query.Set("browser_name", "Chrome")
	query.Set("browser_version", "136.0.0.0")
	query.Set("engine_name", "Blink")
	query.Set("engine_version", "136.0.0.0")
	query.Set("os_name", "Mac OS")
	query.Set("os_version", "10.15.7")
	query.Set("platform", "PC")
	if token := cookieValue(cookie, "msToken"); token != "" {
		query.Set("msToken", token)
	}
	if page > 1 {
		query.Set("pull_type", "2")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader("encoded_pre_item_ids="))
	if err != nil {
		return discovery.Feed{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if c.UserAgent != nil {
		if userAgent := c.UserAgent(endpoint); userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
	}
	req.Header.Set("Referer", "https://www.douyin.com/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://www.douyin.com")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", cookie)
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return discovery.Feed{}, ctx.Err()
		}
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐暂时无法连接，请稍后手动重试。"}
	}
	defer res.Body.Close()
	denied := &discovery.PlatformError{Restricted: true, Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "网页 Cookie 请求未返回可解析的推荐。")}
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
			if feed, mobileErr := c.mobileRecommendations(ctx, page, cookie); mobileErr == nil {
				return feed, nil
			} else {
				diagnostics = append(diagnostics, "移动端免签名 feed 失败："+mobileErr.Error())
			}
			return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "网页请求返回 HTTP "+strconv.Itoa(res.StatusCode)+"，可能触发验证或限流；请在应用内打开抖音完成验证，点击重新验证后重试。"), Restricted: true}
		}
		return discovery.Feed{}, denied
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return discovery.Feed{}, denied
	}
	if looksLikeChallenge(body) {
		if feed, mobileErr := c.mobileRecommendations(ctx, page, cookie); mobileErr == nil {
			return feed, nil
		} else {
			diagnostics = append(diagnostics, "移动端免签名 feed 失败："+mobileErr.Error())
		}
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "返回了网页验证页面；请在应用内打开抖音完成验证，点击重新验证后重试；无 WebView 时请重新导入浏览器 Cookie。"), Restricted: true}
	}
	if status := recommendationAPIStatus(body); status == 2154 {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐（业务状态 2154），请稍后重试。"}
	}
	if !isJSONContentType(res.Header.Get("Content-Type")) {
		diagnostics = append(diagnostics, describeNonJSONPageResponse(res.StatusCode, false, res.Header.Get("Content-Type"), res.Request.URL.String()))
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音推荐接口格式暂不兼容：" + joinDiagnostics(diagnostics, "页面返回非 JSON 数据；请在应用内打开抖音完成验证后重试。")}
	}
	feed, err := decodeRecommendations(body, page, nil)
	if err != nil {
		diagnostics = append(diagnostics, describePageResponse(res.StatusCode, string(body), false, res.Header.Get("Content-Type"), res.Request.URL.String()))
		return discovery.Feed{}, &discovery.PlatformError{Restricted: true, Message: "抖音推荐不可用：" + joinDiagnostics(diagnostics, "网页 Cookie 请求未返回可解析的推荐。")}
	}
	if len(feed.Items) == 0 && !feed.HasMore {
		return discovery.Feed{}, &discovery.PlatformError{Message: "抖音当前暂无推荐，请稍后重试。"}
	}
	return feed, nil
}

// ResolveVideoMedia asks the authenticated WebView to resolve the video in
// page context. It is intentionally called only with a canonical video ID.
func (c *Client) ResolveVideoMedia(ctx context.Context, id string) (platformfetch.VideoMediaResponse, error) {
	trace := playback.DiagnosticsFromContext(ctx)
	ref, err := discovery.ParseDouyin("https://www.douyin.com/video/" + strings.TrimSpace(id))
	if err != nil || ref.ID != id {
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_session", Outcome: "exception", Reason: "invalid_id"})
		return platformfetch.VideoMediaResponse{}, discovery.ErrInvalidQuery
	}
	bridge, ok := c.Page.(platformfetch.VideoMediaFetcher)
	if !ok {
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_session", Outcome: "exception", Reason: "session_destroyed"})
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_page_load", Outcome: "exception", Reason: "page_load_error"})
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_detail_fetch", Outcome: "exception", Reason: "javascript_error"})
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_media_extract", Outcome: "no_url", Reason: "media_not_found"})
		return platformfetch.VideoMediaResponse{}, playback.WrapDiagnostic(&platformfetch.VideoMediaError{Cause: discovery.ErrUnavailable}, trace, "应用内抖音页面不可用。")
	}
	result, err := bridge.ResolveVideoMedia(ctx, id)
	var bridgeError *platformfetch.VideoMediaError
	if errors.As(err, &bridgeError) {
		for _, diagnostic := range bridgeError.Diagnostics {
			recordDouyinDiagnostic(ctx, diagnostic)
		}
	} else {
		for _, diagnostic := range result.Diagnostics {
			recordDouyinDiagnostic(ctx, diagnostic)
		}
	}
	if err != nil {
		var mediaError *platformfetch.VideoMediaError
		if errors.As(err, &mediaError) {
			return platformfetch.VideoMediaResponse{}, playback.WrapDiagnostic(err, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
		}
		last := trace.Snapshot().Stages
		if len(last) == 0 {
			recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_detail_fetch", Outcome: "exception", Reason: "javascript_error"})
		}
		return platformfetch.VideoMediaResponse{}, playback.WrapDiagnostic(&platformfetch.VideoMediaError{Cause: err}, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
	}
	if result.Restricted {
		if !hasDiagnosticStage(result.Diagnostics, "webview_media_extract") {
			recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_media_extract", Outcome: "restricted", Reason: "business_verification"})
		}
		return platformfetch.VideoMediaResponse{}, playback.WrapDiagnostic(&discovery.PlatformError{Restricted: true, Message: "应用内抖音页面需要验证，请在应用内完成登录或验证码验证后重试。"}, trace, "应用内页面要求登录或完成验证。")
	}
	if !hasDiagnosticStage(result.Diagnostics, "webview_media_extract") && result.URL != "" {
		recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_media_extract", Outcome: "ok", Reason: "media_found", CandidateCount: intPtr(1 + len(result.Backups)), AcceptedCount: intPtr(1 + len(result.Backups))})
	}
	candidates := make([]string, 0, 3)
	inputCount := 0
	for _, candidate := range append([]string{result.URL}, result.Backups...) {
		if candidate != "" {
			inputCount++
		}
		if normalized, reason := platformfetch.NormalizeDouyinMediaURL(candidate, time.Now()); reason == "" {
			candidates = append(candidates, normalized)
			if len(candidates) == 3 {
				break
			}
		}
	}
	if len(candidates) == 0 {
		reason := "media_not_found"
		if inputCount > 0 {
			reason = "candidate_filtered"
		}
		if !hasDiagnosticStage(result.Diagnostics, "webview_media_extract") {
			recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_media_extract", Outcome: "no_url", Reason: reason, CandidateCount: intPtr(inputCount), AcceptedCount: intPtr(0)})
		}
		return platformfetch.VideoMediaResponse{}, playback.WrapDiagnostic(discovery.ErrUnavailable, trace, playback.DiagnosticSummary(trace.Snapshot().Stages))
	}
	recordDouyinDiagnostic(ctx, platformfetch.VideoMediaDiagnostic{Stage: "webview_media_extract", Outcome: "ok", Reason: "media_found", CandidateCount: intPtr(inputCount), AcceptedCount: intPtr(len(candidates))})
	result.URL, result.Backups = candidates[0], candidates[1:]
	return result, nil
}

func hasDiagnosticStage(values []platformfetch.VideoMediaDiagnostic, target string) bool {
	for _, value := range values {
		if value.Stage == target {
			return true
		}
	}
	return false
}

func recordDouyinDiagnostic(ctx context.Context, diagnostic platformfetch.VideoMediaDiagnostic) {
	outcome := playback.DiagnosticOutcome(diagnostic.Outcome)
	stage := playback.DiagnosticStage{Stage: diagnostic.Stage, Outcome: outcome, Reason: stringPtr(diagnostic.Reason), HTTPStatus: diagnostic.HTTPStatus, ContentType: diagnostic.ContentType, ResponseURL: diagnostic.ResponseURL, PageURL: diagnostic.PageURL, BlockedNavigationURL: diagnostic.BlockedNavigationURL, PageReadyState: diagnostic.PageReadyState, IsDouyinContext: diagnostic.IsDouyinContext, BusinessStatus: diagnostic.BusinessStatus, CandidateCount: diagnostic.CandidateCount, AcceptedCount: diagnostic.AcceptedCount, VideoElementCount: diagnostic.VideoElementCount, DetailURLCount: diagnostic.DetailURLCount, VideoSourceCount: diagnostic.VideoSourceCount, TargetMatchCount: diagnostic.TargetMatchCount, ResponseBytes: diagnostic.ResponseBytes, ResponseSnippetRedacted: diagnostic.ResponseSnippetRedacted, FetchHookInstalled: diagnostic.FetchHookInstalled, XHRHookInstalled: diagnostic.XHRHookInstalled, CandidateRejections: diagnostic.CandidateRejections}
	playback.RecordDiagnostic(ctx, stage)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func intPtr(value int) *int { return &value }

func captureAPIStatus(responses []platformfetch.FeedResponse) int {
	for _, response := range responses {
		if response.APIStatus != 0 {
			return response.APIStatus
		}
	}
	return 0
}

func hasUnsupportedCapture(diagnostics []string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic, "不是 JSON") || strings.Contains(diagnostic, "未形成字段投影") || strings.Contains(diagnostic, "未捕获到推荐 feed") || strings.Contains(diagnostic, "页面 fetch 响应为空") {
			return true
		}
	}
	return false
}

func recommendationAPIStatus(body []byte) int {
	var value struct {
		Status int `json:"status_code"`
		Data   *struct {
			Status int `json:"status_code"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &value) != nil {
		return 0
	}
	if value.Data != nil && value.Status == 0 {
		return value.Data.Status
	}
	return value.Status
}

func (c *Client) mobileRecommendations(ctx context.Context, page int, cookie string) (discovery.Feed, error) {
	var lastErr error
	for _, endpoint := range []string{
		"https://api5-normal-c-hl.amemv.com/aweme/v1/feed/",
		"https://aweme.snssdk.com/aweme/v1/feed/",
	} {
		query := url.Values{
			"device_platform": {"android"},
			"aid":             {"1128"},
			"version_code":    {"290000"},
			"version_name":    {"29.0.0"},
			"count":           {"20"},
			"refresh_index":   {strconv.Itoa(page)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if err != nil {
			return discovery.Feed{}, err
		}
		req.Header.Set("User-Agent", "com.ss.android.ugc.aweme/290000 (Linux; Android 13)")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Cookie", cookie)
		client := *c.HTTP
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
		res.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > 4<<20 {
			lastErr = errors.New("响应超过大小限制")
			continue
		}
		if res.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s HTTP %d", endpoint, res.StatusCode)
			continue
		}
		if recommendationAPIStatus(body) == 2154 {
			lastErr = errors.New("业务状态 2154（当前暂无推荐）")
			continue
		}
		feed, decodeErr := decodeRecommendations(body, page, nil)
		if decodeErr != nil {
			lastErr = errors.New("响应不是可用的推荐 JSON")
			continue
		}
		feed.SourceTitle = "抖音移动端推荐"
		return feed, nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的移动端 feed 接口")
	}
	return discovery.Feed{}, lastErr
}

func describePageResponse(status int, body string, restricted bool, contentType, responseURL string) string {
	preview := diagnosticPreview(body)
	if restricted || looksLikeChallenge([]byte(body)) || looksLikeLoginOrChallenge(body) {
		return fmt.Sprintf("页面响应 HTTP %d，Content-Type %q，最终 URL %s，内容疑似登录/验证页，前200字符：%s", status, contentType, safeDiagnosticURL(responseURL), preview)
	}
	if body == "" {
		return fmt.Sprintf("页面响应 HTTP %d，Content-Type %q，最终 URL %s，body 为空", status, contentType, safeDiagnosticURL(responseURL))
	}
	return fmt.Sprintf("页面响应 HTTP %d，Content-Type %q，最终 URL %s，未返回可解析推荐，前200字符：%s", status, contentType, safeDiagnosticURL(responseURL), preview)
}

func describeNonJSONPageResponse(status int, restricted bool, contentType, responseURL string) string {
	suffix := "未返回可解析推荐，已跳过响应正文"
	if restricted {
		suffix = "内容疑似登录/验证页，已跳过响应正文"
	}
	return fmt.Sprintf("页面响应 HTTP %d，Content-Type %q，最终 URL %s，%s", status, contentType, safeDiagnosticURL(responseURL), suffix)
}

func describeCapturePage(pageURL, title, ready string) string {
	if strings.TrimSpace(pageURL) == "" {
		pageURL = "(未知)"
	} else {
		pageURL = safeDiagnosticURL(pageURL)
	}
	pageTitle := strings.TrimSpace(title)
	if pageTitle == "" {
		pageTitle = "(无标题)"
	}
	return fmt.Sprintf("页面最终 URL %s，标题 %q，ready=%s", pageURL, redactDiagnostic(pageTitle), ready)
}

func diagnosticPreview(body string) string {
	value := strings.TrimSpace(body)
	if len(value) > 200 {
		value = value[:200]
	}
	return redactDiagnostic(value)
}

func redactDiagnostic(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if i+8 <= len(value) && isDiagnosticTokenStart(value[i]) {
			j := i + 1
			for j < len(value) && isDiagnosticTokenChar(value[j]) {
				j++
			}
			if j-i >= 32 {
				out.WriteString("[已脱敏]")
				i = j
				continue
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func isDiagnosticTokenStart(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '_' || value == '-'
}

func isDiagnosticTokenChar(value byte) bool {
	return isDiagnosticTokenStart(value) || value == '.' || value == '~'
}

func safeDiagnosticURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(未知)"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isDouyinPageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return u.Scheme == "https" && (host == "www.douyin.com" || host == "douyin.com")
}

func looksLikeVerificationURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	value := strings.ToLower(u.Path + "?" + u.RawQuery)
	for _, marker := range []string{"login", "passport", "verify", "captcha", "challenge", "acrawler", "sec_"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func joinDiagnostics(diagnostics []string, fallback string) string {
	if len(diagnostics) == 0 {
		return fallback
	}
	// Keep the first response clues and the latest page/DOM state. Trimming
	// from the front alone can hide the only useful HTTP context, while an
	// unbounded list would make a repeated feed capture unwieldy in the UI.
	selected := diagnostics
	if len(diagnostics) > 8 {
		selected = append(append([]string(nil), diagnostics[:3]...), diagnostics[len(diagnostics)-5:]...)
	}
	parts := make([]string, 0, len(selected)+1)
	for _, diagnostic := range selected {
		if value := strings.TrimSpace(diagnostic); value != "" {
			parts = append(parts, value)
		}
	}
	if value := strings.TrimSpace(fallback); value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, "；")
}

func cookieValue(header, name string) string {
	for _, pair := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && key == name && len(value) <= 4096 {
			return value
		}
	}
	return ""
}

func looksLikeChallenge(body []byte) bool {
	value := strings.ToLower(string(body))
	return strings.Contains(value, "verify_center") || strings.Contains(value, "verify_page") || strings.Contains(value, "captcha") || strings.Contains(value, "secsdk") || strings.Contains(value, "byted_acrawler") || strings.Contains(value, "slide_verify") || strings.Contains(value, "just a moment") || strings.Contains(value, "cf-chl-") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "cloudflare")
}

func looksLikeLoginOrChallenge(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "login") || strings.Contains(lower, "登录") || strings.Contains(lower, "verify") || strings.Contains(lower, "验证") || strings.Contains(lower, "captcha")
}

type feedVideo struct {
	ID          string `json:"aweme_id"`
	Description string `json:"desc"`
	Created     int64  `json:"create_time"`
	Author      struct {
		ID     string    `json:"uid"`
		Name   string    `json:"nickname"`
		Avatar feedImage `json:"avatar_thumb"`
	} `json:"author"`
	Statistics struct {
		Likes    int `json:"digg_count"`
		Comments int `json:"comment_count"`
		Plays    int `json:"play_count"`
	} `json:"statistics"`
	Video struct {
		Duration     int       `json:"duration"`
		Cover        feedImage `json:"cover"`
		OriginCover  feedImage `json:"origin_cover"`
		DynamicCover feedImage `json:"dynamic_cover"`
		Fallback     feedImage `json:"fallback_cover"`
		CoverURL     string    `json:"coverUrl"`
		OriginURL    string    `json:"originCover"`
		DynamicURL   string    `json:"dynamicCover"`
	} `json:"video"`
}

type feedImage struct {
	URLs      []string `json:"url_list"`
	URLsCamel []string `json:"urlList"`
	URL       string   `json:"url"`
	URI       string   `json:"uri"`
}

func (image feedImage) candidates() []string {
	values := append([]string(nil), image.URLs...)
	values = append(values, image.URLsCamel...)
	if image.URL != "" {
		values = append(values, image.URL)
	}
	if image.URI != "" {
		values = append(values, image.URI)
	}
	return values
}

const maxDouyinCapturePages = 10

func mergeRecommendationFeeds(feeds []discovery.Feed, page int, sourceTitle string) discovery.Feed {
	merged := discovery.Feed{Items: make([]content.Item, 0), Page: page, SourceTitle: sourceTitle}
	seen := make(map[string]struct{})
	for _, feed := range feeds {
		merged.HasMore = merged.HasMore || feed.HasMore
		for _, item := range feed.Items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			merged.Items = append(merged.Items, item)
		}
	}
	merged.Total = len(merged.Items)
	return merged
}

func mapDOMCards(cards []platformfetch.DOMCard, page int) discovery.Feed {
	feed := discovery.Feed{Items: make([]content.Item, 0, len(cards)), Page: page, SourceTitle: "抖音精选"}
	seen := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		id := strings.TrimSpace(card.ID)
		if !content.ValidID("douyin:" + id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		ref, err := discovery.ParseDouyin("https://www.douyin.com/video/" + id)
		if err != nil {
			continue
		}
		title := strings.TrimSpace(card.Title)
		if title == "" {
			title = strings.TrimSpace(card.Author) + "的视频"
		}
		item := content.Item{ID: "douyin:" + id, Platform: "douyin", PlatformID: id, Kind: "video", Title: title, Author: strings.TrimSpace(card.Author), AuthorID: strings.TrimSpace(card.AuthorID), URL: ref.URL, ThumbnailURL: safeHTTPSURL(card.ThumbnailURL), DurationSeconds: maxInt(card.Duration, 0), LikeCount: maxInt(card.LikeCount, 0)}
		if content.Validate(item) != nil {
			continue
		}
		feed.Items = append(feed.Items, item)
		seen[id] = struct{}{}
	}
	feed.Total = len(feed.Items)
	return feed
}

func mapFeedCards(cards []platformfetch.DOMCard, page int) discovery.Feed {
	feed := mapDOMCards(cards, page)
	feed.SourceTitle = "抖音页面推荐"
	return feed
}

func safeHTTPSURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return ""
	}
	return u.String()
}

func maxInt(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

func describeCaptureResources(paths, initiators []string, fetchHook, xhrHook bool) string {
	parts := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for index, path := range paths {
		path = redactDiagnostic(path)
		if path == "" {
			continue
		}
		initiator := ""
		if index < len(initiators) {
			initiator = strings.TrimSpace(initiators[index])
		}
		value := path
		if initiator != "" {
			value += "(" + initiator + ")"
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		parts = append(parts, value)
	}
	if len(parts) > 12 {
		parts = parts[:12]
	}
	return fmt.Sprintf("页面 aweme 资源：%s；fetch 钩子=%t，XHR 钩子=%t", strings.Join(parts, ", "), fetchHook, xhrHook)
}

func describeCaptureDOM(d platformfetch.DOMDiagnostics) string {
	selectors := make([]string, 0, len(d.CandidateSelectorHits))
	for selector, count := range d.CandidateSelectorHits {
		selectors = append(selectors, fmt.Sprintf("%s=%d", selector, count))
	}
	sort.Strings(selectors)
	return fmt.Sprintf("精选 DOM 诊断：URL=%s，视频链接=%d，候选选择器命中=%s，读取卡片=%d，等待=%dms", safeDiagnosticURL(d.URL), d.VideoLinkCount, strings.Join(selectors, ","), d.CardsRead, d.WaitMilliseconds)
}

func isJSONContentType(contentType string) bool {
	value := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return value == "" || value == "application/json" || value == "text/json" || strings.HasSuffix(value, "+json")
}

func decodeRecommendations(body []byte, page int, denied error) (discovery.Feed, error) {
	var response struct {
		Status *int        `json:"status_code"`
		More   int         `json:"has_more"`
		Videos []feedVideo `json:"aweme_list"`
		Data   *struct {
			Status *int        `json:"status_code"`
			More   int         `json:"has_more"`
			Videos []feedVideo `json:"aweme_list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return discovery.Feed{}, decodeError(denied, "响应不是 JSON")
	}
	if response.Data != nil {
		if response.Status == nil {
			response.Status = response.Data.Status
		}
		if len(response.Videos) == 0 {
			response.Videos = response.Data.Videos
		}
		if response.More == 0 {
			response.More = response.Data.More
		}
	}
	if response.Status == nil || *response.Status != 0 || response.Videos == nil || len(response.Videos) > 500 {
		return discovery.Feed{}, decodeError(denied, "推荐 JSON 状态无效或没有视频列表")
	}
	feed := discovery.Feed{Items: []content.Item{}, Page: page, HasMore: response.More != 0, SourceTitle: "抖音网页推荐"}
	seen := map[string]bool{}
	for _, video := range response.Videos {
		ref, err := discovery.ParseDouyin("https://www.douyin.com/video/" + video.ID)
		if err != nil || ref.ID != video.ID || seen[video.ID] {
			continue
		}
		title := strings.TrimSpace(video.Description)
		if title == "" {
			title = video.Author.Name + "的视频"
		}
		item := content.Item{ID: "douyin:" + video.ID, Platform: "douyin", PlatformID: video.ID, Kind: "video", Title: title, Description: video.Description, Author: video.Author.Name, AuthorID: video.Author.ID, URL: ref.URL, DurationSeconds: video.Video.Duration / 1000, PublishedAt: video.Created, LikeCount: video.Statistics.Likes}
		coverCandidates := make([]string, 0, 12)
		for _, image := range []feedImage{video.Video.Cover, video.Video.OriginCover, video.Video.DynamicCover, video.Video.Fallback} {
			coverCandidates = append(coverCandidates, image.candidates()...)
		}
		coverCandidates = append(coverCandidates, video.Video.CoverURL, video.Video.OriginURL, video.Video.DynamicURL)
		for _, raw := range coverCandidates {
			if safe := safeHTTPSURL(raw); safe != "" {
				item.ThumbnailURL = safe
				break
			}
		}
		for _, raw := range video.Author.Avatar.candidates() {
			if safe := safeHTTPSURL(raw); safe != "" {
				item.AuthorAvatarURL = safe
				break
			}
		}
		if content.Validate(item) == nil {
			feed.Items = append(feed.Items, item)
			seen[video.ID] = true
		}
	}
	if len(response.Videos) > 0 && len(feed.Items) == 0 {
		return discovery.Feed{}, decodeError(denied, "推荐视频字段无法映射")
	}
	feed.Total = len(feed.Items)
	return feed, nil
}

func decodeError(denied error, detail string) error {
	if denied != nil {
		return denied
	}
	return errors.New(detail)
}
