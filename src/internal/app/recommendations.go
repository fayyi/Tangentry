package app

import (
	"context"

	"tangentry/internal/discovery"
)

// recommender routes recommendation pages to each platform's native feed.
type recommender struct {
	platforms     *platforms
	subscriptions func(context.Context) (discovery.Feed, error)
	queue         *discovery.RecommendationQueue
	// override replaces every platform feed (not the local subscription gallery) in tests.
	override func(context.Context, string) (discovery.Feed, error)
}

// Page serves the subscription gallery directly; platform pages pass through the
// persistent recommendation queue so fetched items survive reloads.
func (r *recommender) Page(ctx context.Context, source string, page int) (discovery.Feed, error) {
	if source == "subscriptions" {
		return r.subscriptions(ctx)
	}
	return r.queue.Fetch(ctx, source, func(ctx context.Context) (discovery.Feed, error) {
		return r.platformPage(ctx, source, page)
	})
}

func (r *recommender) platformPage(ctx context.Context, source string, page int) (discovery.Feed, error) {
	if r.override != nil {
		return r.override(ctx, source)
	}
	p := r.platforms
	switch source {
	case "bilibili":
		return p.discovery.bilibili.Recommendations(ctx, page)
	case "youtube":
		return p.network.toolkit.YouTubeRecommendationPage(ctx, page)
	case "douyin":
		return p.discovery.douyin.Recommendations(ctx, page)
	case "xiaoyuzhou":
		return p.discovery.podcasts.Recommendations(ctx, page, p.podcastToken())
	case "linuxdo":
		if filter := discovery.RecommendationFilter(ctx); filter != "" {
			return p.discovery.forum.Feed(ctx, filter, page)
		}
		return p.discovery.forum.Feed(ctx, "top", page)
	case "xiaohongshu":
		// The home feed has no continuation API yet; every page reads the first one.
		return p.discovery.notes.Recommendations(ctx)
	default:
		return discovery.Feed{}, discovery.ErrInvalidQuery
	}
}
