package douyin

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxPlaybackURLLength = 8192

// PlaybackURL reports whether a URL is a short-lived, public Douyin media
// address. This boundary is deliberately narrower than the page origin.
func PlaybackURL(raw string) bool {
	if len(raw) == 0 || len(raw) > maxPlaybackURLLength {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host == "douyinvod.com" || strings.HasSuffix(host, ".douyinvod.com")
}

func playbackURLLive(raw string, now time.Time) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	deadline := u.Query().Get("deadline")
	if deadline == "" {
		return true
	}
	value, err := strconv.ParseInt(deadline, 10, 64)
	return err == nil && value > now.Unix()
}

func playbackURLDeadline(raw string) time.Time {
	u, err := url.Parse(raw)
	if err != nil {
		return time.Time{}
	}
	value, err := strconv.ParseInt(u.Query().Get("deadline"), 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
