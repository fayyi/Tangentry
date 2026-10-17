package discovery

import (
	"net/url"
	"regexp"
	"strings"
	"tangentry/internal/content"
)

type XiaohongshuReference struct{ ID, URL string }

var xhsShortCode = regexp.MustCompile(`^[A-Za-z0-9_-]{3,80}$`)

// Retain only access parameters supplied by the share link. They are locators,
// not account credentials; identity is always the note ID.
func ParseXiaohongshu(raw string) (XiaohongshuReference, error) {
	if len(raw) > 2048 {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	links := sharedLink.FindAllString(strings.TrimSpace(raw), -1)
	if len(links) != 1 {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	u, err := url.Parse(strings.TrimRight(links[0], "),;!！）"))
	if err != nil || u.User != nil || u.Port() != "" || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	host := strings.ToLower(u.Hostname())
	if host == "xhslink.com" || host == "www.xhslink.com" {
		path := strings.Trim(u.Path, "/")
		parts := strings.Split(path, "/")
		code := path
		if len(parts) == 2 && (parts[0] == "a" || parts[0] == "o" || parts[0] == "m") {
			code = parts[1]
		} else if len(parts) != 1 {
			return XiaohongshuReference{}, ErrInvalidQuery
		}
		if !xhsShortCode.MatchString(code) {
			return XiaohongshuReference{}, ErrInvalidQuery
		}
		return XiaohongshuReference{URL: "https://xhslink.com/" + path}, nil
	}
	if host != "www.xiaohongshu.com" && host != "xiaohongshu.com" {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := ""
	if len(parts) == 2 && parts[0] == "explore" {
		id = parts[1]
	}
	if len(parts) == 3 && parts[0] == "discovery" && parts[1] == "item" {
		id = parts[2]
	}
	if !content.ValidID("xiaohongshu:" + id) {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return XiaohongshuReference{}, ErrInvalidQuery
	}
	access := url.Values{}
	for _, key := range []string{"xsec_token", "xsec_source"} {
		if len(query[key]) > 1 || len(query.Get(key)) > 512 {
			return XiaohongshuReference{}, ErrInvalidQuery
		}
		if v := query.Get(key); v != "" {
			access.Set(key, v)
		}
	}
	canonical := "https://www.xiaohongshu.com/explore/" + id
	if len(access) > 0 {
		canonical += "?" + access.Encode()
	}
	return XiaohongshuReference{ID: id, URL: canonical}, nil
}

// Access tokens may change between share links, while a video remains the same.
func SameVideo(a, b string) bool {
	ap, ai, _, ae := VideoURL(a)
	bp, bi, _, be := VideoURL(b)
	return ae == nil && be == nil && ap == bp && ai == bi
}
