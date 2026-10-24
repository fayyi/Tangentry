package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryInspectionRejectsMismatchedPlanAndReusesOtherMetadata(t *testing.T) {
	s, _, root := configured(t, nil)
	in := SummaryInput{ContentID: "rss:article-example", Source: "article:feed", Text: "Original paragraph"}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		t.Fatal(err)
	}
	key := summaryKey(in, segments)
	stored := summaryCache{Result: SummaryResult{Key: key, ContentID: "rss:another-article", Source: in.Source, BaseURL: "https://another-fixture.example/v1", Model: "another-model", TotalCalls: 1}}
	if err := s.summaryWrite(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	inspection, err := s.SummaryInspect(context.Background(), in)
	if err != nil || inspection.Result.Key != key || inspection.Result.ContentID != in.ContentID || inspection.Result.Model != "another-model" {
		t.Fatal("stored summary of the same text not reused", err)
	}
	stored.Result.TotalCalls = 2
	if err := s.summaryWrite(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SummaryInspect(context.Background(), in); !errors.Is(err, ErrStorage) {
		t.Fatal("mismatched plan shown", err)
	}
	if err := storeFor(root).PutAIResult(context.Background(), StoredResult{Kind: kindSummary, SourceHash: key, Target: targetChinese, Model: "m", Value: strings.Repeat(" ", maxStoredValue+1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SummaryCached(context.Background(), key); !errors.Is(err, ErrStorage) {
		t.Fatal("oversized value accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SummaryInspect(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled inspection read storage", err)
	}
}

func TestLegacyJSONCachesImportTranslationsAndDiscardOldSummaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ai")
	in := source(3)
	summaryIn := SummaryInput{ContentID: "rss:article-example", Source: "article:feed", Text: "Original paragraph", Revision: 1}
	segments, err := normalizeSummaryInput(summaryIn)
	if err != nil {
		t.Fatal(err)
	}
	old := Config{BaseURL: "https://ai-fixture.example/v1", Model: "old-model", Revision: 1}
	legacyTranslation := legacyIdentity(in, old)
	legacySummary := summaryIdentity(summaryIn, segments, old)
	translation := Result{Key: legacyTranslation, ContentID: in.ContentID, Language: in.Track.Language, BaseURL: old.BaseURL, Model: old.Model, Total: 3, Texts: []string{"第一句", "第二句"}, UpdatedAt: 123}
	summary := summaryCache{Result: SummaryResult{Key: legacySummary, ContentID: summaryIn.ContentID, Source: summaryIn.Source, BaseURL: old.BaseURL, Model: old.Model, TotalCalls: 1, CompletedCalls: 1, Complete: true, Overview: "已有摘要"}}
	summary.Result.Points = []SummaryPoint{{Text: "已保存要点", Indices: []int{0}}}
	summary.Result.Outline = []SummaryOutline{{Title: "已有章节", Level: 0, Indices: []int{0}}}
	summary.Result.Citations = buildCitations(SummaryDraft{Overview: summary.Result.Overview, Points: summary.Result.Points, Outline: summary.Result.Outline}, segments)
	for dir, value := range map[string]any{"translations/" + legacyTranslation: translation, "summaries/" + legacySummary: summary} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(dir)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := atomicJSON(filepath.Join(root, dir+".json"), value); err != nil {
			t.Fatal(err)
		}
	}
	// A file that is not a cache entry is left behind untouched.
	if err := os.WriteFile(filepath.Join(root, "translations", "notes.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryResults()
	s, err := New(root, store, translatorFunc(func(_ context.Context, c Config, segments []Segment) ([]Segment, error) {
		if c.Model != "new-model" || len(segments) != 1 || segments[0].Index != 2 {
			t.Fatal("legacy progress not resumed", c.Model, segments)
		}
		return translations(segments), nil
	}), summaryProviderFunc(func(context.Context, Config, SummaryRequest) (SummaryDraft, error) {
		return SummaryDraft{Overview: "新摘要", Points: []SummaryPoint{{Text: "新要点", Indices: []int{0}}}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if store.Count() != 1 {
		t.Fatal("only translation cache should be imported", store.Count())
	}
	for _, dir := range []string{"translations", "summaries"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("imported directory still active", dir)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "translations.imported", "notes.txt")); err != nil || string(data) != "keep" {
		t.Fatal("imported directory was not preserved", err)
	}
	key := "synthetic-only-key"
	status, err := s.Configure(ConfigureInput{BaseURL: old.BaseURL, Model: "new-model", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := s.Inspect(context.Background(), in)
	if err != nil || inspection.Result.Key != identity(in, Config{}).Key || len(inspection.Result.Texts) != 2 || inspection.Result.Model != "old-model" {
		t.Fatal("legacy translation not adopted after a model change", err, inspection.Result)
	}
	in.Revision = status.Revision
	result, err := s.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 3 || result.Texts[1] != "第二句" || result.Model != "new-model" {
		t.Fatal("legacy translation not resumed", err, result)
	}
	summaryIn.Revision = status.Revision
	summaryResult, err := s.SummaryGenerate(context.Background(), summaryIn)
	if err != nil || summaryResult.Key != summaryKey(summaryIn, segments) || summaryResult.Overview != "新摘要" {
		t.Fatal("legacy summary was not regenerated", err, summaryResult)
	}
	if rows, _ := store.AIResults(context.Background(), kindLegacyTranslation, legacyTarget(in.ContentID, in.Track.Language)); len(rows) != 0 {
		t.Fatal("promoted legacy row kept")
	}
	// Restarting does not import the renamed directories again.
	again, err := New(root, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	if store.Count() != 2 {
		t.Fatal("legacy caches imported twice", store.Count())
	}
}

func TestCompletedSummaryWithoutOutlineIsRegenerated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ai")
	provider := summaryProviderFunc(func(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
		return SummaryDraft{Overview: "重新生成", Points: []SummaryPoint{{Text: "要点", Indices: []int{request.Segments[0].Index}}}, Outline: []SummaryOutline{{Title: "新章节", Level: 0, Indices: []int{request.Segments[0].Index}}}}, nil
	})
	s, err := New(root, storeFor(root), nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err := s.Configure(ConfigureInput{BaseURL: "https://summary-fixture.example/v1", Model: "fixture", Key: func() *string { value := "synthetic-only-key"; return &value }(), Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := SummaryInput{ContentID: "rss:legacy-outline", Source: "article", Text: "正文", Revision: status.Revision}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		t.Fatal(err)
	}
	key := summaryKey(in, segments)
	cache := summaryCache{Result: SummaryResult{Key: key, ContentID: in.ContentID, Source: in.Source, TotalCalls: 1, CompletedCalls: 1, Complete: true, Overview: "旧摘要", Points: []SummaryPoint{{Text: "旧要点", Indices: []int{0}}}, Citations: []SummaryCitation{{Index: 0, Quote: "正文"}}}}
	if err := s.summaryWrite(context.Background(), cache); err != nil {
		t.Fatal(err)
	}
	result, err := s.SummaryGenerate(context.Background(), in)
	if err != nil || result.Overview != "重新生成" || len(result.Outline) != 1 {
		t.Fatalf("outline-less cache was not regenerated: %#v %v", result, err)
	}
}
