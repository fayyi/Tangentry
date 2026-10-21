package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"tangentry/internal/app"
)

// platformLoginBridge is the only WebView-to-Go bridge exposed by the native
// login window. It never returns Cookie or User-Agent values to JavaScript.
// A single active window also keeps the shared MyGo cookie profile unambiguous.
type platformLoginBridge struct {
	mu          sync.Mutex
	credentials app.PlatformCredentialsPort
	active      *platformLoginAttempt
}

// platformLoginService is intentionally a separate bound value: only
// Complete is reachable from a platform page. The HTTP-triggered Open method
// is kept out of the JavaScript bridge.
type platformLoginService struct{ bridge *platformLoginBridge }

func (s platformLoginService) Complete(ctx context.Context) error { return s.bridge.Complete(ctx) }

type platformLoginAttempt struct {
	platform string
	origin   string
	save     func([]*http.Cookie, string) error
	window   *mygo.Window
}

func newPlatformLoginBridge(credentials app.PlatformCredentialsPort) *platformLoginBridge {
	return &platformLoginBridge{credentials: credentials}
}

// Open starts a platform-scoped login window. The saver is created before the
// window opens, so a later logout or newer import invalidates this attempt.
func (b *platformLoginBridge) Open(ctx context.Context, platform string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	origin, ok := platformLoginOrigin(platform)
	if !ok {
		return errors.New("unsupported platform login")
	}
	save := b.credentials.PrepareBrowserLogin(platform)
	b.mu.Lock()
	if b.active != nil {
		b.mu.Unlock()
		return errors.New("platform login already open")
	}
	attempt := &platformLoginAttempt{platform: platform, origin: origin, save: save}
	b.active = attempt
	b.mu.Unlock()

	window := mygo.NewWindow(mygo.WindowOptions{
		Title: platformLoginTitle(platform), URL: "about:blank",
		Width: 1080, Height: 760, MinWidth: 720, MinHeight: 520,
		BackgroundColor: "#171717",
		Page: mygo.PageOptions{
			TrustedOrigins: platformLoginTrustedOrigins(platform),
			PreloadScript:  platformLoginPreload,
		},
	})
	b.mu.Lock()
	if b.active != attempt {
		b.mu.Unlock()
		window.Close()
		return errors.New("platform login was cancelled")
	}
	attempt.window = window
	b.mu.Unlock()

	page := window.Page()
	page.OnPageTitleUpdated(func(event *mygo.TitleEvent) { event.PreventDefault() })
	page.OnWillNavigate(func(event *mygo.NavigateEvent) {
		if platformLoginURLAllowed(event.URL, platform) {
			return
		}
		event.PreventDefault()
	})
	// Do not hand links clicked inside the login surface to the system browser.
	// The window is an isolated credential flow, not a general-purpose browser.
	page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
	window.OnClosed(func() { b.cancel(attempt) })
	page.LoadURL(origin)
	return nil
}

func (b *platformLoginBridge) cancel(attempt *platformLoginAttempt) {
	b.mu.Lock()
	if b.active == attempt {
		b.active = nil
	}
	b.mu.Unlock()
}

// Complete is called by the native toolbar button. HttpOnly cookies are read
// through MyGo's cookie store, never through document.cookie or page JavaScript.
func (b *platformLoginBridge) Complete(ctx context.Context) error {
	b.mu.Lock()
	attempt := b.active
	b.mu.Unlock()
	if attempt == nil {
		return errors.New("platform login is not open")
	}
	page := mygo.CallerPage(ctx)
	if page == nil || !platformLoginURLAllowed(page.URL(), attempt.platform) {
		return errors.New("login page is outside the platform allowlist")
	}
	var cookies []mygo.Cookie
	for _, domain := range platformLoginCookieDomains(attempt.platform) {
		values, err := mygo.Cookies.List(mygo.CookieFilter{Domain: domain})
		if err != nil {
			return errors.New("无法读取平台登录状态")
		}
		cookies = append(cookies, values...)
	}
	if len(cookies) == 0 {
		return errors.New("无法读取平台登录状态")
	}
	converted := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		converted = append(converted, &http.Cookie{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Expires: cookie.Expires, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
		})
	}
	userAgent := page.UserAgent()
	if err := attempt.save(converted, userAgent); err != nil {
		return err
	}
	b.mu.Lock()
	if b.active == attempt {
		b.active = nil
	}
	b.mu.Unlock()
	if attempt.window != nil {
		go attempt.window.Close()
	}
	return nil
}

func platformLoginOrigin(platform string) (string, bool) {
	switch platform {
	case "bilibili":
		return "https://passport.bilibili.com/h5-app/passport/login", true
	case "linuxdo":
		return "https://linux.do/", true
	case "douyin":
		return "https://www.douyin.com/", true
	case "xiaohongshu":
		return "https://www.xiaohongshu.com/", true
	case "youtube":
		return "https://accounts.google.com/", true
	default:
		return "", false
	}
}

func platformLoginCookieDomains(platform string) []string {
	switch platform {
	case "bilibili":
		return []string{"bilibili.com"}
	case "linuxdo":
		return []string{"linux.do"}
	case "douyin":
		return []string{"douyin.com"}
	case "xiaohongshu":
		return []string{"xiaohongshu.com"}
	case "youtube":
		return []string{"youtube.com", "google.com"}
	default:
		return nil
	}
}

func platformLoginTitle(platform string) string {
	if platform == "linuxdo" {
		return "Linux.do 登录"
	}
	labels := map[string]string{"douyin": "抖音登录", "bilibili": "B站登录", "xiaohongshu": "小红书登录", "youtube": "YouTube 登录"}
	return labels[platform]
}

func platformLoginTrustedOrigins(platform string) []string {
	if platform == "linuxdo" {
		return []string{"https://linux.do", "https://www.linux.do", "https://challenges.cloudflare.com", "https://turnstile.cloudflare.com", "https://static.cloudflareinsights.com"}
	}
	if platform == "douyin" {
		return []string{"https://douyin.com", "https://www.douyin.com", "https://passport.douyin.com", "https://www.iesdouyin.com", "https://aweme.snssdk.com", "https://challenges.cloudflare.com", "https://turnstile.cloudflare.com", "https://static.cloudflareinsights.com"}
	}
	if platform == "bilibili" {
		return []string{"https://bilibili.com", "https://www.bilibili.com", "https://passport.bilibili.com", "https://account.bilibili.com", "https://captcha.bilibili.com", "https://s1.hdslb.com"}
	}
	if platform == "xiaohongshu" {
		return []string{"https://xiaohongshu.com", "https://www.xiaohongshu.com", "https://edith.xiaohongshu.com", "https://sso.xiaohongshu.com", "https://captcha.xiaohongshu.com"}
	}
	if platform == "youtube" {
		return []string{"https://accounts.google.com", "https://www.google.com", "https://myaccount.google.com", "https://ogs.google.com", "https://accounts.youtube.com", "https://youtube.com", "https://www.youtube.com", "https://consent.youtube.com", "https://www.recaptcha.net", "https://recaptcha.net"}
	}
	return []string{"https://douyin.com", "https://www.douyin.com", "https://passport.douyin.com", "https://www.iesdouyin.com", "https://challenges.cloudflare.com", "https://turnstile.cloudflare.com", "https://static.cloudflareinsights.com"}
}

func platformLoginURLAllowed(rawURL, platform string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "challenges.cloudflare.com" || host == "turnstile.cloudflare.com" || host == "static.cloudflareinsights.com" {
		return true
	}
	if platform == "linuxdo" {
		return host == "linux.do" || host == "www.linux.do"
	}
	switch platform {
	case "douyin":
		return host == "douyin.com" || host == "www.douyin.com" || host == "passport.douyin.com" || host == "www.iesdouyin.com" || host == "aweme.snssdk.com"
	case "bilibili":
		return host == "bilibili.com" || host == "www.bilibili.com" || host == "passport.bilibili.com" || host == "account.bilibili.com" || host == "captcha.bilibili.com" || host == "s1.hdslb.com"
	case "xiaohongshu":
		return host == "xiaohongshu.com" || host == "www.xiaohongshu.com" || host == "edith.xiaohongshu.com" || host == "sso.xiaohongshu.com" || host == "captcha.xiaohongshu.com"
	case "youtube":
		return host == "accounts.google.com" || host == "www.google.com" || host == "myaccount.google.com" || host == "ogs.google.com" || host == "accounts.youtube.com" || host == "youtube.com" || host == "www.youtube.com" || host == "consent.youtube.com" || host == "www.recaptcha.net" || host == "recaptcha.net"
	default:
		return false
	}
}

const platformLoginPreload = `(function(){
  function install(){
    if (!document.body || document.getElementById('tangentry-platform-login')) return;
    const bar=document.createElement('div'); bar.id='tangentry-platform-login';
    bar.style='position:fixed;z-index:2147483647;right:16px;bottom:16px;padding:10px 12px;background:#171717;color:#fff;border:1px solid #555;border-radius:8px;font:13px system-ui,sans-serif;box-shadow:0 4px 18px #0008;display:flex;align-items:center;gap:10px';
    const text=document.createElement('span'); text.textContent='登录或验证完成后点击此处';
    const button=document.createElement('button'); button.type='button'; button.textContent='完成登录'; button.style='border:0;border-radius:6px;padding:6px 10px;background:#fff;color:#111;cursor:pointer';
    button.addEventListener('click',async()=>{button.disabled=true;button.textContent='正在保存…';try{await mygo.call('PlatformLogin.Complete')}catch(error){button.disabled=false;button.textContent='重试';text.textContent=String(error&&error.message||'未能保存登录状态')}});
    function checkGoogleBlock(){
      const body=(document.body&&document.body.innerText||'').toLowerCase();
      if(location.hostname.endsWith('google.com') && (body.includes('this browser or app may not be secure') || body.includes('此浏览器或应用可能不安全'))){
        text.textContent='Google 拒绝了嵌入式浏览器登录，请改用浏览器 Cookie 或 OAuth。Tangentry 不会绕过此安全检查。';
        button.disabled=true;
      }
    }
    bar.append(text,button); document.body.appendChild(bar); checkGoogleBlock();
    new MutationObserver(checkGoogleBlock).observe(document.body,{subtree:true,childList:true,characterData:true});
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded',install,{once:true}); else install();
})();`
