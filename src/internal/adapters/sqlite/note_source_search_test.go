package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestNoteSearchAndExportMatchLocalSourceWithoutSearchingURLs(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	source := inboxTestSource(t, s, "来源", "https://example.com/feed")
	item := inboxTestItem(45, "article")
	item.Title = "Alpha 来源 100%_quoted'"
	item.Author = "作者测试"
	item.URL = "https://example.com/private-url-only-token"
	if _, err := s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}}, 10); err != nil {
		t.Fatal(err)
	}
	s.DeleteSubscription(ctx, source.ID)
	service := notes.NewService(s)
	note, err := service.Create(ctx, notes.Input{ContentID: item.ID, Body: "这里的正文不含来源名称", Tags: []string{"学习"}})
	if err != nil {
		t.Fatal(err)
	}
	service.SetPinned(ctx, note.ID, true)
	for _, text := range []string{"alpha", "100%_quoted'", "作者测试"} {
		page, err := service.SearchFiltered(ctx, text, "", notes.Filters{PinnedOnly: true, Tag: "学习"})
		if err != nil || len(page.Notes) != 1 || page.Notes[0].ID != note.ID {
			t.Fatal(text, page, err)
		}
		exported, err := s.ExportNotes(ctx, notes.SearchQuery{Text: text, PinnedOnly: true, Tag: "学习"}, "")
		if err != nil || len(exported) != 1 || exported[0].Note.ID != note.ID {
			t.Fatal(text, exported, err)
		}
	}
	for _, text := range []string{"private-url-only-token", "100XYZquoted"} {
		page, err := service.Search(ctx, text, "")
		if err != nil || len(page.Notes) != 0 {
			t.Fatal("unexpected match", text, page, err)
		}
		exported, err := s.ExportNotes(ctx, notes.SearchQuery{Text: text}, "")
		if err != nil || len(exported) != 0 {
			t.Fatal("unexpected export", text, exported, err)
		}
	}
	for _, payload := range []string{"invalid JSON", `{"id":"other","title":"foreign title"}`} {
		if _, err := s.db.Exec(`UPDATE subscription_inbox SET payload=? WHERE content_id=?`, payload, item.ID); err != nil {
			t.Fatal(err)
		}
		page, err := service.Search(ctx, "foreign", "")
		if err != nil || len(page.Notes) != 0 {
			t.Fatal("invalid metadata matched", page, err)
		}
		page, err = service.Search(ctx, "正文", "")
		if err != nil || len(page.Notes) != 1 || page.Notes[0].Source != nil {
			t.Fatal("body search broken", page, err)
		}
	}
	item.Title = "原历史标题"
	data, _ := json.Marshal(item)
	s.db.Exec(`UPDATE subscription_inbox SET payload=? WHERE content_id=?`, string(data), item.ID)
	item.Title = "收藏标题"
	if _, err := s.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	page, err := service.Search(ctx, "收藏标题", "")
	if err != nil || len(page.Notes) != 1 {
		t.Fatal(page, err)
	}
	page, err = service.Search(ctx, "原历史标题", "")
	if err != nil || len(page.Notes) != 0 {
		t.Fatal("source precedence inconsistent", page, err)
	}
}

func TestNoteSourcesUseOpenedPersonalRecommendationsAndCompleteXiaohongshuDetails(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	service := notes.NewService(s)
	items := []content.Item{
		{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", Title: "推荐课程 100%_quoted'", Author: "课程作者", URL: "https://www.youtube.com/watch?v=abcdefghijk&private=hidden-url-value"},
		{ID: "xiaoyuzhou:63527ceea526d88c7038aae0", Platform: "xiaoyuzhou", PlatformID: "63527ceea526d88c7038aae0", Kind: "audio", Title: "播客推荐标题", Author: "播客作者", URL: "https://www.xiaoyuzhoufm.com/episode/63527ceea526d88c7038aae0"},
		{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "推荐图文标题", Author: "图文作者", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=hidden-access-locator"},
	}
	for index, item := range items {
		scope := discovery.QueueScope{Platform: item.Platform, Account: discovery.PersonalAccount}
		if err := s.EnqueueRecommendations(ctx, scope, []content.Item{item}, 100); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkQueuedRecommendation(ctx, scope, item.ID, true, 101); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateNote(ctx, notes.Note{ID: fmt.Sprintf("%032x", index+1), ContentID: item.ID, Body: "自己的想法", Tags: []string{"学习/来源"}, Pinned: true, CreatedAt: 1000, UpdatedAt: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	detail := items[2]
	detail.Title = "已保存完整图文"
	detail.Description = "正文不是来源搜索关键词"
	if err := s.SaveXiaohongshuDetail(ctx, detail); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		query string
		item  content.Item
	}{{"100%_quoted'", items[0]}, {"课程作者", items[0]}, {"播客推荐标题", items[1]}, {"已保存完整图文", detail}} {
		page, err := service.SearchFiltered(ctx, input.query, "", notes.Filters{Tag: "学习", PinnedOnly: true, From: 999, To: 1001})
		if err != nil || len(page.Notes) != 1 || page.Notes[0].Source == nil || page.Notes[0].Source.Title != input.item.Title || page.Notes[0].Source.URL != "" {
			t.Fatal("local source missing or locator exposed", input.query, page, err)
		}
		entries, err := s.ExportNotes(ctx, notes.SearchQuery{Text: input.query, Tag: "学习", PinnedOnly: true, FromCreatedAt: 999, ToCreatedAt: 1001}, "")
		if err != nil || len(entries) != 1 || entries[0].Source == nil || entries[0].Source.Title != input.item.Title || entries[0].Source.URL != input.item.URL {
			t.Fatal("export and search source differ", input.query, entries, err)
		}
	}
	for _, query := range []string{"推荐图文标题", "hidden-url-value", "hidden-access-locator", detail.Description} {
		page, err := service.Search(ctx, query, "")
		if err != nil || len(page.Notes) != 0 {
			t.Fatal("hidden fields or lower-priority metadata matched", query, page, err)
		}
	}
}

func TestNoteRecommendationMetadataIsScopedIdentifiedAndLowerPriorityThanCollection(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	service := notes.NewService(s)
	item := content.Item{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", Title: "个人推荐标题", Author: "推荐作者", URL: "https://www.youtube.com/watch?v=abcdefghijk"}
	if _, err := service.Create(ctx, notes.Input{ContentID: item.ID, Body: "自己的正文"}); err != nil {
		t.Fatal(err)
	}
	other := discovery.QueueScope{Platform: item.Platform, Account: strings.Repeat("a", 64)}
	if err := s.EnqueueRecommendations(ctx, other, []content.Item{item}, 100); err != nil {
		t.Fatal(err)
	}
	assertSource := func(query string, count int, title string) {
		t.Helper()
		page, err := service.Search(ctx, query, "")
		if err != nil || len(page.Notes) != count {
			t.Fatal(query, page, err)
		}
		if count == 1 && (title == "" && page.Notes[0].Source != nil || title != "" && (page.Notes[0].Source == nil || page.Notes[0].Source.Title != title)) {
			t.Fatal("wrong metadata", query, page)
		}
		entries, err := s.ExportNotes(ctx, notes.SearchQuery{Text: query}, "")
		if err != nil || len(entries) != count || count == 1 && (title == "" && entries[0].Source != nil || title != "" && (entries[0].Source == nil || entries[0].Source.Title != title)) {
			t.Fatal("export metadata disagrees", query, entries, err)
		}
	}
	assertSource("个人推荐标题", 0, "")
	assertSource("正文", 1, "")
	scope := discovery.QueueScope{Platform: item.Platform, Account: discovery.PersonalAccount}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{item}, 101); err != nil {
		t.Fatal(err)
	}
	assertSource("个人推荐标题", 1, item.Title)
	for _, payload := range []string{"invalid JSON", `{"id":"youtube:foreign0001","title":"错误标题","platform":"youtube"}`, `{"id":"youtube:abcdefghijk","title":"错误标题","platform":"bilibili"}`} {
		if _, err := s.db.Exec(`UPDATE recommendation_queue SET payload=? WHERE account=?`, payload, discovery.PersonalAccount); err != nil {
			t.Fatal(err)
		}
		assertSource("错误标题", 0, "")
		assertSource("正文", 1, "")
	}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{item}, 102); err != nil {
		t.Fatal(err)
	}
	item.Title = "收藏优先标题"
	if _, err := s.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	assertSource("个人推荐标题", 0, "")
	assertSource("收藏优先标题", 1, item.Title)
	// A damaged higher-priority local copy cannot hide the valid personal one.
	if _, err := s.db.Exec(`UPDATE saved_content SET payload='invalid JSON' WHERE id=?`, item.ID); err != nil {
		t.Fatal(err)
	}
	assertSource("个人推荐标题", 1, "个人推荐标题")
}

func TestRetainedNoteSourceMetadataSurvivesReopenAndPaginatedSearch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retained-notes.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := content.Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "重开推荐来源", Author: "重开作者", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9"}
	scope := discovery.QueueScope{Platform: item.Platform, Account: discovery.PersonalAccount}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{item}, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkQueuedRecommendation(ctx, scope, item.ID, true, 101); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 62; index++ {
		if _, err := s.CreateNote(ctx, notes.Note{ID: fmt.Sprintf("%032x", index+1), ContentID: item.ID, Body: "保存的理解", Tags: []string{"学习/图文"}, Pinned: true, CreatedAt: 1000, UpdatedAt: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service := notes.NewService(s)
	filters := notes.Filters{Tag: "学习", PinnedOnly: true, From: 999, To: 1001}
	first, err := service.SearchFiltered(ctx, item.Author, "", filters)
	if err != nil || len(first.Notes) != 50 || first.NextCursor == "" {
		t.Fatal("first page", first, err)
	}
	second, err := service.SearchFiltered(ctx, item.Author, first.NextCursor, filters)
	if err != nil || len(second.Notes) != 12 || second.NextCursor != "" {
		t.Fatal("second page", second, err)
	}
	seen := map[string]bool{}
	for _, note := range append(first.Notes, second.Notes...) {
		if seen[note.ID] || note.Source == nil || note.Source.Title != item.Title {
			t.Fatal("duplicate or missing source", note)
		}
		seen[note.ID] = true
	}
	entries, err := s.ExportNotes(ctx, notes.SearchQuery{Text: item.Author, Tag: "学习", PinnedOnly: true, FromCreatedAt: 999, ToCreatedAt: 1001}, "")
	if err != nil || len(entries) != len(seen) {
		t.Fatal("export disagrees", len(entries), err)
	}
	for _, entry := range entries {
		if !seen[entry.Note.ID] || entry.Source == nil || entry.Source.Title != item.Title {
			t.Fatal("export source missing", entry)
		}
	}
	// A malformed complete copy cannot hide an identified recommendation.
	for _, payload := range []string{"invalid JSON", `{"id":"xiaohongshu:6411cf99000000001300b6da","platform":"xiaohongshu","title":"错误详情"}`, `{"id":"xiaohongshu:6411cf99000000001300b6d9","platform":"youtube","title":"错误详情"}`} {
		if _, err := s.db.Exec(`INSERT INTO xiaohongshu_details(content_id,payload) VALUES(?,?) ON CONFLICT(content_id) DO UPDATE SET payload=excluded.payload`, item.ID, payload); err != nil {
			t.Fatal(err)
		}
		page, err := service.SearchFiltered(ctx, item.Author, "", filters)
		if err != nil || len(page.Notes) != 50 || page.Notes[0].Source == nil || page.Notes[0].Source.Title != item.Title {
			t.Fatal("damaged detail hid recommendations", page, err)
		}
	}
}
