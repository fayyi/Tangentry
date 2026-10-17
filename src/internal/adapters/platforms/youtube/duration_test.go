package youtube

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"tangentry/internal/content"
	"testing"
)

func TestWatchDuration(t *testing.T) {
	for _, test := range []struct {
		body string
		want int
	}{
		{`<meta itemprop="duration" content="PT12M34S">`, 754},
		{`<meta content="PT1H2M3S" itemprop="duration"/>`, 3723},
		{`<meta itemprop="duration" content="PT90M">`, 5400},
		{`<meta itemprop="duration" content="PT90S">`, 90},
		{`<meta itemprop="duration" content="PT169H">`, 0},
		{`<meta itemprop="duration" content="PT999999999999999999999H">`, 0},
		{`<meta itemprop="duration" content="LIVE">`, 0},
		{`<html>consent</html>`, 0},
	} {
		if got := watchDuration(strings.NewReader(test.body)); got != test.want {
			t.Errorf("%s: got %d, want %d", test.body, got, test.want)
		}
	}
}

func TestDurationEnrichmentCachesSuccessAndPreservesUnavailableItems(t *testing.T) {
	var calls atomic.Int32
	c := New(&http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		status, body := 200, `<meta itemprop="duration" content="PT12M34S">`
		if r.URL.Query().Get("v") == "blocked0001" {
			status, body = 403, ""
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	items := []content.Item{
		{Platform: "youtube", PlatformID: "abcdefghijk", URL: "https://www.youtube.com/watch?v=abcdefghijk"},
		{Platform: "youtube", PlatformID: "blocked0001", URL: "https://www.youtube.com/watch?v=blocked0001"},
	}
	c.EnrichDurations(context.Background(), items)
	if items[0].DurationSeconds != 754 || items[1].DurationSeconds != 0 {
		t.Fatal(items)
	}
	items[0].DurationSeconds = 0
	c.EnrichDurations(context.Background(), items[:1])
	if items[0].DurationSeconds != 754 || calls.Load() != 2 {
		t.Fatal("duration not cached", items, calls.Load())
	}
}
