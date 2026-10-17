package discovery

import (
	"net/url"
	"regexp"
	"strings"
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func ParseYouTube(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", ErrInvalidQuery
	}
	host := strings.ToLower(u.Hostname())
	id := ""
	if host == "youtu.be" {
		id = strings.Trim(u.Path, "/")
	} else if host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" {
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed") {
				id = parts[1]
			}
		}
	}
	if !youtubeID.MatchString(id) {
		return "", "", ErrInvalidQuery
	}
	return id, "https://www.youtube.com/watch?v=" + id, nil
}
func VideoURL(raw string) (string, string, string, error) {
	if ref, err := ParseXiaohongshu(raw); err == nil && ref.ID != "" {
		return "xiaohongshu", ref.ID, ref.URL, nil
	}
	if ref, err := ParseDouyin(raw); err == nil && ref.ID != "" {
		return "douyin", ref.ID, ref.URL, nil
	}
	if id, u, err := ParseYouTube(raw); err == nil {
		return "youtube", id, u, nil
	}
	kind, id, err := ParseQuery(raw)
	if err != nil || kind != "video" {
		return "", "", "", ErrInvalidQuery
	}
	return "bilibili", id, "https://www.bilibili.com/video/" + id + "/", nil
}
