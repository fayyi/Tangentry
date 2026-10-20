package playback

import (
	"testing"
)

func TestNormalizeDouyinSourceUpgradesAndFiltersSharedCDNs(t *testing.T) {
	source, err := normalizeSource(Source{
		Platform: "douyin",
		URL:      "http://media.zjcdn.com/video.mp4",
		Backups:  []string{"http://v26-web.douyinvod.com/backup.mp4", "https://media.example.com/video.mp4"},
	})
	if err != nil || source.URL != "https://media.zjcdn.com/video.mp4" || len(source.Backups) != 1 || source.Backups[0] != "https://v26-web.douyinvod.com/backup.mp4" {
		t.Fatalf("Douyin source normalization = %+v, %v", source, err)
	}
	if _, err := normalizeSource(Source{Platform: "douyin", URL: "https://media.example.com/video.mp4"}); err == nil {
		t.Fatal("non-allowlisted Douyin source was accepted")
	}
}

func TestDouyinSourceDeadlineRecognizesXExpiresAndExpires(t *testing.T) {
	for _, raw := range []string{
		"https://v1.douyinvod.com/video.mp4?x-expires=1",
		"https://v1.douyinvod.com/video.mp4?expires=1",
	} {
		if _, err := normalizeSource(Source{Platform: "douyin", URL: raw}); err == nil {
			t.Fatalf("expired source was accepted: %s", raw)
		}
	}
	if _, err := normalizeSource(Source{Platform: "bilibili", URL: "https://media.bilivideo.com/video.mp4?x-expires=1"}); err != nil {
		t.Fatalf("Bilibili source semantics changed: %v", err)
	}
}
