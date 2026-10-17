package subscriptions_test

import (
	"context"
	"path/filepath"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestGalleryUsesMixedSnapshotsWithoutFetchingOrCollecting(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "gallery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	calls := 0
	service := subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) {
		calls++
		t.Error("gallery fetched remote content")
		return subscriptions.Snapshot{}, nil
	})
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	unupdated, _, _ := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/unupdated"})
	_ = unupdated
	items := []content.Item{}
	for i, kind := range []string{"audio", "article", "video", "pdf", "image"} {
		id := feeds.Hash(kind)
		items = append(items, content.Item{ID: "rss:" + id, Platform: "rss", PlatformID: id, Kind: kind, Title: kind, URL: "https://example.com/" + kind, PublishedAt: int64(i + 1)})
	}
	if _, err := store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: items, Total: 5}, 100); err != nil {
		t.Fatal(err)
	}
	feed, err := service.Gallery(ctx)
	if err != nil || len(feed.Items) != 5 || calls != 0 || feed.Items[0].Kind != "image" || feed.Items[4].Kind != "audio" {
		t.Fatal(feed, err, calls)
	}
	if library, _ := store.List(ctx); len(library) != 0 {
		t.Fatal("gallery collected content")
	}
	if err := service.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	feed, err = service.Gallery(ctx)
	if err != nil || len(feed.Items) != 0 {
		t.Fatal("removed source remains in gallery", feed, err)
	}
}
