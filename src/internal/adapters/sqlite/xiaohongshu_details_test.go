package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func TestXiaohongshuDetailsSurviveReopenAndOpenedRecommendation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "details.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	cover := content.Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "图文", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=test-locator"}
	scope := discovery.QueueScope{Platform: "xiaohongshu", Account: discovery.PersonalAccount}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{cover}, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkQueuedRecommendation(ctx, scope, cover.ID, true, 101); err != nil {
		t.Fatal(err)
	}
	item, complete, err := s.XiaohongshuDetail(ctx, cover.ID)
	if err != nil || complete || item.URL != cover.URL {
		t.Fatal(item, complete, err)
	}
	detail := cover
	detail.Description = "完整正文"
	detail.Images = []content.Image{{URL: "https://sns-webpic-qc.xhscdn.com/first.jpg"}, {URL: "https://sns-webpic-qc.xhscdn.com/second.jpg"}}
	if err := s.SaveXiaohongshuDetail(ctx, detail); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{cover}, 102); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	item, complete, err = s.XiaohongshuDetail(ctx, cover.ID)
	if err != nil || !complete || item.Description != detail.Description || len(item.Images) != 2 {
		t.Fatal(item, complete, err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("detail acquisition should not collect content", items, err)
	}
	if err := s.SaveXiaohongshuDetail(ctx, content.Item{ID: "bad"}); !errors.Is(err, content.ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := s.XiaohongshuDetail(ctx, "youtube:abcdefghijk"); !errors.Is(err, content.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestRefreshingXiaohongshuLocatorKeepsCollectionAndQueueState(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "locator.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	original := content.Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "原收藏", Description: "收藏时的正文", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=old", SavedAt: 100}
	if _, err := s.Save(ctx, original); err != nil {
		t.Fatal(err)
	}
	scope := discovery.QueueScope{Platform: "xiaohongshu", Account: discovery.PersonalAccount}
	if err := s.EnqueueRecommendations(ctx, scope, []content.Item{original}, 101); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkQueuedRecommendation(ctx, scope, original.ID, true, 102); err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.URL = "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=fresh"
	updated.Description = "新获取的全文"
	if err := s.SaveXiaohongshuDetail(ctx, updated); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 || items[0].URL != updated.URL || items[0].SavedAt != 100 || items[0].Description != original.Description {
		t.Fatal("collection content or time changed", err)
	}
	var seenAt, openedAt int64
	var url string
	if err := s.db.QueryRowContext(ctx, `SELECT seen_at,opened_at,json_extract(payload,'$.url') FROM recommendation_queue WHERE platform='xiaohongshu' AND account=? AND content_id=?`, discovery.PersonalAccount, original.ID).Scan(&seenAt, &openedAt, &url); err != nil || seenAt != 0 || openedAt != 102 || url != updated.URL {
		t.Fatal("queue state changed or locator remained stale", err, seenAt, openedAt)
	}
}
