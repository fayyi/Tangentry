package app

import (
	"context"
	"strings"

	"tangentry/internal/adapters/platforms/xiaoyuzhou"
)

func podcastEpisodeID(ctx context.Context, inbox inboxReader, content contentReader, id string) string {
	if strings.HasPrefix(id, "xiaoyuzhou:") {
		episode, _, err := xiaoyuzhou.URL("https://www.xiaoyuzhoufm.com/episode/" + strings.TrimPrefix(id, "xiaoyuzhou:"))
		if err == nil {
			return episode
		}
		return ""
	}
	if !strings.HasPrefix(id, "rss:") {
		return ""
	}
	if inbox != nil {
		if item, err := inbox.Item(ctx, id); err == nil && item.Kind == "audio" && item.ID == id {
			if episode, _, parseErr := xiaoyuzhou.URL(item.URL); parseErr == nil {
				return episode
			}
		}
	}
	if item, err := content.Get(ctx, id); err == nil && item.Kind == "audio" && item.ID == id {
		if episode, _, parseErr := xiaoyuzhou.URL(item.URL); parseErr == nil {
			return episode
		}
	}
	return ""
}
