package discovery

import (
	"context"
	"errors"
	"testing"

	"tangentry/internal/content"
)

type searchProviderFunc func(context.Context, string, string) (SearchPage, error)

func (f searchProviderFunc) Search(ctx context.Context, query, cursor string) (SearchPage, error) {
	return f(ctx, query, cursor)
}

func TestUnifiedSearchKeepsPlatformFailuresLocal(t *testing.T) {
	service := NewSearchService(func(context.Context, content.Query) (content.Page, error) {
		return content.Page{Items: []content.Item{{ID: "rss:1", Title: "本地测试"}}, Total: 1}, nil
	}, map[string]SearchProvider{
		"ok": searchProviderFunc(func(context.Context, string, string) (SearchPage, error) {
			return SearchPage{Platform: "ok", Items: []content.Item{{ID: "ok:1", Title: "平台测试"}}}, nil
		}),
		"bad": searchProviderFunc(func(context.Context, string, string) (SearchPage, error) {
			return SearchPage{}, errors.New("平台超时")
		}),
	})
	result, err := service.Search(context.Background(), "测试", SearchOptions{})
	if err != nil || len(result.Local.Items) != 1 || len(result.Sources["ok"].Page.Items) != 1 || result.Sources["bad"].Error != "平台超时" {
		t.Fatalf("unexpected result: %+v, %v", result, err)
	}
}

func TestUnifiedSearchHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewSearchService(nil, nil)
	if _, err := service.Search(ctx, "x", SearchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestUnifiedSearchFiltersPlatformPages(t *testing.T) {
	service := NewSearchService(nil, map[string]SearchProvider{
		"fixture": searchProviderFunc(func(context.Context, string, string) (SearchPage, error) {
			return SearchPage{Platform: "fixture", HasMore: true, Cursor: "next", Items: []content.Item{
				{ID: "fixture:recent", Kind: "video", PublishedAt: 200},
				{ID: "fixture:old", Kind: "article", PublishedAt: 10},
			}}, nil
		}),
	})
	result, err := service.Search(context.Background(), "x", SearchOptions{Kind: "video", From: 100})
	if err != nil {
		t.Fatal(err)
	}
	page := result.Sources["fixture"].Page
	if len(page.Items) != 1 || page.Items[0].ID != "fixture:recent" || page.Cursor != "next" || !page.HasMore {
		t.Fatalf("unexpected filtered page: %+v", page)
	}
}
