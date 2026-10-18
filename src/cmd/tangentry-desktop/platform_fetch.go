package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
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
	douyinCapturePoll    = 200 * time.Millisecond
	douyinCaptureReads   = 75
	douyinDOMWait        = 15 * time.Second
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

const (
	platformPageWindowWidth  = 1024
	platformPageWindowHeight = 768
	platformPageWindowGap    = 32
)

// platformPageWindowPosition places the bridge outside the union of all
// connected displays. macOS may still constrain a native window back onto a
// display; concealPlatformPageWindow handles that fallback after creation.
func platformPageWindowPosition(displays []mygo.Display, width, height int) (int, int) {
	if len(displays) == 0 {
		return -width - platformPageWindowGap, -height - platformPageWindowGap
	}
	minX, minY := displays[0].Bounds.X, displays[0].Bounds.Y
	maxX := displays[0].Bounds.X + displays[0].Bounds.Width
	maxY := displays[0].Bounds.Y + displays[0].Bounds.Height
	for _, display := range displays[1:] {
		minX = minInt(minX, display.Bounds.X)
		minY = minInt(minY, display.Bounds.Y)
		maxX = maxInt(maxX, display.Bounds.X+display.Bounds.Width)
		maxY = maxInt(maxY, display.Bounds.Y+display.Bounds.Height)
	}
	return minX - width - platformPageWindowGap, minY - height - platformPageWindowGap
}

func rectanglesOverlap(a, b mygo.Rectangle) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func platformPageWindowScreen(bounds mygo.Rectangle, displays []mygo.Display) string {
	for _, display := range displays {
		if rectanglesOverlap(bounds, display.Bounds) {
			if display.Label != "" {
				return display.Label
			}
			return fmt.Sprintf("display-%d", display.ID)
		}
	}
	return "屏幕外"
}

func platformPageWindowNeedsFallback(bounds mygo.Rectangle, displays []mygo.Display) bool {
	return platformPageWindowScreen(bounds, displays) != "屏幕外"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func concealPlatformPageWindow(window *mygo.Window, planned mygo.Rectangle) {
	if window == nil || window.IsDestroyed() {
		return
	}
	displays := mygo.Screen.Displays()
	actual := window.Bounds()
	screen := platformPageWindowScreen(actual, displays)
	fallback := platformPageWindowNeedsFallback(actual, displays)
	// Opacity=0 is applied after creation because WindowOptions intentionally
	// normalizes non-positive opacity to 1. The native page remains composited,
	// while the bridge cannot be seen, focused, clicked, or captured.
	window.SetOpacity(0)
	window.SetIgnoreMouseEvents(true)
	window.SetContentProtection(true)
	window.Blur()
	slog.Info("平台请求窗口已隐藏", "frame", actual, "screen", screen, "planned", planned, "fallback", fallback)
}

const douyinFeedCaptureScript = `(function(){
  if (window.__tangentryDouyinFeedInstalled) return;
  window.__tangentryDouyinFeedInstalled = true;
  window.__tangentryDouyinFeed = window.__tangentryDouyinFeed || [];
  const max = 4 * 1024 * 1024;
  const isFeed = value => /\/aweme\/(?:v1|v2)\/[^?]*feed/i.test(String(value || ''));
  const absolute = raw => { try { return new URL(String(raw || ''), location.href).href; } catch (_) { return String(raw || ''); } };
  const push = (url, status, contentType, body) => {
    const resolvedURL = absolute(url);
    if (!isFeed(resolvedURL) || typeof body !== 'string' || !body || body.length > max) return;
    window.__tangentryDouyinFeed.push({url:resolvedURL, status:Number(status)||0, contentType:String(contentType || ''), body:body});
    if (window.__tangentryDouyinFeed.length > 32) window.__tangentryDouyinFeed.splice(0, window.__tangentryDouyinFeed.length - 32);
  };
  const originalFetch = window.fetch;
  const fetchWrapper = function(){
    const request = arguments[0];
    const url = typeof request === 'string' ? request : request && request.url;
    return originalFetch.apply(this, arguments).then(response => {
      if (!isFeed(url || response.url)) return response;
      try { const contentType = response.headers && response.headers.get ? response.headers.get('content-type') : ''; response.clone().text().then(body => push(url || response.url, response.status, contentType, body)).catch(() => {}); } catch (_) {}
      return response;
    });
  };
  window.__tangentryDouyinFetchWrapper = fetchWrapper;
  window.fetch = fetchWrapper;
  const open = XMLHttpRequest.prototype.open;
  const send = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function(method, url){ this.__tangentryURL = url; return open.apply(this, arguments); };
  const xhrSendWrapper = function(){
    this.addEventListener('load', function(){ try { const body = this.responseType === 'json' ? JSON.stringify(this.response) : this.responseText; push(this.__tangentryURL || this.responseURL, this.status, this.getResponseHeader('content-type'), body); } catch (_) {} });
    return send.apply(this, arguments);
  };
  window.__tangentryDouyinXHRSend = xhrSendWrapper;
  XMLHttpRequest.prototype.send = xhrSendWrapper;
})();`

const douyinFeedClearScript = `(function(){ if (window.__tangentryDouyinFeed) window.__tangentryDouyinFeed.splice(0); return true; })();`

const douyinFeedScrollScript = `(function(){ window.scrollTo(0, document.body ? document.body.scrollHeight : 0); return {pageUrl: location.href, title: document.title || "", ready: document.readyState}; })();`

const douyinDomScrollScript = `(function(){ window.scrollBy(0, Math.max(480, Math.floor(window.innerHeight * 0.85))); return {pageUrl: location.href, title: document.title || "", ready: document.readyState}; })();`

const douyinRecommendNextScript = `(function(){ const target=document.activeElement || document.body || document.documentElement; target.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',code:'ArrowDown',keyCode:40,which:40,bubbles:true})); target.dispatchEvent(new WheelEvent('wheel',{deltaY:Math.max(480,Math.floor(window.innerHeight*0.85)),bubbles:true})); return {pageUrl:location.href,title:document.title||'',ready:document.readyState}; })();`

const douyinFeedReadScript = `(function(){ return {pageUrl: location.href, title: document.title || "", ready: document.readyState, responses: (window.__tangentryDouyinFeed || []).splice(0, 32), fetchHookInstalled:window.fetch === window.__tangentryDouyinFetchWrapper, xhrHookInstalled:XMLHttpRequest.prototype.send === window.__tangentryDouyinXHRSend}; })();`

const douyinDomReadScript = `(function(){
  const text = node => (node && (node.innerText || node.textContent) || '').trim();
  const absolute = raw => { try { return new URL(raw || '', location.href).href; } catch (_) { return ''; } };
  const idFrom = raw => { const match = String(raw || '').match(/\/video\/([1-9][0-9]{15,19})/); return match ? match[1] : ''; };
  const imageFrom = node => {
    if (!node) return '';
    const image = node.querySelector('img, video, [data-e2e*="cover"], [class*="cover"]');
    const raw = image && (image.currentSrc || image.src || image.poster || image.getAttribute('data-src') || image.getAttribute('data-lazy-src') || image.getAttribute('data-url'));
    if (raw) return absolute(raw);
    const styled = node.querySelector('[style*="background-image"]');
    const match = styled && String(styled.getAttribute('style') || '').match(/url\(["']?([^"')]+)["']?\)/i);
    return absolute(match && match[1]);
  };
  const findText = (node, names) => { for (const name of names) { const found = node.querySelector(name); if (text(found)) return text(found); } return ''; };
  const durationOf = value => { const match = String(value || '').match(/(?:^|\s)([0-9]{1,2}):([0-5][0-9])(?:\s|$)/); return match ? Number(match[1]) * 60 + Number(match[2]) : 0; };
  const countOf = value => { const match = String(value || '').replace(/,/g, '').match(/([0-9]+(?:\.[0-9]+)?)\s*([万千wkKmM])?/i); if (!match) return 0; const number = Number(match[1]); const unit = String(match[2] || '').toLowerCase(); const multiplier = unit === '万' || unit === 'w' ? 10000 : unit === '千' || unit === 'k' ? 1000 : unit === 'm' ? 1000000 : 1; return Math.round(number * multiplier); };
  const selectors = ['a[href*="/video/"]','[data-e2e*="video"]','[class*="video-card"]','[class*="videoCard"]','[class*="feed-card"]'];
  const candidateSelectorHits = {}; for (const selector of selectors) { try { candidateSelectorHits[selector] = document.querySelectorAll(selector).length; } catch (_) { candidateSelectorHits[selector] = 0; } }
  const links = document.querySelectorAll('a[href*="/video/"]');
  const cards = []; const seen = new Set();
  for (const link of links) {
    const url = absolute(link.getAttribute('href') || link.href); const id = idFrom(url); if (!id || seen.has(id)) continue;
    let node = link; for (let i = 0; i < 6 && node.parentElement; i++) { node = node.parentElement; if (node.querySelectorAll('a[href*="/video/"]').length > 1 || node.querySelector('img, video, [data-e2e*="cover"], [class*="cover"], [style*="background-image"]')) break; }
    const rawText = text(node); const title = text(link) || findText(node, ['h1','h2','h3','[class*="title"]','[class*="desc"]']) || rawText.slice(0, 200);
    const author = findText(node, ['[class*="author"]','[class*="user"]','[data-e2e*="user"]','a[href*="/user/"]','a[href*="/search/"]']);
    const likeText = findText(node, ['[class*="like"]','[data-e2e*="like"]']);
    const duration = durationOf(findText(node, ['[class*="duration"]','[class*="time"]']) || rawText);
    cards.push({id:id, url:url.split('?')[0], thumbnailURL:imageFrom(node), title:title, author:author, likeCount:countOf(likeText), duration:duration}); seen.add(id);
  }
  const resources = performance.getEntriesByType('resource').filter(entry => /\/aweme\//i.test(entry.name)).map(entry => { try { return {path:new URL(entry.name).pathname, initiatorType:String(entry.initiatorType || '')}; } catch (_) { return null; } }).filter(Boolean);
  return {pageUrl:location.href, title:document.title || '', ready:document.readyState, cards:cards, responses:(window.__tangentryDouyinFeed || []).splice(0, 32), resources:resources, fetchHookInstalled:window.fetch === window.__tangentryDouyinFetchWrapper, xhrHookInstalled:XMLHttpRequest.prototype.send === window.__tangentryDouyinXHRSend, domDiagnostics:{url:location.href,videoLinkCount:links.length,candidateSelectorHits:candidateSelectorHits,cardsRead:cards.length}};
})();`

const searchDomReadScript = `(function(){
  const text = node => (node && (node.innerText || node.textContent) || '').trim();
  const absolute = raw => { try { return new URL(raw || '', location.href).href; } catch (_) { return ''; } };
  const idFrom = raw => { const value = String(raw || ''); let match = value.match(/\/video\/([1-9][0-9]{15,19})/); if (match) return {platform:'douyin',id:match[1]}; match = value.match(/\/(?:explore|discovery\/item)\/([A-Za-z0-9_-]{3,80})/); if (match) return {platform:'xiaohongshu',id:match[1]}; return null; };
  const authorIDFrom = raw => { const value = String(raw || ''); let match = value.match(/\/user\/profile\/([^/?#]+)/); if (!match) match = value.match(/\/user\/([^/?#]+)/); if (!match) return ''; try { return decodeURIComponent(match[1]); } catch (_) { return ''; } };
  const imageFrom = node => { const image = node && node.querySelector('img,video,[class*="cover"]'); const raw = image && (image.currentSrc || image.src || image.poster || image.getAttribute('data-src')); return absolute(raw); };
  const findText = (node, names) => { for (const name of names) { const found = node && node.querySelector(name); if (text(found)) return text(found); } return ''; };
  const cards = [], seen = new Set();
  for (const link of document.querySelectorAll('a[href]')) { const url = absolute(link.getAttribute('href') || link.href); const ref = idFrom(url); if (!ref || seen.has(ref.platform+':'+ref.id)) continue; let node = link; for (let i=0;i<6 && node.parentElement;i++) { node=node.parentElement; if (node.querySelector('img,video,[class*="cover"]') || node.querySelectorAll('a[href]').length > 2) break; } const authorLink = Array.from(node.querySelectorAll('a[href]')).map(candidate => ({candidate, url:absolute(candidate.getAttribute('href') || candidate.href)})).find(value => /\/user\//.test(value.url) || /\/profile\//.test(value.url)); const title = text(link) || findText(node,['h1','h2','h3','[class*="title"]','[class*="desc"]']) || text(node).slice(0,200); const author = findText(node,['[class*="author"]','[class*="user"]','[data-e2e*="user"]','a[href*="/user/"]','a[href*="/profile/"]']); cards.push({id:ref.platform+':'+ref.id,url:url.split('?')[0],thumbnailURL:imageFrom(node),title:title,author:author,authorID:authorIDFrom(authorLink && authorLink.url)}); seen.add(ref.platform+':'+ref.id); }
  return {pageUrl:location.href,title:document.title||'',ready:document.readyState,cards:cards};
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
	rawResult, err := evalPageWithRetry(ctx, session.window.Page(), code)
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
	responseURL := result.URL
	if responseURL == "" {
		responseURL = session.window.Page().URL()
	}
	return app.PlatformPageResponse{Status: result.Status, Body: result.Body, Restricted: isCloudflarePage(result.Status, result.Body, result.ContentType), URL: responseURL, ContentType: result.ContentType}, nil
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
	if err := session.window.Page().LoadURL("https://www.douyin.com/jingxuan"); err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	if err := waitForPageReady(ctx, session.window.Page(), "jingxuan"); err != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	if _, err := evalPageWithRetry(ctx, session.window.Page(), douyinFeedClearScript); err != nil && ctx.Err() != nil {
		return platformfetch.FeedCaptureResponse{}, err
	}
	result := douyinCaptureResult{}
	started := time.Now()
	var lastErr error
	deadline := time.NewTimer(douyinDOMWait)
	defer deadline.Stop()
	for i := 0; i < pages; i++ {
		if i > 0 {
			if scroll, err := evalPageWithRetry(ctx, session.window.Page(), douyinDomScrollScript); err == nil {
				_ = decodeDouyinCaptureResult(scroll, &result)
			} else {
				lastErr = err
			}
		}
		for {
			if err := waitCapturePoll(ctx); err != nil {
				return platformfetch.FeedCaptureResponse{}, err
			}
			beforeResponses, beforeCards := len(result.Responses), len(result.Cards)
			readScript := douyinDomReadScript
			if raw, err := evalPageWithRetry(ctx, session.window.Page(), readScript); err == nil {
				_ = decodeDouyinCaptureResult(raw, &result)
			} else {
				lastErr = err
			}
			if len(result.Responses) > beforeResponses || len(result.Cards) > beforeCards || len(result.Cards) >= 20 {
				break
			}
			select {
			case <-ctx.Done():
				return platformfetch.FeedCaptureResponse{}, ctx.Err()
			case <-deadline.C:
				break
			default:
			}
			if time.Since(started) >= douyinDOMWait {
				break
			}
		}
		if time.Since(started) >= douyinDOMWait {
			break
		}
	}
	for attempts := 0; attempts < 2; attempts++ {
		if err := waitCapturePoll(ctx); err != nil {
			return platformfetch.FeedCaptureResponse{}, err
		}
		raw, err := evalPageWithRetry(ctx, session.window.Page(), douyinFeedReadScript)
		if err != nil {
			lastErr = err
			continue
		}
		_ = decodeDouyinCaptureResult(raw, &result)
	}
	if len(result.Cards) == 0 && len(result.Responses) == 0 {
		if err := session.window.Page().LoadURL("https://www.douyin.com/?recommend=1"); err == nil {
			if err := waitForPageReady(ctx, session.window.Page(), "recommend=1"); err == nil {
				for i := 0; i < pages; i++ {
					if raw, err := evalPageWithRetry(ctx, session.window.Page(), douyinRecommendNextScript); err == nil {
						_ = decodeDouyinCaptureResult(raw, &result)
					}
					if err := waitCapturePoll(ctx); err != nil {
						return platformfetch.FeedCaptureResponse{}, err
					}
					if raw, err := evalPageWithRetry(ctx, session.window.Page(), douyinFeedReadScript); err == nil {
						_ = decodeDouyinCaptureResult(raw, &result)
					}
				}
			}
		}
	}
	if len(result.Responses) == 0 && lastErr != nil && ctx.Err() != nil {
		return platformfetch.FeedCaptureResponse{}, lastErr
	}
	responses := make([]platformfetch.Response, 0, len(result.Responses))
	for _, response := range result.Responses {
		if len(response.Body) > platformFetchMaxBody {
			continue
		}
		responses = append(responses, platformfetch.Response{Status: response.Status, Body: response.Body, URL: response.URL, ContentType: response.ContentType, Restricted: isCloudflarePage(response.Status, response.Body, response.ContentType)})
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
	result.DOM.WaitMilliseconds = int(time.Since(started) / time.Millisecond)
	return platformfetch.FeedCaptureResponse{Responses: responses, DOMCards: result.Cards, PageURL: pageURL, Title: result.Title, Ready: result.Ready, ResourcePaths: result.ResourcePaths, InitiatorTypes: result.InitiatorTypes, FetchHookInstalled: result.FetchHookInstalled, XHRHookInstalled: result.XHRHookInstalled, DOM: result.DOM}, nil
}

func (b *platformPageBridge) CaptureSearch(ctx context.Context, platform, query, cursor string) (platformfetch.SearchCaptureResponse, error) {
	origin, ok := platformPageOrigin(platform)
	if !ok || (platform != "douyin" && platform != "xiaohongshu") {
		return platformfetch.SearchCaptureResponse{}, errors.New("该平台不支持页面关键词搜索")
	}
	path, err := searchPageURL(platform, query, cursor)
	if err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	session, err := b.session(platform, origin)
	if err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := waitPlatformPage(ctx, session.ready); err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	if err := session.window.Page().LoadURL(path); err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	if err := waitForPageReady(ctx, session.window.Page(), "search"); err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	raw, err := evalPageWithRetry(ctx, session.window.Page(), searchDomReadScript)
	if err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	var result struct {
		PageURL string                  `json:"pageUrl"`
		Title   string                  `json:"title"`
		Ready   string                  `json:"ready"`
		Cards   []platformfetch.DOMCard `json:"cards"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return platformfetch.SearchCaptureResponse{}, err
	}
	return platformfetch.SearchCaptureResponse{DOMCards: result.Cards, PageURL: result.PageURL, Title: result.Title, Ready: result.Ready}, nil
}

func searchPageURL(platform, query, cursor string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 200 || strings.ContainsRune(query, '\x00') {
		return "", errors.New("页面关键词无效")
	}
	if cursor != "" {
		page, err := strconv.Atoi(cursor)
		if err != nil || page < 1 || page > 1000 {
			return "", errors.New("页面搜索游标无效")
		}
	}
	var raw string
	switch platform {
	case "douyin":
		raw = "https://www.douyin.com/search/" + url.PathEscape(query)
	case "xiaohongshu":
		raw = "https://www.xiaohongshu.com/search_result?keyword=" + url.QueryEscape(query)
	default:
		return "", errors.New("该平台不支持页面关键词搜索")
	}
	if cursor != "" {
		raw += "&page=" + url.QueryEscape(cursor)
	}
	return raw, nil
}

type douyinCaptureResponse struct {
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
}

type douyinCaptureResult struct {
	PageURL   string                  `json:"pageUrl"`
	Title     string                  `json:"title"`
	Ready     string                  `json:"ready"`
	Responses []douyinCaptureResponse `json:"responses"`
	Cards     []platformfetch.DOMCard `json:"cards"`
	Resources []struct {
		Path          string `json:"path"`
		InitiatorType string `json:"initiatorType"`
	} `json:"resources"`
	ResourcePaths      []string
	InitiatorTypes     []string
	FetchHookInstalled bool                         `json:"fetchHookInstalled"`
	XHRHookInstalled   bool                         `json:"xhrHookInstalled"`
	DOM                platformfetch.DOMDiagnostics `json:"domDiagnostics"`
}

func decodeDouyinCaptureResult(raw any, result *douyinCaptureResult) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var current douyinCaptureResult
	if err := json.Unmarshal(encoded, &current); err != nil {
		return err
	}
	if current.PageURL != "" {
		result.PageURL = current.PageURL
	}
	if current.Title != "" {
		result.Title = current.Title
	}
	if current.Ready != "" {
		result.Ready = current.Ready
	}
	for _, response := range current.Responses {
		if len(response.Body) > platformFetchMaxBody {
			continue
		}
		result.Responses = append(result.Responses, response)
	}
	if len(result.Responses) > 128 {
		result.Responses = result.Responses[len(result.Responses)-128:]
	}
	result.Cards = mergeDOMCards(result.Cards, current.Cards)
	if current.DOM.URL != "" {
		result.DOM = current.DOM
		result.DOM.CardsRead = len(result.Cards)
	}
	for _, resource := range current.Resources {
		if len(result.ResourcePaths) >= 256 {
			break
		}
		result.ResourcePaths = append(result.ResourcePaths, resource.Path)
		result.InitiatorTypes = append(result.InitiatorTypes, resource.InitiatorType)
	}
	var metadata struct {
		FetchHookInstalled *bool `json:"fetchHookInstalled"`
		XHRHookInstalled   *bool `json:"xhrHookInstalled"`
	}
	if err := json.Unmarshal(encoded, &metadata); err == nil {
		if metadata.FetchHookInstalled != nil {
			result.FetchHookInstalled = *metadata.FetchHookInstalled
		}
		if metadata.XHRHookInstalled != nil {
			result.XHRHookInstalled = *metadata.XHRHookInstalled
		}
	}
	return nil
}

func mergeDOMCards(existing, incoming []platformfetch.DOMCard) []platformfetch.DOMCard {
	merged := append([]platformfetch.DOMCard(nil), existing...)
	indexes := make(map[string]int, len(merged)+len(incoming))
	for index, card := range merged {
		indexes[card.ID] = index
	}
	for _, card := range incoming {
		if index, ok := indexes[card.ID]; ok {
			current := &merged[index]
			if current.URL == "" {
				current.URL = card.URL
			}
			if current.ThumbnailURL == "" {
				current.ThumbnailURL = card.ThumbnailURL
			}
			if current.Title == "" {
				current.Title = card.Title
			}
			if current.Author == "" {
				current.Author = card.Author
			}
			if current.LikeCount == 0 {
				current.LikeCount = card.LikeCount
			}
			if current.Duration == 0 {
				current.Duration = card.Duration
			}
			continue
		}
		if len(merged) >= 500 {
			break
		}
		indexes[card.ID] = len(merged)
		merged = append(merged, card)
	}
	return merged
}

func waitCapturePoll(ctx context.Context) error {
	timer := time.NewTimer(douyinCapturePoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForPageReady(ctx context.Context, page *mygo.Page, pathHint string) error {
	deadline := time.NewTimer(platformFetchTimeout)
	defer deadline.Stop()
	for {
		result, err := evalPageWithRetry(ctx, page, `(function(){ return {url:location.href, ready:document.readyState}; })()`)
		if err == nil {
			encoded, marshalErr := json.Marshal(result)
			if marshalErr == nil {
				var state struct {
					URL   string `json:"url"`
					Ready string `json:"ready"`
				}
				if json.Unmarshal(encoded, &state) == nil && state.Ready == "complete" {
					// A challenge can replace /jingxuan with the home page or a
					// same-origin verification route. Return as soon as the document
					// is complete so diagnostics can report that final location.
					_ = pathHint
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if err != nil {
				return err
			}
			return context.DeadlineExceeded
		default:
		}
		if err := waitCapturePoll(ctx); err != nil {
			return err
		}
	}
}

func evalPageWithRetry(ctx context.Context, page *mygo.Page, code string) (any, error) {
	return evalWithRetry(ctx, page.EvalContext, code)
}

func evalWithRetry(ctx context.Context, eval func(context.Context, string) (any, error), code string) (any, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result, err := eval(ctx, code)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isPageContextGoneError(err) {
			return nil, err
		}
		if err := waitCapturePoll(ctx); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func isPageContextGoneError(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "completion handler") || strings.Contains(value, "context") && (strings.Contains(value, "destroy") || strings.Contains(value, "navigation") || strings.Contains(value, "reachable"))
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
	x, y := platformPageWindowPosition(mygo.Screen.Displays(), platformPageWindowWidth, platformPageWindowHeight)
	planned := mygo.Rectangle{X: x, Y: y, Width: platformPageWindowWidth, Height: platformPageWindowHeight}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "Tangentry 平台请求", X: x, Y: y,
		Width: platformPageWindowWidth, Height: platformPageWindowHeight, MinWidth: 640, MinHeight: 480,
		Frameless: true, Transparent: true, Panel: true,
		DisableResize: true, DisableMove: true, DisableMinimize: true, DisableMaximize: true,
		DisableClose: true, DisableFullScreen: true, SkipTaskbar: true,
		Page: mygo.PageOptions{TrustedOrigins: []string{origin}, PreloadScript: func() string {
			if platform == "douyin" {
				return douyinFeedCaptureScript
			}
			return ""
		}()},
	})
	concealPlatformPageWindow(window, planned)
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
	case "xiaohongshu":
		return "https://www.xiaohongshu.com/", true
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
	case "xiaohongshu":
		return host == "www.xiaohongshu.com"
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
	Status      int    `json:"status"`
	Body        string `json:"body"`
	TooLarge    bool   `json:"tooLarge"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
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
	return fmt.Sprintf(`const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), %d); try { return await fetch(location.origin + %s, {method:%s, headers:%s, credentials:"include"%s, signal:controller.signal}).then(async response => { const max=%d; const meta={status:response.status, url:response.url || "", contentType:response.headers.get("content-type") || ""}; if (!response.body) { const body=await response.text(); return {...meta, body:body.length>max?"":body, tooLarge:body.length>max}; } const reader=response.body.getReader(); const decoder=new TextDecoder(); let body="", bytes=0; try { for (;;) { const part=await reader.read(); if (part.done) break; bytes+=part.value.byteLength; if (bytes>max) { await reader.cancel(); return {...meta, body:"", tooLarge:true}; } body+=decoder.decode(part.value,{stream:true}); } body+=decoder.decode(); return {...meta, body:body, tooLarge:false}; } finally { reader.releaseLock(); } }); } finally { clearTimeout(timer); }`, platformFetchTimeout.Milliseconds(), pathJSON, method, headers, bodyOption, platformFetchMaxBody), nil
}

func isCloudflarePage(status int, body string, contentTypes ...string) bool {
	contentType := ""
	if len(contentTypes) > 0 {
		contentType = strings.ToLower(strings.TrimSpace(contentTypes[0]))
	}
	// A valid JSON response is never a Cloudflare interstitial, even if a
	// harmless field happens to contain the word "cloudflare".
	if json.Valid([]byte(body)) || strings.Contains(contentType, "json") {
		return false
	}
	if contentType != "" && !strings.Contains(contentType, "text/html") {
		return false
	}
	value := strings.ToLower(body)
	if !strings.Contains(contentType, "text/html") {
		return false
	}
	return strings.Contains(value, "just a moment") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "cf-chl-") || strings.Contains(value, "cloudflare")
}
