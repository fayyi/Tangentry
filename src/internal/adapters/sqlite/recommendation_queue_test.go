package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func TestRecommendationQueuePersistsUnopenedItemsAndIsolatesAccounts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queue.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := discovery.QueueScope{Platform: "bilibili", Account: strings.Repeat("a", 64)}
	other := discovery.QueueScope{Platform: "bilibili", Account: strings.Repeat("b", 64)}
	first := content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "第一条", URL: "https://www.bilibili.com/video/BV17x411w7KC/"}
	second := first
	second.ID = "bilibili:BV18x411w7KC"
	second.PlatformID = "BV18x411w7KC"
	second.Title = "第二条"
	third := first
	third.ID = "bilibili:BV19x411w7KC"
	third.PlatformID = "BV19x411w7KC"
	third.Title = "第三条"
	if err := store.EnqueueRecommendations(ctx, scope, []content.Item{first, second, third}, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueRecommendations(ctx, other, []content.Item{first}, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueuedRecommendation(ctx, scope, first.ID, false, 110); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueuedRecommendation(ctx, scope, third.ID, true, 120); err != nil {
		t.Fatal(err)
	}
	first.Title = "更新标题"
	if err := store.EnqueueRecommendations(ctx, scope, []content.Item{first, third}, 200); err != nil {
		t.Fatal(err)
	}
	first.Title = "迟到的旧标题"
	if err := store.EnqueueRecommendations(ctx, scope, []content.Item{first}, 150); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.ListQueuedRecommendations(ctx, scope, 500)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if items[0].Item.ID != second.ID || items[1].Item.Title != "更新标题" || items[1].SeenAt != 110 || items[1].FetchedAt != 100 {
		t.Fatal(items)
	}
	isolated, err := store.ListQueuedRecommendations(ctx, other, 500)
	if err != nil || len(isolated) != 1 || isolated[0].SeenAt != 0 || isolated[0].OpenedAt != 0 {
		t.Fatal(isolated, err)
	}
	if err := store.EnqueueRecommendations(ctx, scope, []content.Item{second, {ID: "bad"}}, 300); !errors.Is(err, discovery.ErrInvalidQuery) {
		t.Fatal(err)
	}
	if _, err := store.ListQueuedRecommendations(ctx, scope, 501); !errors.Is(err, discovery.ErrInvalidQuery) {
		t.Fatal(err)
	}
	var saved int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM saved_content`).Scan(&saved); err != nil || saved != 0 {
		t.Fatal("queue must not collect recommendations", saved, err)
	}
}
