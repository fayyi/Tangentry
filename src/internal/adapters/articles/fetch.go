package articles

import (
	"context"
	"io"
	"mime"
	"net/http"

	"tangentry/internal/articles"
	"tangentry/internal/netpolicy"
)

// fetch performs the bounded anonymous HTTP exchange and charset conversion.
func (e *Extractor) fetch(ctx context.Context, raw string) (fetchedPage, error) {
	canonical, err := netpolicy.URL(raw)
	if err != nil {
		return fetchedPage{}, articles.ErrInvalid
	}
	client := *e.HTTP
	client.Jar = nil
	prior := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return articles.ErrUnavailable
		}
		if prior != nil {
			if err := prior(next, via); err != nil {
				return err
			}
		}
		if _, err := netpolicy.URL(next.URL.String()); err != nil {
			return err
		}
		next.Header.Del("Cookie")
		next.Header.Del("Authorization")
		next.Header.Del("Referer")
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, canonical, nil)
	req.Header.Set("User-Agent", "Tangentry/0.1 Article Reader")
	req.Header.Set("Accept", "text/html, application/xhtml+xml")
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fetchedPage{}, ctx.Err()
		}
		return fetchedPage{}, failure("无法连接原站，请检查地址与网络后重试。")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fetchedPage{}, failure("原站未提供正文，可能需要登录或暂时限制访问，可打开原内容查看。")
	}
	kind, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if kind != "" && kind != "text/html" && kind != "application/xhtml+xml" {
		return fetchedPage{}, failure("原地址没有返回网页正文，请打开原内容查看。")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxHTML+1))
	if err != nil {
		if ctx.Err() != nil {
			return fetchedPage{}, ctx.Err()
		}
		return fetchedPage{}, articles.ErrUnavailable
	}
	if len(data) > maxHTML {
		return fetchedPage{}, failure("原网页超过 16 MB，请打开原内容查看。")
	}
	if kind == "" && !isWebDocument(data) {
		return fetchedPage{}, articles.ErrUnavailable
	}
	reader, err := articleReader(data, res.Header.Get("Content-Type"))
	if err != nil {
		return fetchedPage{}, articles.ErrUnavailable
	}
	// Conversion can expand single-byte charsets, so bound its output separately.
	decoded, err := io.ReadAll(io.LimitReader(reader, maxHTML+1))
	if err != nil || len(decoded) > maxHTML {
		return fetchedPage{}, articles.ErrUnavailable
	}
	base := req.URL
	if res.Request != nil {
		base = res.Request.URL
	}
	final, err := netpolicy.URL(base.String())
	if err != nil {
		return fetchedPage{}, articles.ErrUnavailable
	}
	return fetchedPage{decoded: decoded, finalURL: final}, nil
}
