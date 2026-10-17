package discovery

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var youtubeChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

// A handle is a locator; only a confirmed UC ID identifies a saved channel.
type YouTubeChannelReference struct {
	ID  string
	URL string
}
type YouTubeChannel struct {
	ID    string `json:"channelId"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func YouTubeChannelID(id string) bool    { return youtubeChannelID.MatchString(id) }
func YouTubeChannelURL(id string) string { return "https://www.youtube.com/channel/" + id }
func YouTubeChannelFeed(id string) string {
	return "https://www.youtube.com/feeds/videos.xml?channel_id=" + id
}

func validHandle(raw string) bool {
	if !utf8.ValidString(raw) || !strings.HasPrefix(raw, "@") {
		return false
	}
	handle := strings.TrimPrefix(raw, "@")
	if utf8.RuneCountInString(handle) < 1 || utf8.RuneCountInString(handle) > 30 {
		return false
	}
	for i, r := range []rune(handle) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			continue
		}
		if (r == '_' || r == '-' || r == '.' || r == '·') && i > 0 && i < len([]rune(handle))-1 {
			continue
		}
		return false
	}
	return true
}
func ParseYouTubeChannel(raw string) (YouTubeChannelReference, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 || !utf8.ValidString(raw) {
		return YouTubeChannelReference{}, ErrInvalidQuery
	}
	if YouTubeChannelID(raw) {
		return YouTubeChannelReference{ID: raw, URL: YouTubeChannelURL(raw)}, nil
	}
	if validHandle(raw) {
		return YouTubeChannelReference{URL: "https://www.youtube.com/" + url.PathEscape(raw)}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() != "" || u.Opaque != "" {
		return YouTubeChannelReference{}, ErrInvalidQuery
	}
	host := strings.ToLower(u.Hostname())
	if host != "www.youtube.com" && host != "youtube.com" && host != "m.youtube.com" {
		return YouTubeChannelReference{}, ErrInvalidQuery
	}
	if u.Path == "/feeds/videos.xml" {
		query, err := url.ParseQuery(u.RawQuery)
		ids := query["channel_id"]
		if err != nil || len(query) != 1 || len(ids) != 1 || !YouTubeChannelID(ids[0]) {
			return YouTubeChannelReference{}, ErrInvalidQuery
		}
		return YouTubeChannelReference{ID: ids[0], URL: YouTubeChannelURL(ids[0])}, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id, handle string
	if len(parts) >= 2 && parts[0] == "channel" && YouTubeChannelID(parts[1]) {
		id = parts[1]
		parts = parts[2:]
	} else if len(parts) >= 1 && validHandle(parts[0]) {
		handle = parts[0]
		parts = parts[1:]
	} else {
		return YouTubeChannelReference{}, ErrInvalidQuery
	}
	if len(parts) > 1 || len(parts) == 1 && parts[0] != "videos" && parts[0] != "shorts" && parts[0] != "streams" && parts[0] != "featured" && parts[0] != "about" {
		return YouTubeChannelReference{}, ErrInvalidQuery
	}
	if id != "" {
		return YouTubeChannelReference{ID: id, URL: YouTubeChannelURL(id)}, nil
	}
	return YouTubeChannelReference{URL: "https://www.youtube.com/" + url.PathEscape(handle)}, nil
}
