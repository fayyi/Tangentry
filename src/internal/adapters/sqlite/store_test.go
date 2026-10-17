package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tangentry/internal/content"
)

func TestSaveDeduplicatesAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "测试视频", URL: "https://www.bilibili.com/video/BV17x411w7KC/", SavedAt: 100}
	if _, err := store.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	item.Title, item.SavedAt = "更新后的标题", 200
	saved, err := store.Save(ctx, item)
	if err != nil || saved.SavedAt != 100 {
		t.Fatalf("save changed collection timestamp: %+v %v", saved, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.List(ctx)
	if err != nil || len(items) != 1 || items[0].Title != "更新后的标题" || items[0].SavedAt != 100 {
		t.Fatalf("bad reopened library: %+v %v", items, err)
	}
	if err := store.Delete(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, item.ID); !errors.Is(err, content.ErrNotFound) {
		t.Fatalf("missing item: %v", err)
	}
	items, err = store.List(ctx)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("bad empty library %+v %v", items, err)
	}
}
