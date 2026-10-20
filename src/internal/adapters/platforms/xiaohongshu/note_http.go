package xiaohongshu

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

// Share expansion is anonymous. Only the final note page receives its own Cookie.
func (c *Client) Note(ctx context.Context, raw string) (content.Item, error) {
	return c.note(ctx, raw, "")
}

func (c *Client) NoteFor(ctx context.Context, id, raw string) (content.Item, error) {
	if !content.ValidID("xiaohongshu:" + id) {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	return c.note(ctx, raw, id)
}

func (c *Client) note(ctx context.Context, raw, expectedID string) (content.Item, error) {
	ref, err := discovery.ParseXiaohongshu(raw)
	if err != nil {
		return content.Item{}, err
	}
	if expectedID != "" && ref.ID != "" && ref.ID != expectedID {
		return content.Item{}, noteMismatch()
	}
	if ref.ID != "" && !hasShareToken(ref.URL) {
		return content.Item{}, missingShareToken()
	}
	if c.HTTP == nil {
		return content.Item{}, discovery.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := *c.HTTP
	prior := client.CheckRedirect
	client.Jar = nil
	resolvingShare := ref.ID == ""
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !platformHost(strings.ToLower(req.URL.Hostname())) || req.URL.Scheme != "https" || req.URL.User != nil || req.URL.Port() != "" {
			return discovery.ErrInvalidQuery
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		if pageLocationError(req.URL) != nil {
			return http.ErrUseLastResponse
		}
		if resolvingShare {
			if target, err := discovery.ParseXiaohongshu(req.URL.String()); err == nil && target.ID != "" {
				// Resolve the locator without opening the authenticated note
				// anonymously and getting redirected to a login page.
				return http.ErrUseLastResponse
			}
		}
		if prior != nil {
			return prior(req, via)
		}
		return nil
	}
	fetch := func(locator string, authenticated bool) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
		userAgent := "Mozilla/5.0"
		browserUserAgent := ""
		if c.UserAgent != nil {
			browserUserAgent = c.UserAgent(locator)
		}
		if browserUserAgent != "" {
			userAgent = browserUserAgent
		}
		cookie := ""
		if authenticated && c.Cookie != nil {
			cookie = c.Cookie(locator)
			if cookie != "" {
				req.Header.Set("Cookie", cookie)
			}
		}
		if authenticated && cookie == "" && browserUserAgent == "" {
			userAgent = anonymousNoteUserAgent
		}
		req.Header.Set("User-Agent", userAgent)
		return client.Do(req)
	}
	if ref.ID == "" {
		res, err := fetch(ref.URL, false)
		if err != nil {
			return content.Item{}, requestError(ctx)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
		res.Body.Close()
		target := res.Request.URL.String()
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			if location, err := res.Request.URL.Parse(res.Header.Get("Location")); err == nil {
				target = location.String()
			}
		} else if res.StatusCode != http.StatusOK {
			return content.Item{}, noteResponseError(res)
		}
		ref, err = discovery.ParseXiaohongshu(target)
		if err != nil || ref.ID == "" {
			if res.StatusCode != http.StatusOK {
				return content.Item{}, noteResponseError(res)
			}
			return content.Item{}, &discovery.PlatformError{Code: "xiaohongshu_share_unresolved", Message: "分享链接没有返回小红书单条笔记，请复制包含访问参数的完整分享链接。"}
		}
		if !hasShareToken(ref.URL) {
			return content.Item{}, missingShareToken()
		}
		resolvingShare = false
	}
	if expectedID != "" && ref.ID != expectedID {
		return content.Item{}, noteMismatch()
	}
	res, err := fetch(ref.URL, true)
	if err != nil {
		return content.Item{}, requestError(ctx)
	}
	defer res.Body.Close()
	final, err := discovery.ParseXiaohongshu(res.Request.URL.String())
	if res.StatusCode != http.StatusOK {
		return content.Item{}, noteResponseError(res)
	}
	if err != nil || final.ID != ref.ID {
		if locationErr := pageLocationError(res.Request.URL); locationErr != nil {
			return content.Item{}, locationErr
		}
		return content.Item{}, unavailable()
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return content.Item{}, ctx.Err()
		}
		return content.Item{}, discovery.ErrUnavailable
	}
	if len(data) > 4<<20 {
		return content.Item{}, discovery.ErrUnavailable
	}
	item, err := Parse(data, ref)
	if err != nil {
		var platform *discovery.PlatformError
		if errors.As(err, &platform) && platform.Code == "xiaohongshu_page_structure_changed" {
			return content.Item{}, pageStructureError(data, res.StatusCode, res.Header.Get("Content-Type"), res.Request.URL)
		}
	}
	if err == nil {
		c.remember(item)
	}
	return item, err
}

const anonymousNoteUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func hasShareToken(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Query().Get("xsec_token") != ""
}

func platformHost(host string) bool {
	return host == "www.xiaohongshu.com" || host == "xiaohongshu.com" || host == "xhslink.com" || host == "www.xhslink.com"
}

func noteMismatch() error {
	return &discovery.PlatformError{Code: "xiaohongshu_note_mismatch", Message: "分享链接与当前笔记不一致，请复制这篇笔记的链接。"}
}

func missingShareToken() error {
	return &discovery.PlatformError{Code: "xiaohongshu_share_token_missing", Message: "这条笔记地址缺少分享访问参数。请从小红书重新复制包含访问参数的完整分享链接后再试。"}
}

func requestError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &discovery.PlatformError{Code: "xiaohongshu_request_failed", Message: "小红书页面暂时无法连接，请检查链接与网络。"}
}

func unavailable() error {
	return &discovery.PlatformError{Code: "xiaohongshu_page_unavailable", Message: "小红书笔记暂时无法读取，请稍后重试并确认原站仍可访问。"}
}

func initializerUnavailable() error {
	return &discovery.PlatformError{Code: "xiaohongshu_page_structure_changed", Message: "小红书页面缺少或无法解析 __INITIAL_STATE__，页面结构可能已变化或请求被拦截。"}
}

func pageLocationError(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.xiaohongshu.com" && u.Hostname() != "xiaohongshu.com") {
		return nil
	}
	switch strings.TrimRight(u.Path, "/") {
	case "/login", "/website-login":
		return unavailableNote()
	case "/404":
		return unavailableNote()
	}
	return nil
}

func noteResponseError(res *http.Response) error {
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		if target, err := res.Request.URL.Parse(res.Header.Get("Location")); err == nil {
			if locationErr := pageLocationError(target); locationErr != nil {
				return locationErr
			}
		}
	}
	return noteStatusError(res.StatusCode)
}

func noteStatusError(status int) error {
	if status == http.StatusTooManyRequests {
		return restrictedNote()
	}
	if status == http.StatusNotFound {
		return unavailableNote()
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == 461 || status == 471 {
		if status == 461 || status == 471 {
			return restrictedNote()
		}
		return &discovery.PlatformError{Code: "xiaohongshu_access_denied", Message: "小红书拒绝了当前访问，请确认原站笔记可访问，并在工具设置检查小红书 Cookie。"}
	}
	return unavailable()
}

func restrictedNote() error {
	return &discovery.PlatformError{Code: "xiaohongshu_restricted", Message: "小红书暂时限制了内容请求，请稍后重试；如果仍然失败，请在工具设置配置小红书 Cookie。", Restricted: true}
}

func unavailableNote() error {
	return &discovery.PlatformError{Code: "xiaohongshu_note_unavailable", Message: "分享访问参数可能已过期，笔记也可能已删除或不可见。请重新复制分享链接，并确认原站仍可访问。"}
}

func pageStructureError(data []byte, status int, contentType string, finalURL *url.URL) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "未知"
	}
	message := "小红书页面缺少或无法解析 __INITIAL_STATE__，页面结构可能已变化或请求被拦截。响应诊断：HTTP " + strconv.Itoa(status) + "，Content-Type " + mediaType
	if safeURL := redactedResponseURL(finalURL); safeURL != "" {
		message += "，最终地址 " + safeURL
	}
	message += "。"
	if snippet := safePageSnippet(data); snippet != "" {
		message += " 页面片段：「" + snippet + "」"
	}
	return &discovery.PlatformError{Code: "xiaohongshu_page_structure_changed", Message: message}
}

func redactedResponseURL(u *url.URL) string {
	if u == nil || u.User != nil || u.Hostname() == "" {
		return ""
	}
	value := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		value += "?[参数已省略]"
	}
	return value
}

func safePageSnippet(data []byte) string {
	tokenizer := html.NewTokenizer(strings.NewReader(string(data)))
	var text strings.Builder
	skipRawText := false
	written := 0
	for written < 512 {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind == html.StartTagToken {
			name, _ := tokenizer.TagName()
			skipRawText = strings.EqualFold(string(name), "script") || strings.EqualFold(string(name), "style")
		} else if kind == html.EndTagToken {
			skipRawText = false
		} else if kind == html.TextToken && !skipRawText {
			part := []rune(string(tokenizer.Text()))
			if remaining := 512 - written; len(part) > remaining {
				part = part[:remaining]
			}
			text.WriteString(string(part))
			text.WriteByte(' ')
			written += len(part)
		}
	}
	value := strings.Join(strings.Fields(text.String()), " ")
	value = sensitiveQueryPattern.ReplaceAllString(value, "$1[已脱敏]")
	value = sensitiveJSONPattern.ReplaceAllString(value, "$1[已脱敏]")
	value = cookieValuePattern.ReplaceAllString(value, "$1[已脱敏]")
	value = sessionValuePattern.ReplaceAllString(value, "$1[已脱敏]")
	runes := []rune(value)
	if len(runes) > 180 {
		value = string(runes[:180]) + "…"
	}
	return value
}

var sensitiveQueryPattern = regexp.MustCompile(`(?i)((?:[?&]|\b)(?:xsec_token|xsec_source|access_token|web_session|session|sid|token|sign|signature|auth|authorization|cookie)=)[^&#\s"'<>]*`)
var sensitiveJSONPattern = regexp.MustCompile(`(?i)(["']?(?:xsec_token|xsec_source|token|sign|signature|authorization|cookie|web_session|session)["']?\s*[:=]\s*["']?)[^"'&,}\s<>]+`)
var cookieValuePattern = regexp.MustCompile(`(?i)(cookie\s*[:=]\s*)[^\s<>"']+`)
var sessionValuePattern = regexp.MustCompile(`(?i)((?:session|sid|web_session|access_token)\s*=\s*)[^;\s&<>"']+`)
