package httptransport

import (
	"context"

	"tangentry/internal/discovery"
)

type DiscoveryService interface {
	Search(context.Context, string, int) (discovery.Feed, error)
}

type RecommendationQueue interface {
	PendingPage(context.Context, string, int, string) (discovery.QueuePage, error)
	Mark(context.Context, string, string, bool) error
	Fetch(context.Context, string, func(context.Context) (discovery.Feed, error)) (discovery.Feed, error)
}

type RecommendationConfig interface {
	List(context.Context) ([]discovery.RecommendationPreference, error)
	Save(context.Context, string, bool) error
	Enabled(context.Context, string) (bool, error)
}
