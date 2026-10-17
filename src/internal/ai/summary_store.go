package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// summaryStore owns the persisted representation of resumable summaries.
// The generation workflow should only ask it for a cache; it should not know
// how summaries are encoded or how legacy rows are promoted.
type summaryStore struct {
	results ResultStore
}

func newSummaryStore(results ResultStore) *summaryStore {
	return &summaryStore{results: results}
}

func (r *summaryStore) read(ctx context.Context, key string) (summaryCache, error) {
	row, err := r.results.AIResult(ctx, kindSummary, key, targetChinese)
	if errors.Is(err, ErrNotFound) {
		return summaryCache{}, ErrNotFound
	}
	if err != nil {
		return summaryCache{}, ErrStorage
	}
	cache, err := decodeSummary(row.Value, key)
	if err != nil {
		return summaryCache{}, err
	}
	cache.Result.Model = row.Model
	return cache, nil
}

func decodeSummary(value, key string) (summaryCache, error) {
	if len(value) > maxStoredValue {
		return summaryCache{}, ErrStorage
	}
	var cache summaryCache
	decoder := json.NewDecoder(strings.NewReader(value))
	if decoder.Decode(&cache) != nil || decoder.Decode(new(any)) != io.EOF || cache.Result.Key != key || cache.Result.TotalCalls <= 0 || cache.Result.CompletedCalls < 0 || cache.Result.CompletedCalls > cache.Result.TotalCalls || len(cache.Current) > 10000 {
		return summaryCache{}, ErrStorage
	}
	return cache, nil
}

func (r *summaryStore) write(ctx context.Context, cache summaryCache) error {
	cache.Result.UpdatedAt = time.Now().Unix()
	value, err := json.Marshal(cache)
	if err != nil || len(value) > maxStoredValue {
		return ErrStorage
	}
	if err := r.results.PutAIResult(ctx, StoredResult{Kind: kindSummary, SourceHash: cache.Result.Key, Target: targetChinese, Model: cache.Result.Model, Value: string(value), CreatedAt: cache.Result.UpdatedAt}); err != nil {
		return ErrStorage
	}
	return nil
}

// load reads the current summary and promotes a matching pre-database cache
// on demand. Legacy lookup remains here so the generation workflow has one
// persistence boundary for both current and migrated data.
func (r *summaryStore) load(ctx context.Context, key string, in SummaryInput, segments []SummarySegment, c Config) (summaryCache, error) {
	cache, err := r.read(ctx, key)
	if !errors.Is(err, ErrNotFound) {
		if err == nil {
			cache.Result.ContentID, cache.Result.Source = in.ContentID, in.Source
		}
		return cache, err
	}
	rows, err := r.results.AIResults(ctx, kindLegacySummary, legacyTarget(in.ContentID, in.Source))
	if err != nil {
		return summaryCache{}, ErrStorage
	}
	for _, row := range rows {
		legacy, err := decodeSummary(row.Value, row.SourceHash)
		if err != nil {
			continue
		}
		model := Config{BaseURL: legacy.Result.BaseURL, Model: legacy.Result.Model}
		revised := in
		revised.Revision = c.Revision
		if summaryIdentity(in, segments, model) != row.SourceHash && legacySummaryIdentity(revised, segments, model) != row.SourceHash {
			continue
		}
		legacy.Result.Key = key
		if err := r.write(ctx, legacy); err != nil {
			return summaryCache{}, err
		}
		_ = r.results.DeleteAIResult(ctx, row.Kind, row.SourceHash, row.Target)
		return legacy, nil
	}
	return summaryCache{}, ErrNotFound
}
