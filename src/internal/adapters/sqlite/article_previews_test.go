package sqlite

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"tangentry/internal/articles"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
)

func TestCachedArticlePreviewsAcrossListsPreserveFeedAndAnchors(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	item := inboxTestItem(81, "article")
	item.Description = "Comments"
	source := inboxTestSource(t, s, "正文缓存", "https://example.com/cache-feed")
	if _, err := s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	read := func() []content.Item {
		t.Helper()
		library, err := s.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := s.GetSubscription(ctx, source.ID)
		if err != nil {
			t.Fatal(err)
		}
		inbox, err := s.GetSubscriptionInboxItem(ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		page, _, err := s.ListSubscriptionInbox(ctx, subscriptions.InboxQuery{}, subscriptions.InboxPosition{})
		if err != nil || len(page.Entries) != 1 {
			t.Fatal(page, err)
		}
		return []content.Item{library[0], detail.Items[0], inbox, page.Entries[0].Item}
	}
	for _, got := range read() {
		if got.PreviewText != "" || got.Description != item.Description {
			t.Fatal("uncached preview or feed changed", got)
		}
	}
	paragraph := strings.Repeat("原站正文😀\n ", 90)
	doc := articles.Document{ContentID: item.ID, SourceURL: item.URL, Title: item.Title, Revision: 1, Blocks: []articles.Block{
		{Kind: "heading", Level: 2, Text: "正文标题"},
		{Kind: "code", Text: strings.Repeat("code ", 20)},
		{Kind: "paragraph", Text: "短的导航标签"},
		{Kind: "paragraph", Text: paragraph},
	}}
	cached, err := s.SaveArticle(ctx, doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := string([]rune(strings.Join(strings.Fields(paragraph), " "))[:280])
	for _, got := range read() {
		if got.PreviewText != want || got.Description != item.Description || !utf8.ValidString(got.PreviewText) {
			t.Fatal("cached preview or original description mismatch", got)
		}
	}
	original, err := s.GetArticle(ctx, item.ID)
	if err != nil || !reflect.DeepEqual(original, cached) {
		t.Fatal("preview read changed canonical text/hash/revision", original, err)
	}
	for _, table := range []string{"saved_content", "subscription_inbox"} {
		var payload string
		key := "id"
		if table == "subscription_inbox" {
			key = "content_id"
		}
		if err := s.db.QueryRow(`SELECT payload FROM `+table+` WHERE `+key+`=?`, item.ID).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var raw content.Item
		if err := json.Unmarshal([]byte(payload), &raw); err != nil || raw.Description != "Comments" || raw.PreviewText != "" {
			t.Fatal("preview persisted into feed/library", payload, err)
		}
	}
	cached.Revision++
	cached.Blocks = []articles.Block{{Kind: "paragraph", Text: strings.Repeat("更新后的真实正文。", 8)}}
	if _, err := s.SaveArticle(ctx, cached, 1); err != nil {
		t.Fatal(err)
	}
	for _, got := range read() {
		if got.PreviewText != cached.Blocks[0].Text {
			t.Fatal("preview kept stale full text", got)
		}
	}
}

func TestArticlePreviewBatchBoundsAndDescriptionPriority(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	items := make([]content.Item, 300)
	for i := range items {
		item := inboxTestItem(1000+i, "article")
		item.Description = ""
		doc := articles.Document{ContentID: item.ID, SourceURL: item.URL, Title: item.Title, Revision: 1, Blocks: []articles.Block{{Kind: "paragraph", Text: strings.Repeat("正文预览。", 12)}}}
		if _, err := s.SaveArticle(ctx, doc, 0); err != nil {
			t.Fatal(err)
		}
		items[i] = item
	}
	items[0].Description = "订阅提供的真实摘要"
	items[1].Kind = "audio"
	items[2].Platform = "other"
	items[3].PreviewText = "未经验证的预览"
	s.applyArticlePreviews(ctx, items)
	for i, item := range items {
		if i < 3 {
			if item.PreviewText != "" {
				t.Fatal("existing text or other media got an article preview", item)
			}
		} else if item.PreviewText != strings.Repeat("正文预览。", 12) {
			t.Fatal("batch/preview mismatch", i, item)
		}
	}
}

func TestPreviewCacheFailureDoesNotHideLibraryAndSaveDropsProjectedText(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	item := inboxTestItem(91, "article")
	item.Description = ""
	item.PreviewText = "客户端不能伪造本机正文预览"
	saved, err := content.NewService(s).Save(ctx, item)
	if err != nil || saved.PreviewText != "" {
		t.Fatal(saved, err)
	}
	if _, err := s.db.Exec(`DROP TABLE article_documents`); err != nil {
		t.Fatal(err)
	}
	library, err := s.List(ctx)
	if err != nil || len(library) != 1 || library[0].PreviewText != "" || library[0].ID != item.ID {
		t.Fatal("optional cache failure hid saved content", library, err)
	}
}
