package feeds

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/discovery"
	"tangentry/internal/netpolicy"
)

// Validators belong to the exact final resource that supplied the feed.
type Validators struct {
	ETag         string
	LastModified string
	ResourceURL  string
}
type Result struct {
	Feed        discovery.Feed
	Validators  Validators
	NotModified bool
}
type FetchError struct {
	Message           string
	StatusCode        int
	RetryAfterSeconds int64
	Restricted        bool
	TimedOut          bool
}

func (e *FetchError) Timeout() bool { return e.TimedOut }

func (e *FetchError) HTTPStatusCode() int      { return e.StatusCode }
func (e *FetchError) RetryDelaySeconds() int64 { return e.RetryAfterSeconds }
func (e *FetchError) Error() string            { return e.Message }
func (e *FetchError) Unwrap() error {
	return &discovery.PlatformError{Message: e.Message, Restricted: e.Restricted}
}

func cleanETag(raw string) string {
	if len(raw) > 1024 {
		return ""
	}
	tag := strings.TrimPrefix(raw, "W/")
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return ""
	}
	for _, b := range []byte(tag[1 : len(tag)-1]) {
		if b < 0x21 || b == 0x22 || b == 0x7f {
			return ""
		}
	}
	return raw
}
func cleanModified(raw string) string {
	if len(raw) > 128 {
		return ""
	}
	at, err := http.ParseTime(raw)
	if err != nil {
		return ""
	}
	return at.UTC().Format(http.TimeFormat)
}
func validators(headers http.Header, resource string) Validators {
	return Validators{ETag: cleanETag(headers.Get("ETag")), LastModified: cleanModified(headers.Get("Last-Modified")), ResourceURL: resource}
}
func retryAfter(raw string, now time.Time) int64 {
	const limit = 7 * 24 * 3600
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && seconds >= 0 {
		return min(seconds, limit)
	}
	if at, err := http.ParseTime(raw); err == nil && at.After(now) {
		return min(int64(at.Sub(now).Seconds())+1, limit)
	}
	return 0
}

func (c *Client) FetchConditional(ctx context.Context, raw string, cached Validators) (Result, error) {
	return c.fetchConditional(ctx, raw, "", cached)
}

func (c *Client) FetchConditionalIdentity(ctx context.Context, raw, identityURL string, cached Validators) (Result, error) {
	identity, err := netpolicy.URL(identityURL)
	if err != nil {
		return Result{}, discovery.ErrInvalidQuery
	}
	return c.fetchConditional(ctx, raw, identity, cached)
}

func (c *Client) fetchConditional(ctx context.Context, raw, identity string, cached Validators) (Result, error) {
	canonical, err := netpolicy.URL(raw)
	if err != nil {
		return Result{}, discovery.ErrInvalidQuery
	}
	cached.ETag = cleanETag(cached.ETag)
	cached.LastModified = cleanModified(cached.LastModified)
	apply := func(req *http.Request) {
		req.Header.Del("If-None-Match")
		req.Header.Del("If-Modified-Since")
		if req.URL.String() != cached.ResourceURL {
			return
		}
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", canonical, nil)
	req.Header.Set("User-Agent", "Tangentry/0.1 RSS Reader")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/json, application/xml, text/xml")
	apply(req)
	// Clone the client rather than modifying redirect behavior shared by media.
	client := *c.HTTP
	client.Jar = nil
	previous := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return &FetchError{Message: "订阅源重定向过多，请检查地址。"}
		}
		if previous != nil {
			if err := previous(next, via); err != nil {
				return err
			}
		}
		if _, err := netpolicy.URL(next.URL.String()); err != nil {
			return err
		}
		next.Header.Del("Cookie")
		next.Header.Del("Authorization")
		// Feed URLs may carry private RSSHub keys in their query strings.
		next.Header.Del("Referer")
		apply(next)
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return Result{}, &FetchError{TimedOut: true, Message: "等待订阅源响应超时，地址不一定有误。若使用自部署 RSSHub，请检查路由生成耗时、服务日志与上游网络，稍后再更新。"}
		}
		return Result{}, &FetchError{Message: "订阅源暂时无法连接，请检查地址与网络。"}
	}
	defer response.Body.Close()
	resource := canonical
	if response.Request != nil {
		resource = response.Request.URL.String()
	}
	confirmed := validators(response.Header, resource)
	if response.StatusCode == http.StatusNotModified {
		if resource != cached.ResourceURL || cached.ETag == "" && cached.LastModified == "" {
			return Result{}, &FetchError{Message: "订阅源返回了无法验证的缓存结果，请稍后重试。"}
		}
		if confirmed.ETag == "" {
			confirmed.ETag = cached.ETag
		}
		if confirmed.LastModified == "" {
			confirmed.LastModified = cached.LastModified
		}
		return Result{Validators: confirmed, NotModified: true}, nil
	}
	if response.StatusCode != http.StatusOK {
		limited := response.StatusCode == 429
		message := "订阅源未能提供内容，请检查地址与访问权限。"
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			message = "订阅源拒绝访问，请检查 RSSHub 访问密钥（key）或订阅源的鉴权配置。"
		case response.StatusCode == http.StatusNotFound:
			message = "订阅源返回 HTTP 404，请检查路由是否正确，以及自部署 RSSHub 是否支持该路由。"
		case limited:
			message = "订阅源暂时限制请求，请稍后重试。"
		case response.StatusCode >= 500:
			message = "订阅源服务器返回 HTTP " + strconv.Itoa(response.StatusCode) + "，未能生成 RSS。若使用自部署 RSSHub，请检查该路由的服务日志、平台 Cookie 与网络连接。"
		}
		wait := int64(0)
		if limited || response.StatusCode == 503 {
			wait = retryAfter(response.Header.Get("Retry-After"), time.Now())
		}
		return Result{}, &FetchError{Message: message, StatusCode: response.StatusCode, RetryAfterSeconds: wait, Restricted: limited}
	}
	if identity == "" {
		identity = canonical
	}
	feed, err := c.parseResponse(ctx, response, identity)
	if err != nil {
		return Result{}, err
	}
	feed.Query = canonical
	return Result{Feed: feed, Validators: confirmed}, nil
}
