package douyin

import (
	"time"

	"tangentry/internal/platformfetch"
)

// PlaybackURL reports whether a URL is a short-lived, public Douyin media
// address. This boundary is deliberately narrower than the page origin.
func PlaybackURL(raw string) bool {
	_, reason := platformfetch.NormalizeDouyinMediaURL(raw, time.Now())
	return reason == ""
}

func playbackURLLive(raw string, now time.Time) bool {
	_, reason := platformfetch.NormalizeDouyinMediaURL(raw, now)
	return reason == ""
}

func playbackURLDeadline(raw string) time.Time {
	return platformfetch.DouyinMediaDeadline(raw)
}
