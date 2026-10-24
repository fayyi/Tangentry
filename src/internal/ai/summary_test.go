package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

type summaryProviderFunc func(context.Context, Config, SummaryRequest) (SummaryDraft, error)

func (f summaryProviderFunc) Summarize(ctx context.Context, c Config, r SummaryRequest) (SummaryDraft, error) {
	return f(ctx, c, r)
}

type combinedAIProvider struct{ summaryProviderFunc }

type documentSummaryProvider struct {
	calls int
}

func (p *documentSummaryProvider) Translate(context.Context, Config, []Segment) ([]Segment, error) {
	return nil, ErrProvider
}
func (p *documentSummaryProvider) SupportsSummaryDocument(Config) bool { return true }
func (p *documentSummaryProvider) Summarize(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
	p.calls++
	if request.Mode != "document" {
		return SummaryDraft{}, ErrFormat
	}
	return SummaryDraft{Overview: "一次完成", Points: []SummaryPoint{{Text: "关键点", Indices: []int{0}}}, Document: &SummaryDocument{Sections: []SummarySection{{Title: "章节", Source: []SummaryEvidence{{Index: 0, Quote: request.Segments[0].Text}}, Units: []SummaryUnit{{Takeaway: "关键点", Full: request.Segments[0].Text, Sentences: []SummarySentence{{Text: request.Segments[0].Text, Evidence: []SummaryEvidence{{Index: 0, Quote: request.Segments[0].Text}}}}, BriefSentenceIDs: []string{"sentence"}}}}}}}, nil
}

func TestShortSummaryUsesSingleDocumentCallWhenProviderSupportsIt(t *testing.T) {
	provider := &documentSummaryProvider{}
	service, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(ConfigureInput{BaseURL: "https://summary-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SummaryGenerate(context.Background(), SummaryInput{ContentID: "rss:document-mode", Source: "article:original", Text: "原文", Revision: status.Revision})
	if err != nil || !result.Complete || result.CompletedCalls != 1 || result.Document == nil || provider.calls != 1 {
		t.Fatalf("document mode was not used: %#v calls=%d err=%v", result, provider.calls, err)
	}
}

func TestSummaryKeepsMoreThanEightValidCitationsAndReusesSavedResult(t *testing.T) {
	calls := 0
	provider := combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
		calls++
		indices := make([]int, len(request.Segments))
		for i, segment := range request.Segments {
			indices[i] = segment.Index
		}
		return SummaryDraft{Overview: "跨多句的完整概述", Points: []SummaryPoint{{Text: "这些原文共同支持一个要点", Indices: indices}}}, nil
	})}
	root := filepath.Join(t.TempDir(), "ai")
	service, err := New(root, storeFor(root), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(ConfigureInput{BaseURL: "https://summary-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := SummaryInput{ContentID: "youtube:ManyRefs001", Source: "subtitle", Revision: status.Revision}
	for i := 0; i < 11; i++ {
		input.Segments = append(input.Segments, SummarySegment{Index: i, Start: float64(i), End: float64(i + 1), Text: fmt.Sprintf("Original source %d", i)})
	}
	result, err := service.SummaryGenerate(context.Background(), input)
	if err != nil || !result.Complete || len(result.Points[0].Indices) != 11 || len(result.Citations) != 11 || calls != 1 {
		t.Fatal("valid long citation list rejected", result, err, calls)
	}
	for i, citation := range result.Citations {
		if citation.Index != i || citation.Start != input.Segments[i].Start || citation.Quote != input.Segments[i].Text {
			t.Fatal("citation changed", citation)
		}
	}
	reopened, err := New(root, storeFor(root), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cached, err := reopened.SummaryGenerate(context.Background(), input)
	if err != nil || cached.Key != result.Key || len(cached.Citations) != 11 || calls != 1 {
		t.Fatal("completed summary was regenerated", cached, err, calls)
	}
}

func TestSummaryCitationLimitStillRejectsExcessDuplicateAndInventedIndices(t *testing.T) {
	allowed := map[int]bool{}
	indices := make([]int, summaryPointIndexLimit+1)
	for i := range indices {
		indices[i], allowed[i] = i, true
	}
	for _, tc := range []struct {
		indices []int
		valid   bool
	}{
		{indices[:summaryPointIndexLimit], true}, {indices, false},
		{[]int{0, 0}, false}, {[]int{99999}, false}, {nil, false},
	} {
		draft := SummaryDraft{Overview: "概述", Points: []SummaryPoint{{Text: "要点", Indices: tc.indices}}}
		if validateDraft(draft, allowed) != tc.valid {
			t.Fatal("citation validation changed", tc.indices)
		}
	}
}

func TestSummaryCitationJSONPreservesZeroTimestampAndUntimedArticles(t *testing.T) {
	for _, citation := range []SummaryCitation{
		{Index: 0, Start: 0, End: 3.125, Quote: "视频开头"},
		{Index: 1, Start: 4.5, End: 8.75, Quote: "视频后段"},
		{Index: 2, Quote: "文章原文"},
	} {
		encoded, err := json.Marshal(citation)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		_, start := fields["start"]
		_, end := fields["end"]
		timed := citation.End > citation.Start
		if start != timed || end != timed {
			t.Fatalf("incorrect time presence: %s", encoded)
		}
		var restored SummaryCitation
		if err := json.Unmarshal(encoded, &restored); err != nil || restored != citation {
			t.Fatalf("citation changed on cache roundtrip: %+v %v", restored, err)
		}
	}
}

func TestSummaryPointsMayShareSourceWithoutDuplicatingCitation(t *testing.T) {
	draft := SummaryDraft{Overview: "同一段原文包含两条事实", Points: []SummaryPoint{
		{Text: "笔记与原文关联", Indices: []int{0}},
		{Text: "笔记支持再次查阅", Indices: []int{0}},
	}}
	if !validateDraft(draft, map[int]bool{0: true}) {
		t.Fatal("different points must be allowed to cite the same source")
	}
	citations := buildCitations(draft, []SummarySegment{{Index: 0, Text: "Notes stay linked to the article and can be reviewed later."}})
	if len(citations) != 1 || citations[0].Index != 0 {
		t.Fatal("shared source should appear once in citation storage", citations)
	}
	for _, indices := range [][]int{{0, 0}, {99}, {}} {
		invalid := SummaryDraft{Overview: draft.Overview, Points: []SummaryPoint{{Text: "无效引用", Indices: indices}}}
		if validateDraft(invalid, map[int]bool{0: true}) {
			t.Fatal("duplicate, fabricated or missing references accepted", indices)
		}
	}
	calls := 0
	service, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), combinedAIProvider{summaryProviderFunc(func(context.Context, Config, SummaryRequest) (SummaryDraft, error) {
		calls++
		return draft, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(ConfigureInput{BaseURL: "https://summary-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := SummaryInput{ContentID: "rss:shared-source", Source: "article:original", Text: citations[0].Quote, Revision: status.Revision}
	result, err := service.SummaryGenerate(context.Background(), input)
	if err != nil || !result.Complete || len(result.Points) != 2 || len(result.Citations) != 1 {
		t.Fatal("valid shared-source summary rejected", result, err)
	}
	reopened, err := New(filepath.Dir(service.settingsPath), service.results, service.provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cached, err := reopened.SummaryGenerate(context.Background(), input)
	if err != nil || !cached.Complete || len(cached.Points) != 2 || calls != 1 {
		t.Fatal("shared-source summary cache was not reused", cached, err, calls)
	}
}

func TestLongSummaryParagraphPreservesEveryUnicodeCharacter(t *testing.T) {
	for _, unit := range []string{"汉", "é", "😀", "中文😀é𠮷"} {
		for prefix := 0; prefix < 4; prefix++ {
			text := strings.Repeat("a", prefix) + strings.Repeat(unit, 9000) + "结束"
			in := SummaryInput{ContentID: "rss:unicode-summary", Source: "article:original", Text: text}
			segments, err := normalizeSummaryInput(in)
			if err != nil {
				t.Fatal(err)
			}
			var restored strings.Builder
			for index, segment := range segments {
				if segment.Index != index || len(segment.Text) > summaryLeafBytes || !utf8.ValidString(segment.Text) {
					t.Fatalf("invalid boundary: unit %q, prefix %d, segment %d", unit, prefix, index)
				}
				restored.WriteString(segment.Text)
			}
			if restored.String() != text {
				t.Fatal("Unicode characters lost or duplicated")
			}
		}
	}
}

func TestArticleSummaryCitationsPreserveParagraphWhitespace(t *testing.T) {
	for _, text := range []string{
		"  第一段。\n\n\t第二段。  \n\n结尾😀",
		"标题\r\n\r\n正文带有缩进\r\n  下一行",
		strings.Repeat("原文😀 ", 1500) + "\n\n" + strings.Repeat("  第二段。\n", 1000),
	} {
		segments, err := normalizeSummaryInput(SummaryInput{ContentID: "rss:exact-source", Source: "article:original", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		var restored strings.Builder
		for i, segment := range segments {
			if segment.Index != i || len(segment.Text) > summaryLeafBytes || !utf8.ValidString(segment.Text) || !strings.Contains(text, segment.Text) {
				t.Fatal("citation is not an exact bounded original substring", segment)
			}
			restored.WriteString(segment.Text)
		}
		if restored.String() != text {
			t.Fatal("paragraph separators or whitespace changed")
		}
	}
}

func TestLongUnicodeSummaryPersistsExactCitationsAndReusesCache(t *testing.T) {
	calls := 0
	text := "  原文标题\r\n\r\n" + strings.Repeat("中文😀é𠮷", 2500) + "\n\n  完整结尾"
	s, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, r SummaryRequest) (SummaryDraft, error) {
		calls++
		index := 0
		if r.Mode == "leaf" {
			for _, segment := range r.Segments {
				if !utf8.ValidString(segment.Text) || !strings.Contains(text, segment.Text) {
					t.Fatal("provider received damaged source")
				}
			}
			index = r.Segments[len(r.Segments)-1].Index
		} else {
			index = r.Drafts[len(r.Drafts)-1].Points[0].Indices[0]
		}
		return SummaryDraft{Overview: "长文概述", Points: []SummaryPoint{{Text: "完整结尾", Indices: []int{index}}}}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err := s.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := SummaryInput{ContentID: "rss:unicode-summary", Source: "article:original", Text: text, Revision: status.Revision}
	result, err := s.SummaryGenerate(context.Background(), in)
	if err != nil || !result.Complete || len(result.Citations) != 1 || !strings.HasSuffix(result.Citations[0].Quote, "完整结尾") {
		t.Fatal(err, result)
	}
	if !utf8.ValidString(result.Citations[0].Quote) || !strings.Contains(text, result.Citations[0].Quote) {
		t.Fatal("citation not in original")
	}
	reopened, err := New(filepath.Dir(s.settingsPath), s.results, s.provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	before := calls
	cached, err := reopened.SummaryGenerate(context.Background(), in)
	if err != nil || cached.Key != result.Key || calls != before {
		t.Fatal("cache reuse failed", err)
	}
}
func TestSeparateSummaryModelPreservesTranslationAndSummaryCaches(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ai")
	translationCalls, summaryCalls := 0, 0
	s, err := New(root, storeFor(root), translatorFunc(func(_ context.Context, c Config, segments []Segment) ([]Segment, error) {
		translationCalls++
		if c.Model != "translate-model" {
			t.Fatal("translation used summary model", c.Model)
		}
		return translations(segments), nil
	}), summaryProviderFunc(func(_ context.Context, c Config, r SummaryRequest) (SummaryDraft, error) {
		summaryCalls++
		if c.Model != "summary-model" {
			t.Fatal("summary used translation model", c.Model)
		}
		return SummaryDraft{Overview: "摘要", Points: []SummaryPoint{{Text: "要点", Indices: []int{r.Segments[0].Index}}}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summaryModel := "summary-model"
	status, err := s.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "translate-model", SummaryModel: &summaryModel, Revision: 1})
	if err != nil || !status.Ready || !status.SummaryReady {
		t.Fatal(err, status)
	}
	input := source(1)
	input.Revision = status.Revision
	translation, err := s.Translate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	summaryInput := SummaryInput{ContentID: input.ContentID, Source: "article:original", Text: "Original article", Revision: status.Revision}
	summary, err := s.SummaryGenerate(context.Background(), summaryInput)
	if err != nil || summary.Model != summaryModel {
		t.Fatal(err, summary)
	}
	// Older clients may omit the new field. Retain it and reuse both caches.
	status, err = s.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: status.Model, Revision: status.Revision})
	if err != nil || status.SummaryModel != summaryModel {
		t.Fatal("omitted field cleared model", err)
	}
	reopened, err := New(root, storeFor(root), s.provider, s.summarizer)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	input.Revision = status.Revision
	summaryInput.Revision = status.Revision
	cachedTranslation, err := reopened.Translate(context.Background(), input)
	if err != nil || cachedTranslation.Key != translation.Key || translationCalls != 1 {
		t.Fatal("translation cache lost", err)
	}
	cachedSummary, err := reopened.SummaryGenerate(context.Background(), summaryInput)
	if err != nil || cachedSummary.Key != summary.Key || summaryCalls != 1 {
		t.Fatal("summary cache lost", err)
	}
	// Replacing only the translation model leaves the dedicated summary cache reusable.
	status, err = reopened.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: "other-translation", Revision: status.Revision})
	if err != nil {
		t.Fatal(err)
	}
	summaryInput.Revision = status.Revision
	cachedSummary, err = reopened.SummaryGenerate(context.Background(), summaryInput)
	if err != nil || cachedSummary.Key != summary.Key || summaryCalls != 1 {
		t.Fatal("unrelated model change invalidated summary", err)
	}
	cleared := ""
	status, err = reopened.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: status.Model, SummaryModel: &cleared, Revision: status.Revision})
	if err != nil || status.SummaryModel != "" {
		t.Fatal("explicit clear failed", err)
	}
	inspection, err := reopened.SummaryInspect(context.Background(), summaryInput)
	if err != nil || inspection.Result.Key != summary.Key || !inspection.Result.Complete || inspection.Result.Model == "other-translation" {
		t.Fatal("model fallback discarded the stored summary", err)
	}
}

func TestSummaryResumesInterruptedReductionWithoutRepeatingLeavesOrGroups(t *testing.T) {
	calls := 0
	provider := combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, r SummaryRequest) (SummaryDraft, error) {
		calls++
		if calls == 9 {
			return SummaryDraft{}, ErrGatewayTimeout
		}
		index := 0
		if r.Mode == "leaf" {
			if calls > 7 {
				t.Fatal("resume repeated leaf")
			}
			index = r.Segments[0].Index
		} else {
			index = r.Drafts[0].Points[0].Indices[0]
			if calls == 10 && index != 144 {
				t.Fatal("resume repeated completed reduction", index)
			}
		}
		return SummaryDraft{Overview: "摘要", Points: []SummaryPoint{{Text: "要点", Indices: []int{index}}}}, nil
	})}
	root := filepath.Join(t.TempDir(), "ai")
	s, err := New(root, storeFor(root), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err := s.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := SummaryInput{ContentID: "youtube:jNQXAC9IVRw", Source: "subtitle", Revision: status.Revision}
	for i := 0; i < 145; i++ {
		in.Segments = append(in.Segments, SummarySegment{Index: i, Start: float64(i), End: float64(i) + 1, Text: "Original cue"})
	}
	if _, err := s.SummaryGenerate(context.Background(), in); !errors.Is(err, ErrGatewayTimeout) {
		t.Fatal(err)
	}
	reopened, err := New(root, storeFor(root), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := reopened.SummaryGenerate(context.Background(), in)
	if err != nil || !result.Complete || result.CompletedCalls != result.TotalCalls || calls != 11 {
		t.Fatal("reduction resume failed", err, calls, result)
	}
}

func TestCompletedSummaryCacheCitationIntegrity(t *testing.T) {
	segments := []SummarySegment{{Index: 0, Start: 1.25, End: 3.5, Text: "Original subtitle"}}
	draft := SummaryDraft{Overview: "摘要", Points: []SummaryPoint{{Text: "要点", Indices: []int{0}}}}
	base := SummaryResult{Complete: true, TotalCalls: 1, CompletedCalls: 1, Overview: draft.Overview, Points: draft.Points, Citations: buildCitations(draft, segments)}
	if !validCachedSummary(base, segments) {
		t.Fatal("valid summary rejected")
	}
	for _, mutate := range []func(*SummaryResult){
		func(r *SummaryResult) { r.Citations[0].Start++ },
		func(r *SummaryResult) { r.Citations[0].End++ },
		func(r *SummaryResult) { r.Citations[0].Quote = "Other content" },
		func(r *SummaryResult) { r.Citations[0].Index = 1 },
		func(r *SummaryResult) { r.Citations = nil },
		func(r *SummaryResult) { r.Points = []SummaryPoint{{Text: "要点", Indices: []int{99}}} },
		func(r *SummaryResult) { r.CompletedCalls = 0 },
	} {
		result := base
		result.Citations = append([]SummaryCitation(nil), base.Citations...)
		mutate(&result)
		if validCachedSummary(result, segments) {
			t.Fatal("invalid cached citation accepted")
		}
	}
}

func TestSummaryIdentityIgnoresSettingsRevision(t *testing.T) {
	in := SummaryInput{ContentID: "rss:article-example", Source: "article:feed", Text: "Original article", Revision: 0}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{BaseURL: "https://ai-fixture.example/v1", Model: "fixture"}
	key := summaryIdentity(in, segments, c)
	in.Revision = 42
	if summaryIdentity(in, segments, c) != key {
		t.Fatal("settings revision changes summary meaning")
	}
	c.Model = "another-model"
	if summaryIdentity(in, segments, c) == key {
		t.Fatal("different model reused summary")
	}
}

func (combinedAIProvider) Translate(context.Context, Config, []Segment) ([]Segment, error) {
	return nil, ErrProvider
}

func TestSummaryUsesExplicitCallsResumeAndBuildsTimedCitations(t *testing.T) {
	calls := 0
	provider := combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
		calls++
		if calls == 2 {
			return SummaryDraft{}, ErrRate
		}
		if request.Mode == "leaf" {
			return SummaryDraft{Overview: "这是摘要", Points: []SummaryPoint{{Text: "关键内容", Indices: []int{request.Segments[0].Index}}}}, nil
		}
		return SummaryDraft{Overview: "总览", Points: []SummaryPoint{{Text: "最终关键点", Indices: []int{request.Drafts[0].Points[0].Indices[0]}}}}, nil
	})}
	service, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key := "synthetic-key"
	status, err := service.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	segments := make([]SummarySegment, 25)
	for i := range segments {
		segments[i] = SummarySegment{Index: i, Start: float64(i), End: float64(i) + .5, Text: "cue"}
	}
	input := SummaryInput{ContentID: "youtube:jNQXAC9IVRw", Source: "subtitle", Revision: status.Revision, Segments: segments}
	inspection, err := service.SummaryInspect(context.Background(), input)
	if err != nil || inspection.Result.Complete || inspection.Result.TotalCalls != 3 || calls != 0 {
		t.Fatalf("inspection: %#v %v calls=%d", inspection, err, calls)
	}
	if _, err = service.SummaryGenerate(context.Background(), input); !errors.Is(err, ErrRate) {
		t.Fatalf("expected partial failure, got %v", err)
	}
	partial, err := service.SummaryInspect(context.Background(), input)
	if err != nil || partial.Result.CompletedCalls != 1 || partial.Result.Complete {
		t.Fatalf("partial: %#v %v", partial, err)
	}
	if partial.Result.Progress.CurrentCall != 0 || partial.Result.Progress.Phase != "error" || partial.Result.Progress.Error == "" {
		t.Fatalf("failed call did not leave resumable progress: %#v", partial.Result.Progress)
	}
	result, err := service.SummaryGenerate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || result.Overview != "总览" || result.CompletedCalls != 3 || len(result.Citations) != 1 || result.Citations[0].Start != 0 || calls != 4 {
		t.Fatalf("result: %#v calls=%d", result, calls)
	}
	if _, err := service.SummaryGenerate(context.Background(), input); err != nil || calls != 4 {
		t.Fatalf("cached summary invoked provider: %v calls=%d", err, calls)
	}
}

func TestSummaryFinalReductionTimeoutResumesWithVerifiedDrafts(t *testing.T) {
	calls := 0
	provider := combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
		calls++
		if request.Mode == "reduce" && len(request.Drafts) == 3 {
			return SummaryDraft{}, context.DeadlineExceeded
		}
		if request.Mode == "leaf" {
			return SummaryDraft{Overview: "章节摘要", Points: []SummaryPoint{{Text: "章节要点", Indices: []int{request.Segments[0].Index}}}, Outline: []SummaryOutline{{Title: "章节", Level: 0, Indices: []int{request.Segments[0].Index}}}}, nil
		}
		return SummaryDraft{Overview: "合并摘要", Points: []SummaryPoint{{Text: "合并要点", Indices: []int{request.Drafts[0].Points[0].Indices[0]}}}, Outline: []SummaryOutline{{Title: "合并", Level: 0, Indices: []int{request.Drafts[0].Points[0].Indices[0]}}}}, nil
	})}
	service, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	segments := make([]SummarySegment, 36)
	for i := range segments {
		segments[i] = SummarySegment{Index: i, Text: strings.Repeat("cue ", 700)}
	}
	input := SummaryInput{ContentID: "youtube:final-reduction", Source: "subtitle", Revision: status.Revision, Segments: segments}
	result, err := service.SummaryGenerate(context.Background(), input)
	if err != nil || !result.Complete || result.CompletedCalls != result.TotalCalls {
		t.Fatalf("final reduction did not recover locally: complete=%v calls=%d/%d err=%v", result.Complete, result.CompletedCalls, result.TotalCalls, err)
	}
	beforeRetry := calls
	cached, err := service.SummaryGenerate(context.Background(), input)
	if err != nil || !cached.Complete || calls != beforeRetry {
		t.Fatalf("recovered summary was regenerated: calls=%d before=%d err=%v", calls, beforeRetry, err)
	}
}

func TestSummaryArticleChunkingDoesNotDropTail(t *testing.T) {
	provider := combinedAIProvider{summaryProviderFunc(func(_ context.Context, _ Config, request SummaryRequest) (SummaryDraft, error) {
		if request.Mode == "leaf" {
			return SummaryDraft{Overview: fmt.Sprintf("chunk %d", request.Segments[0].Index), Points: []SummaryPoint{{Text: "point", Indices: []int{request.Segments[0].Index}}}}, nil
		}
		return SummaryDraft{Overview: "reduced", Points: []SummaryPoint{{Text: "point", Indices: []int{request.Drafts[0].Points[0].Indices[0]}}}}, nil
	})}
	service, err := New(filepath.Join(t.TempDir(), "ai"), NewMemoryResults(), provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	status, err := service.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	text := "第一段\n\n" + string(make([]byte, 0)) + "最后一段"
	input := SummaryInput{ContentID: "rss:article-example", Source: "article:feed", Text: text, Revision: status.Revision}
	result, err := service.SummaryGenerate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Citations) == 0 {
		t.Fatalf("article summary incomplete: %#v", result)
	}
	// The canonical article path is accepted and always remains explicit; no
	// implicit article fetch occurs in the AI service.
}

func TestSummaryPlanUsesOneCallWithinBudget(t *testing.T) {
	segments := []SummarySegment{{Index: 0, Text: "第一段"}, {Index: 1, Text: "第二段"}}
	groups, calls := summaryPlan(segments, 28000)
	if len(groups) != 1 || calls != 1 || len(groups[0]) != 2 {
		t.Fatalf("unexpected one-call plan: %#v %d", groups, calls)
	}
}

func TestSummaryKeyIncludesPromptAndBudget(t *testing.T) {
	in := SummaryInput{ContentID: "rss:key-test", Source: "article", Text: "正文"}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		t.Fatal(err)
	}
	base := summaryKey(in, segments, "提示词", "28000")
	if base == summaryKey(in, segments, "另一套提示词", "28000") || base == summaryKey(in, segments, "提示词", "4000") {
		t.Fatal("summary cache key ignored prompt or budget")
	}
}

func TestSummaryKeyIncludesCustomUserTemplate(t *testing.T) {
	in := SummaryInput{ContentID: "rss:key-user-template", Source: "article", Text: "正文"}
	segments, err := normalizeSummaryInput(in)
	if err != nil {
		t.Fatal(err)
	}
	base := summaryKey(in, segments, "系统", "28000", "用户 {{content}}")
	if base == summaryKey(in, segments, "系统", "28000", "另一套用户 {{content}}") {
		t.Fatal("summary cache key ignored custom user template")
	}
}

func TestSummaryOutlineValidationAndNormalization(t *testing.T) {
	segments := []SummarySegment{{Index: 0, Start: 3, End: 5, Text: "段落"}}
	draft := SummaryDraft{Overview: "概述", Points: []SummaryPoint{{Text: "要点", Indices: []int{0}}}, Outline: []SummaryOutline{{Title: "章节", Level: 0, Indices: []int{0}, Children: []SummaryOutline{{Title: "小节", Level: 1, Indices: []int{0}}}}}}
	if !validateDraft(draft, map[int]bool{0: true}) {
		t.Fatal("valid outline rejected")
	}
	outline := normalizeOutline(draft.Outline, segments)
	if len(outline) != 1 || outline[0].Start != 3 || len(outline[0].Children) != 1 {
		t.Fatalf("outline was not normalized: %#v", outline)
	}
	invalid := draft
	invalid.Outline = []SummaryOutline{{Title: "越级", Level: 2, Indices: []int{0}}}
	if validateDraft(invalid, map[int]bool{0: true}) {
		t.Fatal("invalid outline accepted")
	}
}

func TestSummaryInputValidationBoundariesAndAutomaticCueShape(t *testing.T) {
	automatic := make([]SummarySegment, 167)
	for index := range automatic {
		automatic[index] = SummarySegment{Index: index, Start: float64(index), End: float64(index + 1), Text: fmt.Sprintf("自动字幕 %d", index)}
	}
	segments, err := normalizeSummaryInput(SummaryInput{ContentID: "bilibili:BVfixture1234", Source: "subtitle", Segments: automatic})
	if err != nil || len(segments) != 167 || segments[0].Index != 0 || segments[166].Index != 166 {
		t.Fatalf("automatic subtitles rejected or reordered: %v %#v", err, segments)
	}
	oneMiB := strings.Repeat("a", maxSummaryTextBytes)
	if _, err := normalizeSummaryInput(SummaryInput{ContentID: "rss:fixture", Source: "article:original", Text: oneMiB}); err != nil {
		t.Fatalf("1 MiB text should be accepted: %v", err)
	}
	if _, err := normalizeSummaryInput(SummaryInput{ContentID: "rss:fixture", Source: "article:original", Text: oneMiB + "a"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("text over 1 MiB was accepted: %v", err)
	}
	for _, invalid := range []SummaryInput{
		{ContentID: "rss:fixture", Source: "article:original", Segments: []SummarySegment{{Index: 1, Text: "跳过 0"}}},
		{ContentID: "rss:fixture", Source: "article:original", Segments: []SummarySegment{{Index: 0, Start: 2, End: 1, Text: "时间倒退"}}},
		{ContentID: "rss:fixture", Source: "article:original", Segments: []SummarySegment{{Index: 0, Text: "   "}}},
	} {
		if _, err := normalizeSummaryInput(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid cue accepted: %#v", invalid)
		}
	}
}

func TestSummaryDocumentUsesStructuredHeadingsAsStableSections(t *testing.T) {
	segments, err := normalizeSummaryInput(SummaryInput{ContentID: "rss:structured", Source: "article:feed", Blocks: []SummaryBlock{
		{Kind: "heading", Text: "第一章"},
		{Kind: "paragraph", Text: "第一段"},
		{Kind: "heading", Text: "第二章"},
		{Kind: "paragraph", Text: "第二段"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := SummaryResult{ContentID: "rss:structured", Source: "article:feed"}
	document := buildSummaryDocument(result, SummaryDraft{Overview: "总览", Points: []SummaryPoint{{Text: "第一点", Indices: []int{1}}, {Text: "第二点", Indices: []int{3}}}}, segments)
	if len(document.Sections) != 2 || document.Sections[0].Title != "第一章" || document.Sections[1].Title != "第二章" {
		t.Fatalf("structured headings not preserved: %#v", document.Sections)
	}
	if document.Sections[0].SectionID == document.Sections[1].SectionID || len(document.Sections[0].Units) != 1 || len(document.Sections[1].Units) != 1 {
		t.Fatalf("section identities or units are unstable: %#v", document.Sections)
	}
}

func TestSummaryBlocksRejectOverlappingSourceRanges(t *testing.T) {
	_, err := normalizeSummaryInput(SummaryInput{ContentID: "pdf:structured", Source: "pdf:document", Blocks: []SummaryBlock{
		{Kind: "heading", Text: "第一章", SegmentIndexes: []int{10}},
		{Kind: "paragraph", Text: "重复来源", SegmentIndexes: []int{10}},
	}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("overlapping block source indexes accepted: %v", err)
	}
	_, err = normalizeSummaryInput(SummaryInput{ContentID: "pdf:structured", Source: "pdf:document", Blocks: []SummaryBlock{
		{Kind: "heading", Text: "第一章", SegmentIndexes: []int{10}},
		{Kind: "paragraph", Text: "倒序来源", SegmentIndexes: []int{9}},
	}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("out-of-order block source indexes accepted: %v", err)
	}
}
