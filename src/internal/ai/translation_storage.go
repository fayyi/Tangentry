package ai

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// translationValue is the stored form of a translation; the model is a column.
type translationValue struct {
	ContentID      string   `json:"contentId"`
	Language       string   `json:"language"`
	TargetLanguage string   `json:"targetLanguage,omitempty"`
	BaseURL        string   `json:"baseUrl"`
	Total          int      `json:"total"`
	Texts          []string `json:"texts"`
	UpdatedAt      int64    `json:"updatedAt"`
}

// load reads the stored translation of these texts, adopting a matching
// legacy cache entry from any earlier model.
func (s *Service) load(ctx context.Context, in Input) (Result, error) {
	expected := identity(in, Config{})
	result, err := s.read(ctx, translationKind(in), expected.Key)
	if !errors.Is(err, ErrNotFound) {
		if err == nil {
			result.ContentID, result.Language = in.ContentID, in.Track.Language
			if result.TargetLanguage == "" {
				result.TargetLanguage = targetChinese
			}
		}
		return result, err
	}
	rows, err := s.results.AIResults(ctx, kindLegacyTranslation, legacyTarget(in.ContentID, in.Track.Language))
	if err != nil {
		return Result{}, ErrStorage
	}
	for _, row := range rows {
		var legacy Result
		if json.Unmarshal([]byte(row.Value), &legacy) != nil || legacyIdentity(in, Config{BaseURL: legacy.BaseURL, Model: legacy.Model}) != row.SourceHash {
			continue
		}
		if expected.TargetLanguage != targetChinese {
			continue
		}
		legacy.Key = expected.Key
		legacy.TargetLanguage = targetChinese
		if !validResult(legacy) || legacy.Total != expected.Total {
			continue
		}
		if err := s.write(ctx, translationKind(in), legacy); err != nil {
			return Result{}, err
		}
		_ = s.results.DeleteAIResult(ctx, row.Kind, row.SourceHash, row.Target)
		return legacy, nil
	}
	return Result{}, ErrNotFound
}

func (s *Service) write(ctx context.Context, kind string, result Result) error {
	value, err := json.Marshal(translationValue{result.ContentID, result.Language, result.TargetLanguage, result.BaseURL, result.Total, result.Texts, result.UpdatedAt})
	if err != nil || len(value) > maxStoredValue {
		return ErrStorage
	}
	// Target is a fixed storage partition key, not the translation language; the language is part of SourceHash and the stored value.
	if err := s.results.PutAIResult(ctx, StoredResult{Kind: kind, SourceHash: result.Key, Target: targetChinese, Model: result.Model, Value: string(value), CreatedAt: time.Now().Unix()}); err != nil {
		return ErrStorage
	}
	return nil
}

func (s *Service) read(ctx context.Context, kind, key string) (Result, error) {
	// Keep reads in the same fixed storage partition used by write; the requested target is encoded in the cache identity.
	row, err := s.results.AIResult(ctx, kind, key, targetChinese)
	if errors.Is(err, ErrNotFound) {
		return Result{}, ErrNotFound
	}
	if err != nil || len(row.Value) > maxStoredValue {
		return Result{}, ErrStorage
	}
	var value translationValue
	if json.Unmarshal([]byte(row.Value), &value) != nil {
		return Result{}, ErrStorage
	}
	result := Result{Key: key, ContentID: value.ContentID, Language: value.Language, TargetLanguage: value.TargetLanguage, BaseURL: value.BaseURL, Model: row.Model, Total: value.Total, Texts: value.Texts, UpdatedAt: value.UpdatedAt}
	if result.TargetLanguage == "" {
		result.TargetLanguage = targetChinese
	}
	if !validResult(result) {
		return Result{}, ErrStorage
	}
	return result, nil
}
