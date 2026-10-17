package sqlite

import (
	"context"
	"tangentry/internal/discovery"
)

func (s *Store) RecommendationPreferences(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source,enabled FROM recommendation_preferences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]bool{}
	for rows.Next() {
		var source string
		var enabled bool
		if err := rows.Scan(&source, &enabled); err != nil {
			return nil, err
		}
		values[source] = enabled
	}
	return values, rows.Err()
}

func (s *Store) SaveRecommendationPreference(ctx context.Context, source string, enabled bool) error {
	if !discovery.RecommendationPlatform(source) {
		return discovery.ErrInvalidQuery
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO recommendation_preferences(source,enabled) VALUES(?,?) ON CONFLICT(source) DO UPDATE SET enabled=excluded.enabled`, source, enabled)
	return err
}
