package discovery

import "context"

type recommendationSessionKey struct{}

func WithRecommendationSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, recommendationSessionKey{}, session)
}

func RecommendationSession(ctx context.Context) string {
	value, _ := ctx.Value(recommendationSessionKey{}).(string)
	return value
}
