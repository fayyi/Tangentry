package app

import (
	"context"

	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"tangentry/internal/transcription"
)

// subscriptionManager is the small command surface retained for app-level
// integration tests. Production shells receive only ShellStatePort and never
// see the subscription service.
type subscriptionManager interface {
	Create(context.Context, subscriptions.Input) (subscriptions.Source, bool, error)
	Delete(context.Context, string) error
}

// xiaohongshuRecords is the small persistence port needed by the platform
// adapter. Keeping it here prevents platform wiring from depending on the
// complete SQLite store.
type xiaohongshuRecords interface {
	SaveXiaohongshuDetail(context.Context, content.Item) error
	XiaohongshuDetail(context.Context, string) (content.Item, bool, error)
	XiaohongshuImageCandidates(context.Context, string) ([]content.Item, error)
}

// subscriptionRecords is used only to recover enrichment data after a feed
// returns HTTP 304. The subscription service owns the rest of its repository.
type subscriptionRecords interface {
	GetSubscription(context.Context, string) (subscriptions.Detail, error)
}

type repositories struct {
	xiaohongshu  xiaohongshuRecords
	subscription subscriptionRecords
}

// contentReader and inboxReader keep application orchestration on the query
// surface it actually needs instead of passing a persistence repository.
type contentReader interface {
	Get(context.Context, string) (content.Item, error)
}

type inboxReader interface {
	Item(context.Context, string) (content.Item, error)
}

type documentFetcher interface {
	Fetch(context.Context, string) (content.Item, error)
}

// sourceMedia groups the platform-specific capabilities needed by the
// transcription source resolver without exposing concrete adapter types.
type sourceMedia struct {
	youtube     func(context.Context, string) (transcription.Audio, error)
	douyin      func(context.Context, string) (transcription.Audio, error)
	xiaohongshu func(context.Context, string, string) (transcription.Audio, error)
	locator     func(string) string
	podcast     func(context.Context, string) (content.Item, error)
	bilibili    func(context.Context, string) (transcription.Audio, error)
}
