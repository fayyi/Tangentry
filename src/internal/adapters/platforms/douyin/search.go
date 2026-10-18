package douyin

import (
	"context"
	"strconv"
	"strings"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/platformfetch"
)

func (c *Client) Search(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	page := 1
	if cursor != "" {
		value, err := strconv.Atoi(cursor)
		if err != nil || value < 1 || value > 1000 {
			return discovery.SearchPage{}, discovery.ErrInvalidQuery
		}
		page = value
	}
	capture, ok := c.Page.(platformfetch.SearchCaptureFetcher)
	if !ok {
		return discovery.SearchPage{Platform: "douyin"}, &discovery.PlatformError{Message: "抖音关键词搜索仅桌面版支持，请在应用内打开搜索结果。"}
	}
	result, err := capture.CaptureSearch(ctx, "douyin", query, strconv.Itoa(page))
	if err != nil {
		if ctx.Err() != nil {
			return discovery.SearchPage{}, ctx.Err()
		}
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "抖音搜索暂时不可用，请在应用内重试。"}
	}
	items := make([]content.Item, 0, len(result.DOMCards))
	seen := map[string]bool{}
	for _, card := range result.DOMCards {
		id := strings.TrimPrefix(strings.TrimSpace(card.ID), "douyin:")
		ref, parseErr := discovery.ParseDouyin("https://www.douyin.com/video/" + id)
		if parseErr != nil || seen[id] {
			continue
		}
		title := strings.TrimSpace(card.Title)
		if title == "" {
			title = strings.TrimSpace(card.Author) + "的视频"
		}
		item := content.Item{ID: "douyin:" + id, Platform: "douyin", PlatformID: id, Kind: "video", Title: title, Author: strings.TrimSpace(card.Author), AuthorID: strings.TrimSpace(card.AuthorID), URL: ref.URL, ThumbnailURL: safeHTTPSURL(card.ThumbnailURL), DurationSeconds: maxInt(card.Duration, 0), LikeCount: maxInt(card.LikeCount, 0)}
		if content.Validate(item) != nil {
			continue
		}
		seen[id] = true
		items = append(items, item)
	}
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return discovery.SearchPage{Items: items, Cursor: next, HasMore: next != "", Platform: "douyin"}, nil
}
