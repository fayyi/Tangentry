package discovery

import "context"

var recommendationPlatforms = []string{"bilibili", "youtube", "xiaohongshu", "linuxdo", "xiaoyuzhou", "douyin"}

func RecommendationPlatform(source string) bool {
	for _, platform := range recommendationPlatforms {
		if source == platform {
			return true
		}
	}
	return false
}

type RecommendationPreference struct {
	Source  string `json:"source"`
	Enabled bool   `json:"enabled"`
}

type PreferenceRepository interface {
	RecommendationPreferences(context.Context) (map[string]bool, error)
	SaveRecommendationPreference(context.Context, string, bool) error
}

type Preferences struct{ repository PreferenceRepository }

func NewPreferences(repository PreferenceRepository) *Preferences {
	return &Preferences{repository: repository}
}

func (p *Preferences) List(ctx context.Context) ([]RecommendationPreference, error) {
	saved, err := p.repository.RecommendationPreferences(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]RecommendationPreference, 0, len(recommendationPlatforms))
	for _, source := range recommendationPlatforms {
		enabled, exists := saved[source]
		if !exists {
			// Preserve existing sources; the experimental Douyin adapter is opt-in.
			enabled = source != "douyin"
		}
		result = append(result, RecommendationPreference{Source: source, Enabled: enabled})
	}
	return result, nil
}

func (p *Preferences) Enabled(ctx context.Context, source string) (bool, error) {
	if !RecommendationPlatform(source) {
		return false, ErrInvalidQuery
	}
	values, err := p.List(ctx)
	if err != nil {
		return false, err
	}
	for _, value := range values {
		if value.Source == source {
			return value.Enabled, nil
		}
	}
	return false, ErrInvalidQuery
}

func (p *Preferences) Save(ctx context.Context, source string, enabled bool) error {
	if !RecommendationPlatform(source) {
		return ErrInvalidQuery
	}
	return p.repository.SaveRecommendationPreference(ctx, source, enabled)
}
