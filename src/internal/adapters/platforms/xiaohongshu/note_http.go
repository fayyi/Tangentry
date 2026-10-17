package xiaohongshu

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
		if c.UserAgent != nil && c.UserAgent(locator) != "" {
			userAgent = c.UserAgent(locator)
		}
		req.Header.Set("User-Agent", userAgent)
		if authenticated && c.Cookie != nil {
			req.Header.Set("Cookie", c.Cookie(locator))
		}
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
			return content.Item{}, &discovery.PlatformError{Message: "分享链接没有返回小红书单条笔记，请复制包含分享访问参数的完整地址。"}
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
	if err == nil {
		c.remember(item)
	}
	return item, err
}

func platformHost(host string) bool {
	return host == "www.xiaohongshu.com" || host == "xiaohongshu.com" || host == "xhslink.com" || host == "www.xhslink.com"
}

func noteMismatch() error {
	return &discovery.PlatformError{Message: "分享链接与当前笔记不一致，请复制这篇笔记的链接。"}
}

func requestError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &discovery.PlatformError{Message: "小红书页面暂时无法连接，请检查链接与网络。"}
}

func unavailable() error {
	return &discovery.PlatformError{Message: "小红书笔记不可访问，请使用完整分享地址，或在工具设置检查小红书 Cookie。"}
}

func pageLocationError(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "www.xiaohongshu.com" && u.Hostname() != "xiaohongshu.com") {
		return nil
	}
	switch strings.TrimRight(u.Path, "/") {
	case "/login", "/website-login":
		return &discovery.PlatformError{Message: "小红书要求登录才能读取正文和图片，请在设置中导入当前登录信息后重试。"}
	case "/404":
		return &discovery.PlatformError{Message: "小红书没有返回这篇笔记，请从原站重新复制完整分享链接。"}
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
		return &discovery.PlatformError{Message: "小红书暂时限制内容请求，请稍后重试。", Restricted: true}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return &discovery.PlatformError{Message: "小红书拒绝了当前访问，请在原站确认笔记可访问并完成验证，再检查完整分享地址与 Cookie。"}
	}
	return unavailable()
}
