package platformfetch

import (
	"testing"
	"time"
)

func TestNormalizeDouyinMediaURL(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	for _, test := range []struct {
		name, raw, want, reason string
	}{
		{"upgrade", "http://v26-web.douyinvod.com/video.mp4", "https://v26-web.douyinvod.com/video.mp4", ""},
		{"zjcdn", "https://media.zjcdn.com/video.mp4", "https://media.zjcdn.com/video.mp4", ""},
		{"vod net", "https://media.douyinvod.net/video.mp4", "https://media.douyinvod.net/video.mp4", ""},
		{"unsafe host", "https://media.example.com/video.mp4", "", "host_not_allowed"},
		{"unsafe scheme", "ftp://v1.douyinvod.com/video.mp4", "", "scheme"},
		{"opaque scheme", "javascript:fixture", "", "scheme"},
		{"expired x expires", "https://v1.douyinvod.com/video.mp4?x-expires=1", "", "expired"},
		{"expired expires", "https://v1.douyinvod.com/video.mp4?expires=1", "", "expired"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, reason := NormalizeDouyinMediaURL(test.raw, now)
			if got != test.want || reason != test.reason {
				t.Fatalf("NormalizeDouyinMediaURL(%q) = %q, %q; want %q, %q", test.raw, got, reason, test.want, test.reason)
			}
		})
	}
}
