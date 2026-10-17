package subscriptions

import (
	"context"
	"errors"
	"sort"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

// Gallery reads confirmed snapshots without updating or collecting content.
func (s *Service) Gallery(ctx context.Context) (discovery.Feed, error) {
	sources, err := s.List(ctx)
	if err != nil {
		return discovery.Feed{}, err
	}
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].LastUpdatedAt > sources[j].LastUpdatedAt })
	result := discovery.Feed{Items: []content.Item{}, Page: 1, SourceTitle: "订阅来源 · 最近成功更新"}
	seen := map[string]bool{}
	for _, source := range sources[:min(len(sources), 50)] {
		if source.LastUpdatedAt == 0 {
			continue
		}
		detail, err := s.Get(ctx, source.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return discovery.Feed{}, err
		}
		for _, item := range detail.Items[:min(len(detail.Items), 20)] {
			if !seen[item.ID] {
				seen[item.ID] = true
				result.Items = append(result.Items, item)
			}
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool { return result.Items[i].PublishedAt > result.Items[j].PublishedAt })
	result.Items = result.Items[:min(len(result.Items), 200)]
	result.Total = len(result.Items)
	return result, nil
}
