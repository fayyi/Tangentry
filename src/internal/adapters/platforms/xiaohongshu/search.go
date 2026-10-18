package xiaohongshu

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
		return discovery.SearchPage{Platform: "xiaohongshu"}, &discovery.PlatformError{Message: "小红书关键词搜索仅桌面版支持，请在应用内打开搜索结果。"}
	}
	result, err := capture.CaptureSearch(ctx, "xiaohongshu", query, strconv.Itoa(page))
	if err != nil {
		if ctx.Err() != nil {
			return discovery.SearchPage{}, ctx.Err()
		}
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "小红书搜索暂时不可用，请在应用内重试。"}
	}
	items := mapSearchCards(result.DOMCards)
	next := ""
	if len(items) > 0 {
		next = strconv.Itoa(page + 1)
	}
	return discovery.SearchPage{Items: items, Cursor: next, HasMore: next != "", Platform: "xiaohongshu"}, nil
}

func mapSearchCards(cards []platformfetch.DOMCard) []content.Item {
	items := make([]content.Item, 0, len(cards))
	seen := map[string]bool{}
	for _, card := range cards {
		id := strings.TrimPrefix(strings.TrimSpace(card.ID), "xiaohongshu:")
		ref, err := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + id)
		if err != nil || seen[id] {
			continue
		}
		title := strings.TrimSpace(card.Title)
		if title == "" {
			title = strings.TrimSpace(card.Author) + "的笔记"
		}
		item := content.Item{ID: "xiaohongshu:" + id, Platform: "xiaohongshu", PlatformID: id, Kind: "image", Title: title, Author: strings.TrimSpace(card.Author), AuthorID: strings.TrimSpace(card.AuthorID), URL: ref.URL, ThumbnailURL: safeImageURL(card.ThumbnailURL)}
		if content.Validate(item) != nil {
			continue
		}
		seen[id] = true
		items = append(items, item)
	}
	return items
}

func safeImageURL(raw string) string {
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}
