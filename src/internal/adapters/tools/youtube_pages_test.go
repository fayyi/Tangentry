package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/discovery"
)

func TestYouTubePagesCacheRetryAndAccountIsolation(t *testing.T) {
	root := t.TempDir()
	page := func(_ context.Context, _ string, continuation json.RawMessage) youtube.HomeResult {
		next := continuation != nil
		result := youtube.HomeResult{Mode: "account", Entries: []youtube.HomeEntry{{ID: "jNQXAC9IVRw", Title: "Actual page", Author: "Fixture", Duration: 10}}}
		if next {
			result.Entries[0].ID = "abcdefghijk"
		} else {
			result.Continuation = json.RawMessage(`{"token":"private-next-token"}`)
		}
		return result
	}
	failing := func(context.Context, string, json.RawMessage) youtube.HomeResult {
		return youtube.HomeResult{Error: "network"}
	}
	var mu sync.Mutex
	fetch := page
	use := func(next func(context.Context, string, json.RawMessage) youtube.HomeResult) {
		mu.Lock()
		fetch = next
		mu.Unlock()
	}
	kit := New(config.Config{})
	kit.youtubeHome = func(ctx context.Context, cookie string, continuation json.RawMessage) youtube.HomeResult {
		mu.Lock()
		current := fetch
		mu.Unlock()
		return current(ctx, cookie, continuation)
	}
	jar, err := credentials.Open(filepath.Join(root, "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	kit.SetCredentials(jar)
	ctx := context.Background()
	first, err := kit.YouTubeRecommendationPage(ctx, 1)
	if err != nil || !first.HasMore || first.Page != 1 {
		t.Fatal(first, err)
	}
	use(failing)
	if _, err = kit.YouTubeRecommendationPage(ctx, 1); err == nil {
		t.Fatal("failed refresh hidden")
	}
	if len(kit.youtubePages.pages) != 1 || len(kit.youtubePages.next) == 0 {
		t.Fatal("failed refresh discarded the displayed feed continuation")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = kit.YouTubeRecommendationPage(canceled, 1); err != context.Canceled {
		t.Fatal("canceled refresh did not stop before changing state", err)
	}
	if len(kit.youtubePages.pages) != 1 {
		t.Fatal("canceled refresh discarded the displayed feed")
	}
	if _, err = kit.YouTubeRecommendationPage(ctx, 2); err == nil {
		t.Fatal("failed continuation hidden")
	}
	use(page)
	second, err := kit.YouTubeRecommendationPage(ctx, 2)
	if err != nil || second.HasMore || second.Page != 2 || second.Items[0].ID == first.Items[0].ID {
		t.Fatal(second, err)
	}
	use(failing)
	if cached, err := kit.YouTubeRecommendationPage(ctx, 2); err != nil || cached.Items[0].ID != second.Items[0].ID {
		t.Fatal("cached page refetched", err)
	}
	jar.Save("youtube", "header", "SAPISID=changed-fixture")
	if _, err := kit.YouTubeRecommendationPage(ctx, 2); err == nil {
		t.Fatal("old account page survived")
	}
	use(page)
	if _, err := kit.YouTubeRecommendationPage(ctx, 1); err != nil {
		t.Fatal(err)
	}
	kit.youtubePages.expires = time.Now().Add(-time.Second)
	if _, err := kit.YouTubeRecommendationPage(ctx, 2); err == nil {
		t.Fatal("expired continuation survived")
	}
	if _, err := kit.YouTubeRecommendationPage(ctx, 1); err != nil {
		t.Fatal(err)
	}
	use(func(context.Context, string, json.RawMessage) youtube.HomeResult {
		return youtube.HomeResult{Mode: "account", Entries: []youtube.HomeEntry{}, Continuation: json.RawMessage(`{"token":"after-empty-page"}`)}
	})
	empty, err := kit.YouTubeRecommendationPage(ctx, 2)
	if err != nil || len(empty.Items) != 0 || !empty.HasMore {
		t.Fatal("empty batch incorrectly ended recommendations", empty, err)
	}
	use(page)
	third, err := kit.YouTubeRecommendationPage(ctx, 3)
	if err != nil || len(third.Items) != 1 || third.Page != 3 {
		t.Fatal("could not continue beyond empty batch", third, err)
	}
	var started, release chan struct{}
	slow := func(ctx context.Context, _ string, _ json.RawMessage) youtube.HomeResult {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return youtube.HomeResult{Error: "network"}
		}
		return youtube.HomeResult{Entries: []youtube.HomeEntry{{ID: "jNQXAC9IVRw", Title: "Delayed old account", Duration: 10}}, Continuation: json.RawMessage(`{"token":"late-token"}`)}
	}
	waitStarted := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("slow fetch never started")
		}
	}
	started, release = make(chan struct{}), make(chan struct{})
	use(slow)
	cancelCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() { _, err := kit.YouTubeRecommendationPage(cancelCtx, 1); result <- err }()
	waitStarted()
	stop()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("in-flight cancellation succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled fetch did not stop")
	}
	if len(kit.youtubePages.pages) != 3 {
		t.Fatal("in-flight cancellation replaced current pages")
	}
	started = make(chan struct{})
	go func() { _, err := kit.YouTubeRecommendationPage(ctx, 1); result <- err }()
	waitStarted()
	if err := jar.Save("youtube", "header", "SAPISID=new-account-fixture"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("old account response committed after Cookie change")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released fetch did not finish")
	}
	if len(kit.youtubePages.pages) != 0 || len(kit.youtubePages.next) != 0 {
		t.Fatal("old account data retained")
	}
	use(page)
	windowA := discovery.WithRecommendationSession(ctx, "window-a-fixture-session")
	windowB := discovery.WithRecommendationSession(ctx, "window-b-fixture-session")
	if _, err := kit.YouTubeRecommendationPage(windowA, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.YouTubeRecommendationPage(windowA, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.YouTubeRecommendationPage(windowB, 1); err != nil {
		t.Fatal(err)
	}
	use(failing)
	if cached, err := kit.YouTubeRecommendationPage(windowA, 2); err != nil || cached.Page != 2 {
		t.Fatal("another window replaced the first window's feed", err)
	}
	use(page)
	for index := 0; index < 6; index++ {
		window := discovery.WithRecommendationSession(ctx, fmt.Sprintf("additional-window-%d", index))
		if _, err := kit.YouTubeRecommendationPage(window, 1); err != nil {
			t.Fatal(err)
		}
	}
	if len(kit.youtubeSessions) != 8 {
		t.Fatal("fixture did not fill the session capacity")
	}
	freshWindow := discovery.WithRecommendationSession(ctx, "fresh-account-window")
	if _, err := kit.YouTubeRecommendationPage(freshWindow, 1); err == nil {
		t.Fatal("active session capacity ignored")
	}
	if err := jar.Save("youtube", "header", "SAPISID=replacement-account-fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.YouTubeRecommendationPage(freshWindow, 1); err != nil {
		t.Fatal("old account sessions blocked the replacement account", err)
	}
	if len(kit.youtubeSessions) != 1 || kit.youtubeSessions["fresh-account-window"] == nil {
		t.Fatal("old account session caches were not reclaimed")
	}
}
