package xiaohongshu

import (
	"context"
	"errors"
	"testing"

	"tangentry/internal/platformfetch"
)

type searchCaptureFunc func(context.Context, string, string, string) (platformfetch.SearchCaptureResponse, error)

func (f searchCaptureFunc) CaptureSearch(ctx context.Context, platform, query, cursor string) (platformfetch.SearchCaptureResponse, error) {
	return f(ctx, platform, query, cursor)
}

func (searchCaptureFunc) Fetch(context.Context, string, string, platformfetch.Request) (platformfetch.Response, error) {
	return platformfetch.Response{}, errors.New("unexpected regular page fetch")
}

func TestSearchMapsDOMCardsAndAuthorID(t *testing.T) {
	calls := 0
	c := &Client{Page: searchCaptureFunc(func(_ context.Context, platform, query, cursor string) (platformfetch.SearchCaptureResponse, error) {
		calls++
		if platform != "xiaohongshu" || query != "旅行" || cursor != "3" {
			t.Fatalf("unexpected search request: %q %q %q", platform, query, cursor)
		}
		return platformfetch.SearchCaptureResponse{DOMCards: []platformfetch.DOMCard{
			{ID: "xiaohongshu:6411cf99000000001300b6d9", Title: "旅行测试", Author: "作者", AuthorID: "profile-1", ThumbnailURL: "//sns-webpic-qc.xhscdn.com/cover"},
			{ID: "6411cf99000000001300b6d9", Title: "重复"},
		}}, nil
	})}
	page, err := c.Search(context.Background(), "旅行", "3")
	if err != nil || calls != 1 || len(page.Items) != 1 || page.Items[0].AuthorID != "profile-1" || page.Items[0].ThumbnailURL != "https://sns-webpic-qc.xhscdn.com/cover" || page.Cursor != "4" || !page.HasMore {
		t.Fatalf("search mapping failed: %+v calls=%d err=%v", page, calls, err)
	}
}

func TestSearchReturnsCancellationWithoutMaskingIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{Page: searchCaptureFunc(func(ctx context.Context, _, _, _ string) (platformfetch.SearchCaptureResponse, error) {
		return platformfetch.SearchCaptureResponse{}, ctx.Err()
	})}
	_, err := c.Search(ctx, "query", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was masked: %v", err)
	}
}

func TestSearchRejectsInvalidCursorBeforeBridge(t *testing.T) {
	calls := 0
	c := &Client{Page: searchCaptureFunc(func(context.Context, string, string, string) (platformfetch.SearchCaptureResponse, error) {
		calls++
		return platformfetch.SearchCaptureResponse{}, nil
	})}
	if _, err := c.Search(context.Background(), "query", "1001"); err == nil || calls != 0 {
		t.Fatalf("invalid cursor was sent to bridge: calls=%d err=%v", calls, err)
	}
}
