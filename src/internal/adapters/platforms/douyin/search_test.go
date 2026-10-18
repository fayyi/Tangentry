package douyin

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
		if platform != "douyin" || query != "街舞" || cursor != "2" {
			t.Fatalf("unexpected search request: %q %q %q", platform, query, cursor)
		}
		return platformfetch.SearchCaptureResponse{DOMCards: []platformfetch.DOMCard{
			{ID: "douyin:6961737553342991651", Title: "舞蹈测试", Author: "作者", AuthorID: "user-1", ThumbnailURL: "https://p3.douyinpic.com/cover.jpg", LikeCount: 12, Duration: 8},
			{ID: "6961737553342991651", Title: "重复"},
		}}, nil
	})}
	page, err := c.Search(context.Background(), "街舞", "2")
	if err != nil || calls != 1 || len(page.Items) != 1 || page.Items[0].AuthorID != "user-1" || page.Items[0].ThumbnailURL != "https://p3.douyinpic.com/cover.jpg" || page.Cursor != "3" || !page.HasMore {
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
	if _, err := c.Search(context.Background(), "query", "0"); err == nil || calls != 0 {
		t.Fatalf("invalid cursor was sent to bridge: calls=%d err=%v", calls, err)
	}
}
