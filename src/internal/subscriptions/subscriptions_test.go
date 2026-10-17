package subscriptions_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/subscriptions"
	"testing"
)

func video() content.Item {
	return content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "subscription fixture", AuthorID: "12345", URL: "https://www.bilibili.com/video/BV17x411w7KC/", SavedAt: 99}
}

func TestSourcesPersistIndependentlyAndFailedUpdatesKeepLastSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscriptions.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	ctx := context.Background()
	calls := 0
	snapshot := subscriptions.Snapshot{Items: []content.Item{video()}, Title: "creator", Total: 25, HasMore: true}
	var failure error
	service := subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) { calls++; return snapshot, failure })
	source, created, err := service.Create(ctx, subscriptions.Input{Query: "https://space.bilibili.com/12345?from=test", Name: "  我的来源  "})
	if err != nil || !created || calls != 0 || source.Name != "我的来源" {
		t.Fatalf("source creation should be local: %+v %v", source, err)
	}
	duplicate, created, err := service.Create(ctx, subscriptions.Input{Query: "12345", Name: "changed"})
	if err != nil || created || duplicate.ID != source.ID || duplicate.Name != source.Name || duplicate.CreatedAt != source.CreatedAt {
		t.Fatal("duplicate source changed identity/name")
	}
	detail, err := service.Update(ctx, source.ID)
	if err != nil || detail.Source.LastUpdatedAt == 0 || len(detail.Items) != 1 || detail.Items[0].SavedAt != 0 || snapshot.Items[0].SavedAt != 99 {
		t.Fatal("snapshot is not independent or did not persist")
	}
	if items, _ := store.List(ctx); len(items) != 0 {
		t.Fatal("subscription automatically collected")
	}
	failure = &discovery.PlatformError{Message: "restricted fixture", Restricted: true}
	if _, err := service.Update(ctx, source.ID); !errors.Is(err, discovery.ErrUnavailable) {
		t.Fatal("failure hidden")
	}
	cached, _ := service.Get(ctx, source.ID)
	if cached.Source.LastUpdatedAt != detail.Source.LastUpdatedAt || cached.Items[0].Title != detail.Items[0].Title {
		t.Fatal("failure replaced cache")
	}
	failure = nil
	for _, bad := range []subscriptions.Snapshot{
		{Items: nil}, {Items: []content.Item{}, Total: 2}, {Items: []content.Item{video()}, Total: 0},
		{Items: []content.Item{video(), video()}, Total: 2}, {Items: []content.Item{video()}, Total: 1, HasMore: true},
		{Items: []content.Item{{ID: "bilibili:bad"}}, Total: 1},
	} {
		snapshot = bad
		if _, err := service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrSnapshot) {
			t.Fatal("incomplete snapshot accepted")
		}
		cached, _ = service.Get(ctx, source.ID)
		if len(cached.Items) != 1 || cached.Source.LastUpdatedAt != detail.Source.LastUpdatedAt {
			t.Fatal("incomplete snapshot damaged cache")
		}
	}
	renamed, err := service.Rename(ctx, source.ID, subscriptions.Edit{Name: "new name"})
	if err != nil || renamed.CreatedAt != source.CreatedAt || renamed.LastUpdatedAt != detail.Source.LastUpdatedAt {
		t.Fatal("rename reset history")
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) {
		return subscriptions.Snapshot{Items: []content.Item{}, Total: 0}, nil
	})
	cached, err = service.Get(ctx, source.ID)
	if err != nil || cached.Source.Name != "new name" || len(cached.Items) != 1 {
		t.Fatal("cache not preserved across reopen")
	}
	other, _, err := service.Create(ctx, subscriptions.Input{Query: "67890"})
	if err != nil {
		t.Fatal(err)
	}
	otherDetail, _ := service.Get(ctx, other.ID)
	if len(otherDetail.Items) != 0 || otherDetail.Source.LastUpdatedAt != 0 {
		t.Fatal("source leakage")
	}
	empty, err := service.Update(ctx, source.ID)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("valid empty response not stored")
	}
	if err := service.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, source.ID); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("deleted source remains")
	}
	if _, err := store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{video()}, Total: 1}, 999); !errors.Is(err, subscriptions.ErrNotFound) {
		t.Fatal("update resurrected deleted source")
	}
}

func TestUpdateCancellationAndConcurrentChanges(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	entered := make(chan struct{}, 2)
	service := subscriptions.NewService(store, func(ctx context.Context, _ string) (subscriptions.Snapshot, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return subscriptions.Snapshot{}, ctx.Err()
	})
	source, _, _ := service.Create(ctx, subscriptions.Input{Query: "12345"})
	other, _, _ := service.Create(ctx, subscriptions.Input{Query: "67890"})
	third, _, _ := service.Create(ctx, subscriptions.Input{Query: "99999"})
	active, cancel := context.WithCancel(ctx)
	done := make(chan error, 2)
	go func() { _, err := service.Update(active, source.ID); done <- err }()
	<-entered
	if _, err := service.Update(ctx, source.ID); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("duplicate update permitted")
	}
	if err := service.Delete(ctx, source.ID); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("deletion raced update")
	}
	if _, err := service.Rename(ctx, source.ID, subscriptions.Edit{Name: "new"}); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("rename raced update")
	}
	if _, _, err := service.Create(ctx, subscriptions.Input{Query: "12345"}); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("recreation raced update")
	}
	go func() { _, err := service.Update(active, other.ID); done <- err }()
	<-entered
	if _, err := service.Update(ctx, third.ID); !errors.Is(err, subscriptions.ErrBusy) {
		t.Fatal("global request limit exceeded")
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation hidden")
		}
	}
	detail, _ := service.Get(ctx, source.ID)
	if detail.Source.LastUpdatedAt != 0 {
		t.Fatal("cancellation committed")
	}
	if err := service.Delete(ctx, source.ID); err != nil {
		t.Fatal("update lock leaked")
	}
}

func TestSubscriptionValidation(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "input.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	ctx := context.Background()
	for _, query := range []string{"", "BV17x411w7KC", "0", "18446744073709551616", "https://evil.test/12345", strings.Repeat("1", 501)} {
		if _, _, err := service.Create(ctx, subscriptions.Input{Query: query}); !errors.Is(err, subscriptions.ErrInvalid) {
			t.Fatalf("accepted %q", query)
		}
	}
	if _, _, err := service.Create(ctx, subscriptions.Input{Query: "12345", Name: strings.Repeat("中", 101)}); !errors.Is(err, subscriptions.ErrInvalid) {
		t.Fatal("oversized name accepted")
	}
	if _, _, err := service.Create(ctx, subscriptions.Input{Query: "12345", Name: strings.Repeat("中", 100)}); err != nil {
		t.Fatal("unicode boundary rejected")
	}
	if _, err := service.Rename(ctx, "bilibili:creator:12345", subscriptions.Edit{Name: " "}); !errors.Is(err, subscriptions.ErrInvalid) {
		t.Fatal("blank rename accepted")
	}
	if _, err := service.Get(ctx, "bilibili:creator:BV17x411w7KC"); !errors.Is(err, subscriptions.ErrInvalid) {
		t.Fatal("invalid source ID accepted")
	}
}

func TestRSSCacheSurvivesRefreshFailureAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rss.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	ctx := context.Background()
	var failure error
	item := content.Item{ID: "rss:fixture", Platform: "rss", PlatformID: "fixture", Kind: "article", Title: "article", URL: "https://example.org/article", Description: "feed text"}
	service := subscriptions.NewService(store, func(_ context.Context, feed string) (subscriptions.Snapshot, error) {
		if feed != "https://example.org/feed.xml" {
			t.Errorf("wrong feed address: %s", feed)
		}
		return subscriptions.Snapshot{Items: []content.Item{item}, Title: "feed", Total: 1}, failure
	})
	source, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.org/feed.xml", Name: "my feed"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.Update(ctx, source.ID)
	if err != nil || len(saved.Items) != 1 {
		t.Fatalf("rss update: %v", err)
	}
	failure = discovery.ErrUnavailable
	if _, err = service.Update(ctx, source.ID); !errors.Is(err, discovery.ErrUnavailable) {
		t.Fatal("RSS error hidden")
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := store.GetSubscription(ctx, source.ID)
	if err != nil || cached.Source.URL != source.URL || cached.Source.LastUpdatedAt != saved.Source.LastUpdatedAt || len(cached.Items) != 1 || !reflect.DeepEqual(cached.Items[0], saved.Items[0]) {
		t.Fatal("RSS failed refresh damaged persisted cache")
	}
	if items, _ := store.List(ctx); len(items) != 0 {
		t.Fatal("RSS update implicitly collected content")
	}
}
