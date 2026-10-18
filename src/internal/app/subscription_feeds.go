package app

import (
	"context"
	"strings"

	"tangentry/internal/adapters/feeds"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/subscriptions"
	"time"
)

// subscriptionFeeds adapts platform clients to the fetchers the subscription
// service consumes, so the subscription domain never sees adapter types.
type subscriptionFeeds struct {
	platformSubscriptions
	records subscriptionRecords
}

func (f subscriptionFeeds) options() []subscriptions.Option {
	return []subscriptions.Option{
		subscriptions.WithConditionalFeeds(f.conditional),
		subscriptions.WithRSSFeeds(f.rss),
		subscriptions.WithPaperSources(f.paper),
		subscriptions.WithYouTubeResolver(f.toolkit.YouTubeChannel),
		subscriptions.WithSourceResolver(f.resolveTitle),
	}
}

func (f subscriptionFeeds) paper(ctx context.Context, query string) (subscriptions.Snapshot, error) {
	if f.papers == nil {
		return subscriptions.Snapshot{}, discovery.ErrUnavailable
	}
	if strings.HasPrefix(query, "today") || strings.HasPrefix(query, "20") && len(query) == 10 {
		date := time.Now()
		if query != "today" {
			if parsed, err := time.Parse("2006-01-02", query); err == nil {
				date = parsed
			}
		}
		feed, err := f.papers.Daily(ctx, date)
		return subscriptions.Snapshot{Items: feed.Items, Title: feed.SourceTitle, Total: feed.Total, HasMore: feed.HasMore}, err
	}
	feed, err := f.papers.ArxivCategory(ctx, query)
	return subscriptions.Snapshot{Items: feed.Items, Title: feed.SourceTitle, Total: feed.Total, HasMore: feed.HasMore}, err
}

// snapshot reads the latest items of a YouTube channel, RSS / Atom address or Bilibili creator.
func (f subscriptionFeeds) snapshot(ctx context.Context, uid string) (subscriptions.Snapshot, error) {
	if discovery.YouTubeChannelID(uid) {
		result, err := f.youtube.FetchConditional(ctx, uid, feeds.Validators{})
		if err != nil {
			return subscriptions.Snapshot{}, err
		}
		f.youtube.EnrichDurations(ctx, result.Feed.Items)
		return snapshotOf(result.Feed), nil
	}
	if strings.HasPrefix(uid, "http://") || strings.HasPrefix(uid, "https://") {
		feed, err := f.feeds.Fetch(ctx, uid)
		if err != nil {
			return subscriptions.Snapshot{}, err
		}
		f.feeds.EnrichSummaries(ctx, feed.Items)
		return snapshotOf(feed), nil
	}
	feed, err := f.creators.CreatorVideos(ctx, uid, 1)
	if err != nil {
		return subscriptions.Snapshot{}, err
	}
	if feed.Page != 1 || feed.Query != uid {
		return subscriptions.Snapshot{}, subscriptions.ErrSnapshot
	}
	snapshot := snapshotOf(feed)
	snapshot.HasMore = feed.HasMore
	return snapshot, nil
}

// conditional checks a YouTube channel feed with cached HTTP validators.
func (f subscriptionFeeds) conditional(ctx context.Context, raw string, v subscriptions.Validators) (subscriptions.ConditionalResult, error) {
	if !discovery.YouTubeChannelID(raw) {
		return subscriptions.ConditionalResult{}, discovery.ErrInvalidQuery
	}
	result, err := f.youtube.FetchConditional(ctx, raw, feedValidators(v))
	if err == nil {
		f.enrichFresh(ctx, &result, "youtube:channel:"+raw, f.youtube.EnrichDurations, func(item content.Item) any { return item.DurationSeconds })
	}
	return conditionalResult(result), err
}

// rss checks an RSS / Atom source with cached HTTP validators and its stable identity.
func (f subscriptionFeeds) rss(ctx context.Context, source subscriptions.Source, v subscriptions.Validators) (subscriptions.ConditionalResult, error) {
	result, err := f.feeds.FetchConditionalIdentity(ctx, source.CreatorID, source.IdentityURL, feedValidators(v))
	if err == nil {
		f.enrichFresh(ctx, &result, source.ID, f.feeds.EnrichSummaries, func(item content.Item) any { return item.Description })
	}
	return conditionalResult(result), err
}

// enrichFresh completes per-item details (durations, summaries) that a feed
// does not carry. An unchanged feed still re-enriches the stored items, because
// earlier enrichment may have failed; any newly filled detail is returned as a
// fresh snapshot instead of "not modified".
func (f subscriptionFeeds) enrichFresh(ctx context.Context, result *feeds.Result, subscriptionID string, enrich func(context.Context, []content.Item), detail func(content.Item) any) {
	if !result.NotModified {
		enrich(ctx, result.Feed.Items)
		return
	}
	previous, err := f.records.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return
	}
	before := make([]any, len(previous.Items))
	for i, item := range previous.Items {
		before[i] = detail(item)
	}
	enrich(ctx, previous.Items)
	for i, item := range previous.Items {
		if detail(item) != before[i] {
			result.NotModified = false
			result.Feed = discovery.Feed{Items: previous.Items, SourceTitle: previous.Source.SourceTitle, Total: previous.Source.Total}
			return
		}
	}
}

// resolveTitle validates a new source address by reading its display title.
func (f subscriptionFeeds) resolveTitle(ctx context.Context, source, query string) (string, error) {
	switch source {
	case "youtube":
		result, err := f.youtube.FetchConditional(ctx, query, feeds.Validators{})
		return result.Feed.SourceTitle, err
	case "bilibili":
		result, err := f.creators.CreatorVideos(ctx, query, 1)
		return result.SourceTitle, err
	case "rss":
		result, err := f.feeds.Fetch(ctx, query)
		return result.SourceTitle, err
	default:
		return "", subscriptions.ErrInvalid
	}
}

func snapshotOf(feed discovery.Feed) subscriptions.Snapshot {
	return subscriptions.Snapshot{Items: feed.Items, Title: feed.SourceTitle, Total: feed.Total}
}

func feedValidators(v subscriptions.Validators) feeds.Validators {
	return feeds.Validators{ETag: v.ETag, LastModified: v.LastModified, ResourceURL: v.ResourceURL}
}

func conditionalResult(result feeds.Result) subscriptions.ConditionalResult {
	v := result.Validators
	return subscriptions.ConditionalResult{
		Snapshot:    snapshotOf(result.Feed),
		NotModified: result.NotModified,
		Validators:  subscriptions.Validators{ETag: v.ETag, LastModified: v.LastModified, ResourceURL: v.ResourceURL},
	}
}
