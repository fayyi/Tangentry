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
	domain := "linux.do"
	if attempt.platform == "douyin" {
		domain = "douyin.com"
	}
	cookies, err := mygo.Cookies.List(mygo.CookieFilter{Domain: domain})
	if err != nil {
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
	case "linuxdo":
		return "https://linux.do/", true
	case "douyin":
		return "https://www.douyin.com/", true
	default:
		return "", false
	}
}

func platformLoginTitle(platform string) string {
	if platform == "linuxdo" {
		return "Linux.do 登录"
	}
	return "抖音登录"
}

func platformLoginTrustedOrigins(platform string) []string {
	if platform == "linuxdo" {
		return []string{"https://linux.do", "https://www.linux.do", "https://challenges.cloudflare.com", "https://turnstile.cloudflare.com", "https://static.cloudflareinsights.com"}
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
	return host == "douyin.com" || host == "www.douyin.com" || host == "passport.douyin.com" || host == "www.iesdouyin.com"
}

const platformLoginPreload = `(function(){
  function install(){
    if (!document.body || document.getElementById('tangentry-platform-login')) return;
    const bar=document.createElement('div'); bar.id='tangentry-platform-login';
    bar.style='position:fixed;z-index:2147483647;right:16px;bottom:16px;padding:10px 12px;background:#171717;color:#fff;border:1px solid #555;border-radius:8px;font:13px system-ui,sans-serif;box-shadow:0 4px 18px #0008;display:flex;align-items:center;gap:10px';
    const text=document.createElement('span'); text.textContent='登录或验证完成后点击此处';
    const button=document.createElement('button'); button.type='button'; button.textContent='完成登录'; button.style='border:0;border-radius:6px;padding:6px 10px;background:#fff;color:#111;cursor:pointer';
    button.addEventListener('click',async()=>{button.disabled=true;button.textContent='正在保存…';try{await mygo.call('PlatformLogin.Complete')}catch(error){button.disabled=false;button.textContent='重试';text.textContent=String(error&&error.message||'未能保存登录状态')}});
    bar.append(text,button); document.body.appendChild(bar);
  }
  if(document.readyState==='loading') document.addEventListener('DOMContentLoaded',install,{once:true}); else install();
})();`
