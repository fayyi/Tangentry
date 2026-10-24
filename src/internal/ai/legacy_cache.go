package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// importLegacyCache moves JSON caches written before AI output lived in the
// database. The migration is isolated from request-time result reads and is
// safe to retry when a file or database write fails.
func importLegacyCache(root string, results ResultStore) {
	for _, legacy := range []struct{ dir, kind string }{{"translations", kindLegacyTranslation}, {"summaries", kindLegacySummary}} {
		dir := filepath.Join(root, legacy.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		if legacy.kind == kindLegacySummary {
			// Summary shape changed incompatibly. Remove legacy summary payloads
			// instead of guessing old overview/points/outline into the new tree.
			failed := false
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".json") && entry.Type().IsRegular() {
					if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
						failed = true
					}
				}
			}
			if failed {
				slog.Warn("legacy summary cache cleanup incomplete; it will be retried on next start", "directory", legacy.dir)
				continue
			}
			if err := os.Rename(dir, dir+".imported"); err != nil {
				slog.Warn("legacy summary cache removed but its directory could not be renamed", "directory", legacy.dir)
			}
			continue
		}
		failed := false
		for _, entry := range entries {
			key, ok := strings.CutSuffix(entry.Name(), ".json")
			if !ok || !keyPattern.MatchString(key) || !entry.Type().IsRegular() {
				continue
			}
			row, err := legacyRow(filepath.Join(dir, entry.Name()), legacy.kind, key)
			if err == nil {
				err = results.PutAIResult(context.Background(), row)
			}
			if err != nil && !errors.Is(err, ErrStorage) {
				failed = true
			}
		}
		if failed {
			slog.Warn("AI cache import incomplete; it will be retried on next start", "directory", legacy.dir)
			continue
		}
		if err := os.Rename(dir, dir+".imported"); err != nil {
			slog.Warn("AI cache imported but its directory could not be renamed", "directory", legacy.dir)
		}
	}
}

func legacyRow(path, kind, key string) (StoredResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return StoredResult{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStoredValue+1))
	if err != nil {
		return StoredResult{}, err
	}
	if len(data) > maxStoredValue {
		return StoredResult{}, ErrStorage
	}
	row := StoredResult{Kind: kind, SourceHash: key, Value: string(data), CreatedAt: time.Now().Unix()}
	if kind == kindLegacyTranslation {
		var result Result
		if json.Unmarshal(data, &result) != nil || result.Key != key || result.Model == "" {
			return StoredResult{}, ErrStorage
		}
		row.Target, row.Model = legacyTarget(result.ContentID, result.Language), result.Model
		if result.UpdatedAt > 0 {
			row.CreatedAt = result.UpdatedAt
		}
	} else {
		var cache summaryCache
		if json.Unmarshal(data, &cache) != nil || cache.Result.Key != key || cache.Result.Model == "" {
			return StoredResult{}, ErrStorage
		}
		row.Target, row.Model = legacyTarget(cache.Result.ContentID, cache.Result.Source), cache.Result.Model
		if cache.Result.UpdatedAt > 0 {
			row.CreatedAt = cache.Result.UpdatedAt
		}
	}
	return row, nil
}

// Legacy rows are found by the content and track or source they were made for.
func legacyTarget(contentID, scope string) string { return contentID + "\n" + scope }
