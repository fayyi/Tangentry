package discovery

import (
	"net/url"
	"regexp"
	"strings"
	"tangentry/internal/content"
)

var sharedLink = regexp.MustCompile(`https?://[^\s<>"，。]+`)
var shortCode = regexp.MustCompile(`^[A-Za-z0-9_-]{3,60}$`)

type DouyinReference struct{ ID, URL string }

// ParseDouyin accepts one video, including a single URL pasted in share text.
// Short links are locators resolved separately without credentials.
func ParseDouyin(raw string) (DouyinReference, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return DouyinReference{}, ErrInvalidQuery
	}
	links := sharedLink.FindAllString(raw, -1)
	if len(links) != 1 {
		return DouyinReference{}, ErrInvalidQuery
	}
	u, err := url.Parse(strings.TrimRight(links[0], "),;!！）"))
	if err != nil || u.User != nil || u.Port() != "" || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return DouyinReference{}, ErrInvalidQuery
	}
	host := strings.ToLower(u.Hostname())
	if host == "v.douyin.com" && shortCode.MatchString(strings.Trim(u.Path, "/")) {
		return DouyinReference{URL: "https://v.douyin.com/" + strings.Trim(u.Path, "/") + "/"}, nil
	}
	if host != "douyin.com" && host != "www.douyin.com" {
		return DouyinReference{}, ErrInvalidQuery
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	id := ""
	if len(parts) == 2 && parts[0] == "video" {
		id = parts[1]
	}
	if u.Path == "/" || u.Path == "" {
		query, err := url.ParseQuery(u.RawQuery)
		if err == nil && len(query["modal_id"]) == 1 {
			id = query.Get("modal_id")
		}
	}
	if !content.ValidID("douyin:" + id) {
		return DouyinReference{}, ErrInvalidQuery
	}
	return DouyinReference{ID: id, URL: "https://www.douyin.com/video/" + id}, nil
}
