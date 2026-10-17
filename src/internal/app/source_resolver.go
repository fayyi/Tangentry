package app

import (
	"context"
	"errors"
	"fmt"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/subscriptions"
	"tangentry/internal/transcription"
)

// sourceResolver is an application service: it coordinates trusted content
// capabilities and platform clients to produce a transcription Audio value.
// The transcription domain itself remains unaware of platform URLs.
type sourceResolver struct {
	content     contentReader
	inbox       inboxReader
	downloads   completedDownloadReader
	youtube     func(context.Context, string) (transcription.Audio, error)
	douyin      func(context.Context, string) (transcription.Audio, error)
	xiaohongshu func(context.Context, string, string) (transcription.Audio, error)
	locator     func(string) string
	podcast     func(context.Context, string) (content.Item, error)
	bilibili    func(context.Context, string) (transcription.Audio, error)
}

type completedDownloadReader interface {
	CompletedPath(context.Context, string) (string, error)
}

var videoPages = map[string]string{
	"bilibili":    "https://www.bilibili.com/video/%s/",
	"youtube":     "https://www.youtube.com/watch?v=%s",
	"douyin":      "https://www.douyin.com/video/%s",
	"xiaohongshu": "https://www.xiaohongshu.com/explore/%s",
}

func (a *sourceResolver) Resolve(ctx context.Context, id string) (transcription.Audio, error) {
	platform, videoID, _ := splitSourceID(id)
	if page, ok := videoPages[platform]; ok {
		path, err := a.downloads.CompletedPath(ctx, fmt.Sprintf(page, videoID))
		if err == nil {
			return transcription.Audio{LocalPath: path, Platform: platform}, nil
		}
		if !errors.Is(err, downloads.ErrNotFound) {
			return transcription.Audio{}, transcription.Failed("local_media", "无法读取本机下载记录，请检查数据目录后重试。")
		}
	}
	switch platform {
	case "bilibili":
		return a.bilibili(ctx, videoID)
	case "youtube":
		return a.youtube(ctx, videoID)
	case "douyin":
		return a.douyin(ctx, videoID)
	case "xiaohongshu":
		return a.xiaohongshuAudio(ctx, id, videoID)
	case "xiaoyuzhou":
		item, err := a.podcast(ctx, "https://www.xiaoyuzhoufm.com/episode/"+videoID)
		if err != nil {
			return transcription.Audio{}, err
		}
		return mediaAudio(item)
	}
	item, err := a.savedItem(ctx, id)
	if err != nil {
		return transcription.Audio{}, err
	}
	return mediaAudio(item)
}

func (a *sourceResolver) xiaohongshuAudio(ctx context.Context, id, videoID string) (transcription.Audio, error) {
	raw := a.locator(videoID)
	item, found, err := a.libraryItem(ctx, id)
	if err != nil {
		return transcription.Audio{}, err
	}
	if found {
		if item.Kind != "video" {
			return transcription.Audio{}, transcription.ErrInvalid
		}
		raw = item.URL
	}
	return a.xiaohongshu(ctx, videoID, raw)
}

func (a *sourceResolver) savedItem(ctx context.Context, id string) (content.Item, error) {
	item, found, err := a.libraryItem(ctx, id)
	if err != nil || found {
		return item, err
	}
	archived, err := a.inbox.Item(ctx, id)
	if err != nil && !errors.Is(err, subscriptions.ErrInboxNotFound) {
		return content.Item{}, err
	}
	return archived, nil
}

func (a *sourceResolver) libraryItem(ctx context.Context, id string) (content.Item, bool, error) {
	item, err := a.content.Get(ctx, id)
	if errors.Is(err, content.ErrNotFound) {
		return content.Item{}, false, nil
	}
	return item, err == nil, err
}

func mediaAudio(item content.Item) (transcription.Audio, error) {
	if (item.Kind != "audio" && item.Kind != "video") || item.MediaURL == "" {
		return transcription.Audio{}, transcription.ErrInvalid
	}
	return transcription.Audio{URL: item.MediaURL, Duration: item.DurationSeconds}, nil
}

func splitSourceID(id string) (string, string, bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[:i], id[i+1:], true
		}
	}
	return id, "", false
}
