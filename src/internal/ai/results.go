package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// AI output is keyed by what was sent to the model, never by content ID or
// model: the same text reuses its result from any source and after a model
// change, until the user explicitly regenerates it.
const (
	kindTranslation       = "translation"
	kindTitle             = "title"
	kindSummary           = "summary"
	kindSummaryTitle      = "summary-title"
	kindLegacyTranslation = "legacy-translation"
	kindLegacySummary     = "legacy-summary"
	targetChinese         = "zh-Hans"
	maxStoredValue        = 12 << 20
)

// StoredResult is one row of persisted AI output.
type StoredResult struct {
	Kind       string
	SourceHash string
	Target     string
	Model      string
	Value      string
	CreatedAt  int64
}

// ResultStore persists AI output. Missing rows report ErrNotFound.
type ResultStore interface {
	AIResult(ctx context.Context, kind, sourceHash, target string) (StoredResult, error)
	AIResults(ctx context.Context, kind, target string) ([]StoredResult, error)
	// PutAIResult replaces the row's model and value, keeping its creation time.
	PutAIResult(ctx context.Context, result StoredResult) error
	DeleteAIResult(ctx context.Context, kind, sourceHash, target string) error
}

func hashSource(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
