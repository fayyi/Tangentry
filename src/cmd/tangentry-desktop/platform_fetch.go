package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"tangentry/internal/app"
	"tangentry/internal/platformfetch"
)

const (
	platformFetchMaxBody = 4 << 20
	platformFetchTimeout = 20 * time.Second
	platformFetchIdle    = 5 * time.Minute
)

type platformPageBridge struct {
	mu       sync.Mutex
	sessions map[string]*platformPageSession
	closed   bool
}

type platformPageSession struct {
	mu                sync.Mutex
	platform          string
	origin            string
	window            *mygo.Window
	lastUsed          time.Time
	closeAt           *time.Timer
	ready             chan struct{}
	readyOnce         sync.Once
	blockedNavigation atomic.Value
}

const douyinFeedCaptureScript = `(function(){
  if (window.__tangentryDouyinFeedInstalled) return;
  window.__tangentryDouyinFeedInstalled = true;
  window.__tangentryDouyinFeed = window.__tangentryDouyinFeed || [];
  const max = 4 * 1024 * 1024;
  const isFeed = value => /\/aweme\/(?:v1|v2)\/[^?]*feed/i.test(String(value || ''));
  const push = (url, status, body) => {
    if (!isFeed(url) || typeof body !== 'string' || !body || body.length > max) return;
    window.__tangentryDouyinFeed.push({url:String(url), status:Number(status)||0, body:body});
    if (window.__tangentryDouyinFeed.length > 32) window.__tangentryDouyinFeed.splice(0, window.__tangentryDouyinFeed.length - 32);
  };
  const originalFetch = window.fetch;
  window.fetch = function(){
    const request = arguments[0];
    const url = typeof request === 'string' ? request : request && request.url;
    return originalFetch.apply(this, arguments).then(response => {
      if (!isFeed(url || response.url)) return response;
      try { response.clone().text().then(body => push(url || response.url, response.status, body)).catch(() => {}); } catch (_) {}
      return response;
    });
  };
  const open = XMLHttpRequest.prototype.open;
  const send = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function(method, url){ this.__tangentryURL = url; return open.apply(this, arguments); };
  XMLHttpRequest.prototype.send = function(){
    this.addEventListener('load', function(){ try { const body = this.responseType === 'json' ? JSON.stringify(this.response) : this.responseText; push(this.__tangentryURL || this.responseURL, this.status, body); } catch (_) {} });
    return send.apply(this, arguments);
  };
})();`

func newPlatformPageBridge() *platformPageBridge {
	return &platformPageBridge{sessions: make(map[string]*platformPageSession)}
}

func (b *platformPageBridge) Fetch(ctx context.Context, platform, path string, request app.PlatformPageRequest) (app.PlatformPageResponse, error) {
	origin, ok := platformPageOrigin(platform)
	if !ok {
		return app.PlatformPageResponse{}, errors.New("不支持的平台页面请求")
	}
	if err := validatePlatformPath(path); err != nil {
		return app.PlatformPageResponse{}, err
	}
	if err := validatePlatformFetchRequest(&request); err != nil {
		return app.PlatformPageResponse{}, err
	}

	session, err := b.session(platform, origin)
	if err != nil {
		return app.PlatformPageResponse{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := waitPlatformPage(ctx, session.ready); err != nil {
		return app.PlatformPageResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return app.PlatformPageResponse{}, err
	}
	session.lastUsed = time.Now()
	if session.closeAt != nil {
		session.closeAt.Stop()
		session.closeAt = nil
	}
	code, err := platformFetchScript(path, request)
	if err != nil {
		return app.PlatformPageResponse{}, err
	}
	rawResult, err := session.window.Page().EvalContext(ctx, code)
	if err != nil {
		return app.PlatformPageResponse{}, err
	}
	encoded, err := json.Marshal(rawResult)
	if err != nil {
		return app.PlatformPageResponse{}, err
	}
	var result platformFetchResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return app.PlatformPageResponse{}, err
	}
	if err := validatePlatformFetchResponse(result); err != nil {
		return app.PlatformPageResponse{}, err
	}
	if session.closeAt == nil {
		session.closeAt = time.AfterFunc(platformFetchIdle, func() {
			session.mu.Lock()
			idle := time.Since(session.lastUsed) >= platformFetchIdle
			if idle {
				session.closeAt = nil
			}
			session.mu.Unlock()
			if idle && session.window != nil && !session.window.IsDestroyed() {
				session.window.Close()
			}
		})
	}
	return app.PlatformPageResponse{Status: result.Status, Body: result.Body, Restricted: isCloudflarePage(result.Status, result.Body), URL: session.window.Page().URL()}, nil
}

func (b *platformPageBridge) CaptureFeed(ctx context.Context, platform string, pages int) (platformfetch.FeedCaptureResponse, error) {
	if platform != "douyin" {
		return platformfetch.FeedCaptureResponse{}, errors.New("该平台不支持页面推荐抓取")
	}
	if pages < 1 || pages > 10 {
		return platformfetch.FeedCaptureResponse{}, errors.New("页面推荐抓取页数无效")
	}
	session, err := b.session(platform, "https://www.douyin.com/")
	if err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := waitPlatformPage(ctx, session.ready); err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	session.lastUsed = time.Now()
	if session.closeAt != nil {
		session.closeAt.Stop()
		session.closeAt = nil
	}
	session.blockedNavigation.Store("")
	code := feedCaptureScript(pages)
	raw, err := session.window.Page().EvalContext(ctx, code)
	if err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	var result struct {
		PageURL   string `json:"pageUrl"`
		Title     string `json:"title"`
		Ready     string `json:"ready"`
		Responses []struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
			Body   string `json:"body"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	responses := make([]platformfetch.Response, 0, len(result.Responses))
	for _, response := range result.Responses {
		if len(response.Body) > platformFetchMaxBody {
			continue
		}
		responses = append(responses, platformfetch.Response{Status: response.Status, Body: response.Body, URL: response.URL, Restricted: isCloudflarePage(response.Status, response.Body)})
	}
	if session.closeAt == nil {
		session.closeAt = time.AfterFunc(platformFetchIdle, func() {
			session.mu.Lock()
			idle := time.Since(session.lastUsed) >= platformFetchIdle
			if idle {
				session.closeAt = nil
			}
			session.mu.Unlock()
			if idle && session.window != nil && !session.window.IsDestroyed() {
				session.window.Close()
			}
		})
	}
	pageURL := result.PageURL
	if blocked, ok := session.blockedNavigation.Load().(string); ok && blocked != "" {
		pageURL = blocked
	}
	return platformfetch.FeedCaptureResponse{Responses: responses, PageURL: pageURL, Title: result.Title, Ready: result.Ready}, nil
}

func (b *platformPageBridge) session(platform, origin string) (*platformPageSession, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errors.New("平台页面桥已关闭")
	}
	if existing := b.sessions[platform]; existing != nil && !existing.window.IsDestroyed() {
		b.mu.Unlock()
		return existing, nil
	}
	session := &platformPageSession{platform: platform, origin: origin, lastUsed: time.Now(), ready: make(chan struct{})}
	session.blockedNavigation.Store("")
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "Tangentry 平台请求", Hidden: true,
		Width: 800, Height: 600, MinWidth: 640, MinHeight: 480,
		SkipTaskbar: true, Page: mygo.PageOptions{TrustedOrigins: []string{origin}, PreloadScript: func() string {
			if platform == "douyin" {
				return douyinFeedCaptureScript
			}
			return ""
		}()},
	})
	window.Page().OnPageTitleUpdated(func(event *mygo.TitleEvent) { event.PreventDefault() })
	window.Page().OnDOMReady(func() { session.readyOnce.Do(func() { close(session.ready) }) })
	window.Page().OnWillNavigate(func(event *mygo.NavigateEvent) {
		if !platformPageURLAllowed(event.URL, platform) {
			session.blockedNavigation.Store(event.URL)
			event.PreventDefault()
		}
	})
	window.Page().SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
	window.OnClosed(func() {
		b.mu.Lock()
		if b.sessions[platform] == session {
			delete(b.sessions, platform)
		}
		b.mu.Unlock()
	})
	session.window = window
	b.sessions[platform] = session
	b.mu.Unlock()
	if err := window.Page().LoadURL(origin); err != nil {
		b.mu.Lock()
		if b.sessions[platform] == session {
			delete(b.sessions, platform)
		}
		b.mu.Unlock()
		window.Close()
		return nil, err
	}
	return session, nil
}

func waitPlatformPage(ctx context.Context, ready <-chan struct{}) error {
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(platformFetchTimeout):
		return context.DeadlineExceeded
	}
}

func (b *platformPageBridge) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	sessions := make([]*platformPageSession, 0, len(b.sessions))
	for _, session := range b.sessions {
		sessions = append(sessions, session)
	}
	b.sessions = make(map[string]*platformPageSession)
	b.mu.Unlock()
	for _, session := range sessions {
		if session.closeAt != nil {
			session.closeAt.Stop()
		}
		if session.window != nil && !session.window.IsDestroyed() {
			session.window.Close()
		}
	}
}

func platformPageOrigin(platform string) (string, bool) {
	switch platform {
	case "linuxdo":
		return "https://linux.do/", true
	case "douyin":
		return "https://www.douyin.com/", true
	default:
		return "", false
	}
}

func validatePlatformPath(raw string) error {
	if raw == "" || len(raw) > 8192 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\x00\r\n") {
		return errors.New("页面请求路径无效")
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || strings.Contains(u.Path, "..") {
		return errors.New("页面请求必须是同源路径")
	}
	return nil
}

func validatePlatformFetchRequest(request *app.PlatformPageRequest) error {
	if request.Method == "" {
		request.Method = "GET"
	}
	if request.Method != "GET" && request.Method != "POST" {
		return errors.New("页面请求方法不受支持")
	}
	if len(request.Body) > 256<<10 || len(request.Headers) > 16 {
		return errors.New("页面请求参数过大")
	}
	total := 0
	for name, value := range request.Headers {
		if name == "" || len(name) > 128 || len(value) > 8192 || strings.ContainsAny(name, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return errors.New("页面请求标头无效")
		}
		total += len(name) + len(value)
	}
	if total > 64<<10 {
		return errors.New("页面请求标头过大")
	}
	return nil
}

func platformPageURLAllowed(raw, platform string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	switch platform {
	case "linuxdo":
		return host == "linux.do"
	case "douyin":
		return host == "www.douyin.com"
	default:
		return false
	}
}

func validatePlatformFetchResponse(result platformFetchResult) error {
	if result.TooLarge || len(result.Body) > platformFetchMaxBody {
		return errors.New("平台页面响应超过大小限制")
	}
	return nil
}

type platformFetchResult struct {
	Status   int    `json:"status"`
	Body     string `json:"body"`
	TooLarge bool   `json:"tooLarge"`
}

func platformFetchScript(path string, request app.PlatformPageRequest) (string, error) {
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return "", err
	}
	method, _ := json.Marshal(request.Method)
	headers, err := json.Marshal(request.Headers)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(request.Body)
	if err != nil {
		return "", err
	}
	bodyOption := ""
	if request.Method != "GET" && request.Method != "HEAD" {
		bodyOption = ", body:" + string(body)
	}
	return fmt.Sprintf(`const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), %d); try { return await fetch(location.origin + %s, {method:%s, headers:%s, credentials:"include"%s, signal:controller.signal}).then(async response => { const max=%d; if (!response.body) { const body=await response.text(); return {status:response.status, body:body.length>max?"":body, tooLarge:body.length>max}; } const reader=response.body.getReader(); const decoder=new TextDecoder(); let body="", bytes=0; try { for (;;) { const part=await reader.read(); if (part.done) break; bytes+=part.value.byteLength; if (bytes>max) { await reader.cancel(); return {status:response.status, body:"", tooLarge:true}; } body+=decoder.decode(part.value,{stream:true}); } body+=decoder.decode(); return {status:response.status, body:body, tooLarge:false}; } finally { reader.releaseLock(); } }); } finally { clearTimeout(timer); }`, platformFetchTimeout.Milliseconds(), pathJSON, method, headers, bodyOption, platformFetchMaxBody), nil
}

func feedCaptureScript(pages int) string {
	return fmt.Sprintf(`const wait = ms => new Promise(resolve => setTimeout(resolve, ms)); const count = %d; if (window.__tangentryDouyinFeed) window.__tangentryDouyinFeed.splice(0); for (let i=0; i<count; i++) { window.scrollTo(0, document.body ? document.body.scrollHeight : 0); await wait(1200); } await wait(800); return {pageUrl: location.href, title: document.title || "", ready: document.readyState, responses: (window.__tangentryDouyinFeed || []).splice(0, 32)};`, pages)
}

func isCloudflarePage(status int, body string) bool {
	if status == 403 {
		return true
	}
	value := strings.ToLower(body)
	return strings.Contains(value, "just a moment") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "cf-chl-") || strings.Contains(value, "cloudflare")
}
