package tools

import (
	"context"
	"encoding/json"
	"time"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

// YouTube home recommendations are read in-process over InnerTube JSON.
func (t *Toolkit) YouTubeRecommendationStatus() downloads.ToolStatus {
	return downloads.ToolStatus{Name: "InnerTube", Available: t.youtubeHome != nil, Version: "内置"}
}

func (t *Toolkit) YouTubeRecommendations(ctx context.Context) (discovery.Feed, error) {
	feed, _, err := t.youTubeHomeBatch(ctx, nil)
	return feed, err
}

func (t *Toolkit) YouTubeSearch(ctx context.Context, query, cursor string) (discovery.SearchPage, error) {
	if t.youtubeHome == nil {
		return discovery.SearchPage{}, &discovery.PlatformError{Message: "YouTube 搜索暂不可用。"}
	}
	cookie := ""
	if t.credentials != nil {
		cookie = t.credentials.Header("youtube", "https://www.youtube.com/youtubei/v1/search")
	}
	home := youtube.NewHome()
	home.UserAgent = ""
	return home.SearchWithCookie(ctx, cookie, query, cursor)
}

func (t *Toolkit) youTubeHomeBatch(ctx context.Context, continuation json.RawMessage) (discovery.Feed, json.RawMessage, error) {
	if t.youtubeHome == nil {
		return discovery.Feed{}, nil, &discovery.PlatformError{Message: "YouTube 首页推荐暂不可用。"}
	}
	ctx, cancel := context.WithTimeout(ctx, 26*time.Second)
	defer cancel()
	cookie := ""
	if t.credentials != nil {
		cookie = t.credentials.Header("youtube", "https://www.youtube.com/youtubei/v1/browse")
	}
	result := t.youtubeHome(ctx, cookie, continuation)
	if ctx.Err() != nil && result.Error != "" {
		return discovery.Feed{}, nil, &discovery.PlatformError{Message: "YouTube 推荐请求超时，请检查运行 Tangentry 的机器能否访问 YouTube。"}
	}
	if len(result.Entries) > 200 {
		return discovery.Feed{}, nil, &discovery.PlatformError{Message: "YouTube 推荐返回的数据暂不兼容，请重试或更新适配器。"}
	}
	if result.Error != "" || (len(result.Entries) == 0 && len(continuation) == 0) {
		message := "YouTube 网络请求失败，请检查运行 Tangentry 的机器能否访问 YouTube；保存 Cookie 不代表网络已连通。"
		restricted := false
		switch result.Error {
		case "batch_size":
			message = "YouTube 推荐批次超出适配器支持范围，请刷新重试或更新适配器；已有推荐仍保留。"
		case "continuation_expired":
			message = "YouTube 推荐会话已过期，请刷新推荐后继续浏览。"
		case "auth":
			message = "保存的 Cookie 未被 YouTube 识别为登录状态。请在已登录的 YouTube 页面重新导出完整 Cookie，并在工具设置重新导入。"
		case "restricted":
			message = "YouTube 暂时限制请求，请稍后重试。"
			restricted = true
		case "empty":
			message = "YouTube 首页没有返回推荐。请检查账号观看历史是否启用、Cookie 是否完整，或使用搜索与频道订阅。"
		case "home_nudge":
			message = "YouTube 返回了开始观看提示，没有提供首页视频。可先搜索或添加频道订阅；需要账号推荐时，请确认该账号在 YouTube 原站有观看历史，并重新导入 Cookie。"
		}
		return discovery.Feed{}, nil, &discovery.PlatformError{Message: message, Restricted: restricted}
	}
	feed := discovery.Feed{Items: []content.Item{}, Page: 1, SourceTitle: "YouTube 首页推荐"}
	if result.Mode == "subscriptions" {
		feed.SourceTitle = "YouTube 账号订阅动态"
	}
	seen := map[string]bool{}
	for _, entry := range result.Entries {
		id, url, err := discovery.ParseYouTube("https://www.youtube.com/watch?v=" + entry.ID)
		if err != nil {
			return discovery.Feed{}, nil, discovery.ErrUnavailable
		}
		item := content.Item{ID: "youtube:" + id, Platform: "youtube", PlatformID: id, Kind: "video", Title: entry.Title, Author: entry.Author, URL: url, ThumbnailURL: entry.Thumbnail, DurationSeconds: entry.Duration}
		item.AuthorAvatarURL = entry.Avatar
		item.PublishedText = entry.PublishedText
		if discovery.YouTubeChannelID(entry.AuthorID) {
			item.AuthorID = entry.AuthorID
		}
		if content.Validate(item) != nil {
			return discovery.Feed{}, nil, discovery.ErrUnavailable
		}
		if !seen[id] {
			feed.Items = append(feed.Items, item)
			seen[id] = true
		}
	}
	feed.Total = len(feed.Items)
	if len(result.Continuation) > 65536 {
		return discovery.Feed{}, nil, discovery.ErrUnavailable
	}
	if string(result.Continuation) == "null" {
		result.Continuation = nil
	}
	feed.HasMore = len(result.Continuation) > 0
	return feed, result.Continuation, nil
}
