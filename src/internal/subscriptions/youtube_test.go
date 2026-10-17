package subscriptions_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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
