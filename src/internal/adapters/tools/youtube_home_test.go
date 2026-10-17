package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
)

func TestHomeAdapterCookiePrivacyAndSanitizedErrors(t *testing.T) {
	root := t.TempDir()
	jar, err := credentials.Open(filepath.Join(root, "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	jar.Save("youtube", "header", "SAPISID=synthetic-private-secret")
	kit := New(config.Config{})
	kit.SetCredentials(jar)
	if status := kit.YouTubeRecommendationStatus(); !status.Available {
		t.Fatal("built-in InnerTube adapter reported unavailable", status)
	}
	kit.youtubeHome = func(_ context.Context, cookie string, continuation json.RawMessage) youtube.HomeResult {
		if cookie != "SAPISID=synthetic-private-secret" || continuation != nil {
			t.Fatal("adapter lost its scoped cookie", cookie, continuation)
		}
		return youtube.HomeResult{Entries: []youtube.HomeEntry{{ID: "jNQXAC9IVRw", Title: "Home recommendation", Author: "Fixture", AuthorID: "UC" + strings.Repeat("a", 22), Thumbnail: "https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg", Duration: 19, PublishedText: "3 days ago"}}}
	}
	feed, err := kit.YouTubeRecommendations(context.Background())
	if err != nil || len(feed.Items) != 1 || feed.SourceTitle != "YouTube 首页推荐" {
		t.Fatal(feed, err)
	}
	if feed.Items[0].AuthorID != "UC"+strings.Repeat("a", 22) {
		t.Fatal("recommendation lost its channel identity", feed.Items[0].AuthorID)
	}
	if feed.Items[0].PublishedText != "3 days ago" || feed.Items[0].PublishedAt != 0 {
		t.Fatal("publication label lost or guessed", feed.Items[0])
	}
	for _, code := range []string{"auth", "restricted", "network", "empty", "home_nudge", "batch_size"} {
		kit.youtubeHome = func(context.Context, string, json.RawMessage) youtube.HomeResult {
			return youtube.HomeResult{Error: code}
		}
		_, err := kit.YouTubeRecommendations(context.Background())
		if err == nil || strings.Contains(err.Error(), "synthetic-private-secret") {
			t.Fatal("failure hidden or leaked")
		}
		if code == "auth" && (!strings.Contains(err.Error(), "登录状态") || strings.Contains(err.Error(), "观看历史")) {
			t.Fatal("rejected cookies must not be diagnosed as missing history", err)
		}
		if code == "batch_size" && (!strings.Contains(err.Error(), "批次") || strings.Contains(err.Error(), "网络")) {
			t.Fatal("oversized batch misdiagnosed", err)
		}
	}
}
