package xiaoyuzhou

import (
	"context"
	"io"
	"net/http"
	"strings"
	"tangentry/internal/content"
	"testing"
)

func TestCollectEpisodesOnlyEmitsConcreteEpisodes(t *testing.T) {
	var items []content.Item
	collectEpisodes(map[string]any{"type": "DISCOVERY_HEADER", "data": []any{"标题"}, "nested": map[string]any{
		"eid": "686000000000000000000001", "title": "测试节目", "duration": 120.0, "podcast": map[string]any{"title": "测试播客", "pid": "podcast-1"}, "image": map[string]any{"picUrl": "https://example.com/image.jpg"},
	}}, &items, map[string]bool{})
	if len(items) != 1 || items[0].Title != "测试节目" {
		t.Fatalf("unexpected items %#v", items)
	}
}

func TestEpisodeImageFallsBackToPodcastCoverAndNormalizesScheme(t *testing.T) {
	item, ok := episodeItem(map[string]any{
		"eid": "686000000000000000000001", "title": "没有单集封面",
		"podcast": map[string]any{"title": "测试播客", "pid": "p1", "image": "//image.xyzcdn.net/podcast.jpg"},
	})
	if !ok || item.ThumbnailURL != "https://image.xyzcdn.net/podcast.jpg" {
		t.Fatalf("podcast cover fallback missing: %+v", item)
	}
	item, ok = episodeItem(map[string]any{
		"eid": "686000000000000000000002", "title": "嵌套封面",
		"image": map[string]any{"url": "http://image.xyzcdn.net/episode.jpg"},
	})
	if !ok || item.ThumbnailURL != "https://image.xyzcdn.net/episode.jpg" {
		t.Fatalf("nested cover normalization missing: %+v", item)
	}
}

func TestRecommendationTimeAcceptsSecondsMillisecondsAndRFC3339(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int64
	}{
		{float64(1791648507), 1791648507},
		{float64(1791648507000), 1791648507},
		{"2026-10-10T12:34:56Z", 1791635696},
		{"1791648507000", 1791648507},
		{"not-a-date", 0},
		{float64(1), 0},
	} {
		if got := parseRecommendationTime(tc.value); got != tc.want {
			t.Fatalf("parseRecommendationTime(%v) = %d, want %d", tc.value, got, tc.want)
		}
	}
}

func TestNestedPodcastImageFallback(t *testing.T) {
	for _, image := range []any{
		map[string]any{"picUrl": "//image.xyzcdn.net/podcast.jpg"},
		map[string]any{"thumbnailUrl": "https://image.xyzcdn.net/podcast.jpg"},
	} {
		item, ok := episodeItem(map[string]any{
			"eid": "686000000000000000000001", "title": "节目封面",
			"imageUrl": "invalid",
			"podcast":  map[string]any{"image": image},
		})
		if !ok || item.ThumbnailURL != "https://image.xyzcdn.net/podcast.jpg" {
			t.Fatalf("nested podcast image missing: %+v", item)
		}
	}
}

func TestEpisodeItemMapsRecommendationPublishedAt(t *testing.T) {
	item, ok := episodeItem(map[string]any{
		"eid": "686000000000000000000003", "title": "带日期的节目",
		"pubDate": "2026-10-10T12:34:56Z",
	})
	if !ok || item.PublishedAt != 1791635696 {
		t.Fatalf("recommendation date missing: %+v", item)
	}
}

func TestRecommendationRetainsOnlyPublicOfficialAudio(t *testing.T) {
	for _, tt := range []struct {
		url     string
		private bool
		want    string
	}{
		{"https://media.xyzcdn.net/opaque-audio", false, "https://media.xyzcdn.net/opaque-audio"},
		{"https://tk.wavpub.com/audio", false, "https://tk.wavpub.com/audio"},
		{"https://tk.wavpub.com/audio", true, ""},
		{"https://media.xyzcdn.net/opaque-audio", true, ""},
		{"https://untrusted.example/audio.mp3", false, ""},
	} {
		item, ok := episodeItem(map[string]any{"eid": "686000000000000000000001", "title": "测试节目",
			"isPrivateMedia": tt.private, "enclosure": map[string]any{"url": tt.url}})
		if !ok || item.MediaURL != tt.want {
			t.Fatalf("unexpected public audio: %+v", item)
		}
	}
}

func TestRecommendationsParsesEpisodeAndRejectsMissingToken(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("x-jike-access-token") == "" {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}
		body := `{"data":[{"type":"DISCOVERY_HEADER","data":["header"]},{"type":"PRESET_CONTENT","data":{"contents":[{"eid":"686000000000000000000001","title":"测试节目","duration":120,"podcast":{"title":"测试播客","pid":"p1"},"image":{"picUrl":"https://example.com/image.jpg"}}]}}],"loadMoreKey":"next"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	feed, err := client.Recommendations(context.Background(), 1, "token")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Kind != "audio" || !feed.HasMore {
		t.Fatalf("unexpected feed %#v %v", feed, err)
	}
	if _, err = client.Recommendations(context.Background(), 1, ""); err == nil {
		t.Fatal("missing token accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
