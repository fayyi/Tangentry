// Package douyin resolves public share locators; media extraction is infrastructure.
package douyin

import (
	"context"
	"io"
	"net/http"
	"strings"
	"tangentry/internal/discovery"
	"time"
)

type Client struct {
	HTTP      *http.Client
	Cookie    func(string) string
	UserAgent func(string) string
}

func (c *Client) Resolve(ctx context.Context, raw string) (discovery.DouyinReference, error) {
	ref, err := discovery.ParseDouyin(raw)
	if err != nil || ref.ID != "" {
		return ref, err
	}
	if c.HTTP == nil {
		return discovery.DouyinReference{}, discovery.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := *c.HTTP
	prior := client.CheckRedirect
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		host := strings.ToLower(r.URL.Hostname())
		if len(via) > 5 || (host != "www.douyin.com" && host != "douyin.com" && host != "v.douyin.com") || r.URL.User != nil || r.URL.Scheme != "https" || r.URL.Port() != "" {
			return discovery.ErrInvalidQuery
		}
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		if prior != nil {
			return prior(r, via)
		}
		return nil
	}
	client.Jar = nil
	req, err := http.NewRequestWithContext(ctx, "GET", ref.URL, nil)
	if err != nil {
		return discovery.DouyinReference{}, discovery.ErrInvalidQuery
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if c.UserAgent != nil {
		if userAgent := c.UserAgent(ref.URL); userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return discovery.DouyinReference{}, ctx.Err()
		}
		return discovery.DouyinReference{}, &discovery.PlatformError{Message: "抖音分享链接暂时无法展开，请粘贴原站 /video/ 完整地址。"}
	}
	defer resp.Body.Close()
	// Only the confirmed final locator is needed; do not parse or retain page HTML.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return discovery.DouyinReference{}, &discovery.PlatformError{Message: "抖音分享链接不可访问，请检查链接与网络。"}
	}
	result, err := discovery.ParseDouyin(resp.Request.URL.String())
	if err != nil || result.ID == "" {
		return discovery.DouyinReference{}, &discovery.PlatformError{Message: "分享链接没有返回单视频地址，请打开原站并复制 /video/ 地址。"}
	}
	return result, nil
}
