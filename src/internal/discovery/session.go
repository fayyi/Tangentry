package discovery

import "context"

type recommendationSessionKey struct{}
type recommendationFilterKey struct{}

func WithRecommendationSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, recommendationSessionKey{}, session)
}

func RecommendationSession(ctx context.Context) string {
	value, _ := ctx.Value(recommendationSessionKey{}).(string)
	return value
}

// WithRecommendationFilter carries a platform feed's native filter without
// widening the application-level recommendation function signature.
func WithRecommendationFilter(ctx context.Context, filter string) context.Context {
	return context.WithValue(ctx, recommendationFilterKey{}, filter)
}

func RecommendationFilter(ctx context.Context) string {
	value, _ := ctx.Value(recommendationFilterKey{}).(string)
	return value
}
