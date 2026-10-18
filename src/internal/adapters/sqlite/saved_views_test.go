package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
)

func TestSavedViewCRUDReorderDefaultCountsAndInboxQuery(t *testing.T) {
	ctx := context.Background()
	s := inboxTestStore(t)
	source := inboxTestSource(t, s, "阅读", "https://example.com/saved-view")
	items := []content.Item{
		inboxTestItem(10, "video"),
		inboxTestItem(11, "article"),
		inboxTestItem(12, "video"),
	}
	items[0] = content.Item{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", Title: "历史 010", Author: "测试作者", URL: "https://youtube.com/watch?v=abcdefghijk", ThumbnailURL: "https://example.com/thumb.jpg"}
	items[1] = content.Item{ID: "rss:" + fmt.Sprintf("%064x", 11), Platform: "rss", PlatformID: fmt.Sprintf("%064x", 11), Kind: "article", Title: "历史 011", Author: "测试作者", URL: "https://example.com/item", ThumbnailURL: "https://example.com/thumb.jpg"}
	items[2] = content.Item{ID: "youtube:12345678901", Platform: "youtube", PlatformID: "12345678901", Kind: "video", Title: "不匹配内容", Author: "测试作者", URL: "https://youtube.com/watch?v=12345678901", ThumbnailURL: "https://example.com/thumb.jpg"}
	if _, err := s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: items, Total: len(items)}, 10); err != nil {
		t.Fatal(err)
	}
	service := subscriptions.NewSavedViewService(s)
	definition := subscriptions.SavedViewDefinition{Version: 1, Filters: subscriptions.SavedViewFilters{Platform: "youtube", Kind: "video", Read: "unread", Query: "历史 010"}}
	view, err := service.Create(ctx, subscriptions.SavedViewInput{Name: "视频阅读", Definition: definition, Icon: "play", Color: "blue"})
	if err != nil || view.ID == "" {
		t.Fatal(view, err)
	}
	editName, setDefault := "精选视频", true
	view, err = service.Update(ctx, view.ID, subscriptions.SavedViewEdit{Name: &editName, Default: &setDefault})
	if err != nil || view.Name != editName || !view.Default {
		t.Fatal(view, err)
	}
	page, count, err := service.Query(ctx, view.ID, "", 10)
	if err != nil || count != 1 || len(page.Entries) != 1 || page.Entries[0].Item.Platform != "youtube" {
		t.Fatal(page, count, err)
	}
	views, err := service.List(ctx)
	if err != nil || len(views) != 4 || views[0].ID != "builtin-unread" || views[1].ID != "builtin-week-videos" || views[2].ID != "builtin-favorite-podcasts" || views[3].Count != 1 {
		t.Fatal(views, err)
	}
	if err = service.Reorder(ctx, []string{view.ID, "builtin-unread", "builtin-week-videos", "builtin-favorite-podcasts"}); err != nil {
		t.Fatal(err)
	}
	views, err = service.List(ctx)
	if err != nil || views[0].ID != view.ID || !views[0].Default {
		t.Fatal(views, err)
	}
	if err = service.Delete(ctx, "builtin-week-videos"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(ctx, "builtin-week-videos"); !errors.Is(err, subscriptions.ErrSavedViewNotFound) {
		t.Fatal(err)
	}
}
