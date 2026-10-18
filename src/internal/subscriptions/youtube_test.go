package subscriptions_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/feeds"
	youtubeadapter "tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/readstate"
	"tangentry/internal/subscriptions"
	"testing"
	"time"
)

const channelID = "UCaaaaaaaaaaaaaaaaaaaaaa"

func channelSnapshot(title string) subscriptions.Snapshot {
	return subscriptions.Snapshot{Title: "fixture channel", Items: []content.Item{{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", AuthorID: channelID, Author: "fixture", Title: title, URL: "https://www.youtube.com/watch?v=abcdefghijk"}}, Total: 1}
}
func TestChannelResolutionAndRegistrationAreSeparateAndStable(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "channel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	calls := 0
	service := subscriptions.NewService(store, nil, subscriptions.WithYouTubeResolver(func(_ context.Context, url string) (discovery.YouTubeChannel, error) {
		calls++
		if url != "https://www.youtube.com/@fixture" {
			t.Fatal("uncanonical handle", url)
		}
		return discovery.YouTubeChannel{ID: channelID, Title: "public fixture", URL: discovery.YouTubeChannelURL(channelID)}, nil
	}))
	defer service.Close()
	channel, err := service.ResolveYouTube(ctx, "https://youtube.com/@fixture/shorts?share=ignored")
	if err != nil || calls != 1 || channel.ID != channelID {
		t.Fatal(channel, err)
	}
	sources, _ := service.List(ctx)
	if len(sources) != 0 {
		t.Fatal("resolve saved source")
	}
	direct, err := service.ResolveYouTube(ctx, channelID)
	if err != nil || direct.ID != channelID || calls != 1 {
		t.Fatal("direct ID accessed platform")
	}
	if _, _, err = service.Create(ctx, subscriptions.Input{Type: "youtube", Query: "@fixture"}); !errors.Is(err, subscriptions.ErrChannelInput) {
		t.Fatal("handle saved without canonical identity")
	}
	first, created, err := service.Create(ctx, subscriptions.Input{Type: "youtube", Query: channel.ID, Name: "my channel"})
	if err != nil || !created || first.ID != "youtube:channel:"+channelID || first.URL != channel.URL || !first.Refresh.Enabled || first.Refresh.IntervalSeconds != subscriptions.DefaultInterval || calls != 1 {
		t.Fatal(first, err)
	}
	for _, input := range []subscriptions.Input{{Type: "youtube", Query: channel.URL + "/videos", Name: "should not rename"}, {Type: "youtube", Query: discovery.YouTubeChannelFeed(channelID)}, {Type: "rss", Query: discovery.YouTubeChannelFeed(channelID)}} {
		duplicate, created, err := service.Create(ctx, input)
		if err != nil || created || duplicate.ID != first.ID || duplicate.Name != first.Name || duplicate.CreatedAt != first.CreatedAt {
			t.Fatal("duplicate identity", duplicate, created, err)
		}
	}
	for _, query := range []string{"https://www.youtube.com/watch?v=abcdefghijk", "https://www.youtube.com/playlist?list=foo", "https://evil.example/channel/" + channelID, "UCinvalid", "https://www.youtube.com/channel/" + channelID + "/extra"} {
		if _, _, err = service.Create(ctx, subscriptions.Input{Type: "youtube", Query: query}); !errors.Is(err, subscriptions.ErrChannelInput) {
			t.Fatal("invalid channel created", query, err)
		}
	}
	if _, err = service.Get(ctx, "youtube:channel:invalid"); !errors.Is(err, subscriptions.ErrInvalid) {
		t.Fatal("invalid source ID")
	}
}
func TestYouTubeChannelRefreshSharesCacheAndScheduleWithoutSideEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	ctx := context.Background()
	now := time.Unix(1800000000, 0)
	calls := 0
	result := subscriptions.ConditionalResult{Snapshot: channelSnapshot("first"), Validators: subscriptions.Validators{ETag: `"channel"`, ResourceURL: discovery.YouTubeChannelFeed(channelID)}}
	var failure error
	service := subscriptions.NewService(store, nil, subscriptions.WithClock(func() time.Time { return now }), subscriptions.WithConditionalFeeds(func(_ context.Context, identity string, v subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		calls++
		if identity != channelID {
			t.Fatal("wrong source identity", identity)
		}
		return result, failure
	}))
	defer service.Close()
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "youtube", Query: channelID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Update(ctx, source.ID)
	if err != nil || first.Source.Refresh.ContentRevision != 1 || calls != 1 {
		t.Fatal(first, err)
	}
	_, err = readstate.New(store).Set(ctx, []string{first.Items[0].ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	gallery, err := service.Gallery(ctx)
	if err != nil || len(gallery.Items) != 1 || gallery.Items[0].Platform != "youtube" || calls != 1 {
		t.Fatal("gallery refetched/changed identity", gallery, err)
	}
	library, _ := store.List(ctx)
	if len(library) != 0 {
		t.Fatal("channel fetched into library")
	}
	now = now.Add(time.Minute)
	result.NotModified = true
	same, err := service.Update(ctx, source.ID)
	if err != nil || !same.NotModified || same.Source.LastUpdatedAt != first.Source.LastUpdatedAt {
		t.Fatal("304 changed content", same, err)
	}
	policy, err := service.ConfigureRefresh(ctx, source.ID, subscriptions.RefreshPolicy{Enabled: true, IntervalSeconds: 900})
	if err != nil || policy.Refresh.NextCheckAt != now.Unix()+900 || calls != 2 {
		t.Fatal("enable fetched", policy, err)
	}
	now = now.Add(900 * time.Second)
	_ = service.RefreshDue(ctx, now)
	if calls != 3 {
		t.Fatal("channel not scheduled")
	}
	failure = discovery.ErrUnavailable
	if _, err = service.Update(ctx, source.ID); err == nil {
		t.Fatal("channel failure hidden")
	}
	cached, _ := service.Get(ctx, source.ID)
	if len(cached.Items) != 1 || cached.Source.LastUpdatedAt != first.Source.LastUpdatedAt || !strings.HasPrefix(cached.Source.Refresh.Message, "YouTube 频道") {
		t.Fatal("failure reset cache", cached)
	}
	failure = nil
	result.NotModified = false
	result.Snapshot.Items[0].AuthorID = "UCbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
		t.Fatal("wrong channel cached", err)
	}
	result.Snapshot = channelSnapshot("valid")
	result.Snapshot.Items[0].Kind = "article"
	if _, err = service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
		t.Fatal("nonvideo cached", err)
	}
	result.Snapshot = channelSnapshot("valid")
	result.Snapshot.Items[0].URL = "https://www.youtube.com/watch?v=different11"
	if _, err = service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
		t.Fatal("wrong video link cached", err)
	}
	service.Close()
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetSubscription(ctx, source.ID)
	if err != nil || persisted.Source.URL != source.URL || persisted.Source.CreatorID != channelID || !persisted.Source.Refresh.Enabled || len(persisted.Items) != 1 {
		t.Fatal("channel lost across reopen", persisted, err)
	}
	if err = store.DeleteSubscription(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	states, _ := readstate.New(store).Batch(ctx, []string{first.Items[0].ID})
	if len(states) != 1 || !states[0].Read {
		t.Fatal("channel delete reset state")
	}
}

func TestYouTubeRefreshUsesRSSFallbackAndStableIDs(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "fallback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const rssFeed = `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015"><yt:channelId>aaaaaaaaaaaaaaaaaaaaaa</yt:channelId><title>fixture channel</title><author><name>fixture</name><uri>https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa</uri></author><entry><id>yt:video:abcdefghijk</id><yt:videoId>abcdefghijk</yt:videoId><yt:channelId>UCaaaaaaaaaaaaaaaaaaaaaa</yt:channelId><title>first</title><link rel="alternate" href="https://www.youtube.com/watch?v=abcdefghijk"/><author><name>fixture</name><uri>https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa</uri></author><published>2026-10-01T12:00:00Z</published></entry></feed>`
	feed := discovery.Feed{Page: 1, Query: channelID, SourceTitle: "fixture channel", Total: 2, Items: []content.Item{
		{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", AuthorID: channelID, Author: "fixture", Title: "first", URL: "https://www.youtube.com/watch?v=abcdefghijk", PublishedAt: 1},
		{ID: "youtube:lmnopqrstuv", Platform: "youtube", PlatformID: "lmnopqrstuv", Kind: "video", AuthorID: channelID, Author: "fixture", Title: "second", URL: "https://www.youtube.com/watch?v=lmnopqrstuv", PublishedAt: 2},
	}}
	rssCalls := 0
	client := youtubeadapter.New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rssCalls++
		if rssCalls == 1 {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/atom+xml"}}, Body: io.NopCloser(strings.NewReader(rssFeed)), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("404")), Request: r}, nil
	})})
	var fallbackErr error
	fallbackCalls := 0
	client.SetChannelFallback(func(context.Context, string) (discovery.Feed, error) {
		fallbackCalls++
		if fallbackErr != nil {
			return discovery.Feed{}, fallbackErr
		}
		return feed, nil
	})
	service := subscriptions.NewService(store, nil, subscriptions.WithConditionalFeeds(func(ctx context.Context, id string, v subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		result, err := client.FetchConditional(ctx, id, feeds.Validators{ETag: v.ETag, LastModified: v.LastModified, ResourceURL: v.ResourceURL})
		return subscriptions.ConditionalResult{Snapshot: subscriptions.Snapshot{Items: result.Feed.Items, Title: result.Feed.SourceTitle, Total: result.Feed.Total}, NotModified: result.NotModified}, err
	}))
	defer service.Close()
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "youtube", Query: channelID})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctx, source.ID)
	if err != nil || len(updated.Items) != 1 || updated.Items[0].ID != "youtube:abcdefghijk" || fallbackCalls != 0 {
		t.Fatalf("initial RSS snapshot not saved: %+v fallbackCalls=%d err=%v", updated, fallbackCalls, err)
	}
	updated, err = service.Update(ctx, source.ID)
	if err != nil || len(updated.Items) != 2 || updated.Items[0].ID != "youtube:abcdefghijk" || updated.Items[1].ID != "youtube:lmnopqrstuv" || updated.Items[0].ID == updated.Items[1].ID || fallbackCalls != 1 {
		t.Fatalf("fallback snapshot lost stable IDs or duplicated RSS content: %+v fallbackCalls=%d err=%v", updated, fallbackCalls, err)
	}
	fallbackErr = errors.New("yt-dlp 退出码 1")
	if _, err = service.Update(ctx, source.ID); err == nil {
		t.Fatal("double failure hidden")
	}
	failed, err := service.Get(ctx, source.ID)
	if err != nil || strings.Contains(failed.Source.Refresh.SuggestedAction, "RSSHub") || !strings.Contains(failed.Source.Refresh.SuggestedAction, "YouTube") {
		t.Fatalf("YouTube failure suggested RSSHub or lost platform guidance: %+v err=%v", failed.Source.Refresh, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestChannelResolverSharesLimitsAndShutdown(t *testing.T) {
	store, _ := sqlite.Open(filepath.Join(t.TempDir(), "limits.db"))
	defer store.Close()
	entered := make(chan struct{}, 2)
	done := make(chan error, 2)
	service := subscriptions.NewService(store, nil, subscriptions.WithYouTubeResolver(func(ctx context.Context, _ string) (discovery.YouTubeChannel, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return discovery.YouTubeChannel{}, ctx.Err()
	}))
	for range 2 {
		go func() { _, err := service.ResolveYouTube(context.Background(), "@fixture"); done <- err }()
		<-entered
	}
	if _, err := service.ResolveYouTube(context.Background(), "@third"); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("resolver global limit")
	}
	service.Close()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("resolver shutdown not canceled", err)
		}
	}
	if _, err := service.ResolveYouTube(context.Background(), channelID); !errors.Is(err, context.Canceled) {
		t.Fatal("closed resolver accepts calls")
	}
}
