package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"tangentry/internal/subtitles"
	"testing"
	"time"
	"unicode/utf8"
)

type translatorFunc func(context.Context, Config, []Segment) ([]Segment, error)

func (f translatorFunc) Translate(ctx context.Context, c Config, segments []Segment) ([]Segment, error) {
	return f(ctx, c, segments)
}
func source(n int) Input {
	in := Input{ContentID: "youtube:jNQXAC9IVRw", Track: subtitles.Track{Language: "en", Label: "English"}}
	for i := 0; i < n; i++ {
		in.Track.Cues = append(in.Track.Cues, subtitles.Cue{Start: float64(i) + 0.125, End: float64(i) + 1.125, Text: fmt.Sprintf("Original segment %d", i)})
	}
	return in
}
func translations(segments []Segment) []Segment {
	out := make([]Segment, len(segments))
	for i, segment := range segments {
		out[len(out)-1-i] = Segment{Index: segment.Index, Text: fmt.Sprintf("這是第%d句譯文。", segment.Index)}
	}
	return out
}
func configured(t *testing.T, provider Translator) (*Service, Input, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ai")
	s, err := New(root, storeFor(root), provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	key := "synthetic-only-key"
	status, err := s.Configure(ConfigureInput{BaseURL: "https://ai-fixture.example/v1", Model: "fixture", Key: &key, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	in := source(40)
	in.Revision = status.Revision
	return s, in, root
}

func TestTranslationRecoversOnlyOnceWithSmallerMultiSegmentBatches(t *testing.T) {
	for _, failure := range []error{ErrIncomplete, ErrTruncated} {
		t.Run(failure.Error(), func(t *testing.T) {
			var sizes []int
			s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
				sizes = append(sizes, len(segments))
				if len(sizes) == 1 {
					return nil, failure
				}
				return translations(segments), nil
			}))
			before, err := s.Inspect(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.Translate(context.Background(), in)
			if err != nil || result.Key != before.Result.Key || len(result.Texts) != 40 || fmt.Sprint(sizes) != "[32 16 16 8]" {
				t.Fatal("recovery changed cache identity or fell back to individual requests", result, err, sizes)
			}
			if _, err := s.Translate(context.Background(), in); err != nil || len(sizes) != 4 {
				t.Fatal("completed recovery repeated model requests", err, sizes)
			}
		})
	}
	for _, failure := range []error{ErrFormat, ErrAuth, ErrRate, ErrRefused, ErrProvider, ErrGatewayTimeout, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			calls := 0
			s, in, _ := configured(t, translatorFunc(func(context.Context, Config, []Segment) ([]Segment, error) {
				calls++
				return nil, failure
			}))
			if _, err := s.Translate(context.Background(), in); !errors.Is(err, failure) || calls != 1 {
				t.Fatal("unrelated failure retried", err, calls)
			}
		})
	}
}

func TestTranslationTargetHasDistinctPersistentCacheIdentity(t *testing.T) {
	var targets []string
	s, input, root := configured(t, translatorFunc(func(_ context.Context, config Config, segments []Segment) ([]Segment, error) {
		targets = append(targets, config.TranslationTarget)
		return translations(segments), nil
	}))
	input.Track = source(2).Track
	input.TargetLanguage = "en"
	english, err := s.Inspect(context.Background(), input)
	if err != nil || english.Result.TargetLanguage != "en" || english.Result.Key == "" {
		t.Fatalf("English target identity was not returned: %+v %v", english, err)
	}
	if _, err := s.Translate(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.TargetLanguage = "ja"
	japanese, err := s.Inspect(context.Background(), input)
	if err != nil || japanese.Result.TargetLanguage != "ja" || japanese.Result.Key == english.Result.Key || len(japanese.Result.Texts) != 0 {
		t.Fatalf("Japanese reused a different target cache: %+v %v", japanese, err)
	}
	if _, err := s.Translate(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.TargetLanguage = "en"
	if cached, err := s.Inspect(context.Background(), input); err != nil || cached.Result.Key != english.Result.Key || len(cached.Result.Texts) != 2 {
		t.Fatalf("English cache was not retained: %+v %v", cached, err)
	}
	if fmt.Sprint(targets) != "[en ja]" {
		t.Fatalf("provider received wrong translation targets or cache was not reused: %v", targets)
	}
	reopened, err := New(root, storeFor(root), translatorFunc(func(context.Context, Config, []Segment) ([]Segment, error) {
		t.Fatal("persisted target cache invoked provider")
		return nil, ErrProvider
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	input.TargetLanguage = "ja"
	if cached, err := reopened.Inspect(context.Background(), input); err != nil || cached.Result.TargetLanguage != "ja" || len(cached.Result.Texts) != 2 {
		t.Fatalf("target language did not survive cache roundtrip: %+v %v", cached, err)
	}
}

func TestTranslationSmallerBatchFailureStopsAndPreservesCommittedHalf(t *testing.T) {
	calls := 0
	s, in, root := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		if calls == 1 || calls == 3 {
			return nil, ErrIncomplete
		}
		return translations(segments), nil
	}))
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrIncomplete) || calls != 3 {
		t.Fatal("recovery was repeated", err, calls)
	}
	partial, err := s.Inspect(context.Background(), in)
	if err != nil || len(partial.Result.Texts) != 16 {
		t.Fatal("successful half not committed", partial, err)
	}
	resumed := 0
	reopened, err := New(root, storeFor(root), translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		resumed++
		if segments[0].Index != 16 || len(segments) != 24 {
			t.Fatal("resume repeated saved translations", segments[0].Index, len(segments))
		}
		return translations(segments), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := reopened.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 40 || result.Key != partial.Result.Key || resumed != 1 {
		t.Fatal("persistent recovery failed", result, err, resumed)
	}
	for i, text := range result.Texts {
		if text != fmt.Sprintf("这是第%d句译文。", i) {
			t.Fatal("translation shifted", i)
		}
	}
}

func TestSmallTranslationFailureNeverSplitsIntoIndividualCalls(t *testing.T) {
	calls := 0
	s, in, _ := configured(t, translatorFunc(func(context.Context, Config, []Segment) ([]Segment, error) {
		calls++
		return nil, ErrIncomplete
	}))
	in.Track.Cues = in.Track.Cues[:4]
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrIncomplete) || calls != 1 {
		t.Fatal("small batch was split", err, calls)
	}
}

func TestTranslationOnlyShrinksFailedBatchAndKeepsOddBatchesGrouped(t *testing.T) {
	for _, tc := range []struct {
		count int
		sizes string
	}{{80, "[32 16 16 32 16]"}, {9, "[9 5 4]"}} {
		var sizes []int
		s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
			sizes = append(sizes, len(segments))
			if len(sizes) == 1 {
				return nil, ErrIncomplete
			}
			return translations(segments), nil
		}))
		in.Track = source(tc.count).Track
		result, err := s.Translate(context.Background(), in)
		if err != nil || len(result.Texts) != tc.count || fmt.Sprint(sizes) != tc.sizes {
			t.Fatal("unaffected batch shrunk or recovery became single-sentence", err, sizes)
		}
	}
}

func TestExplicitTranslationResumeCacheSimplifiedAndTimingIdentity(t *testing.T) {
	calls := 0
	s, in, root := configured(t, translatorFunc(func(_ context.Context, c Config, segments []Segment) ([]Segment, error) {
		calls++
		if calls == 2 {
			return nil, ErrRate
		}
		if c.Key != "synthetic-only-key" {
			t.Fatal("missing explicit credential")
		}
		return translations(segments), nil
	}))
	before, _ := json.Marshal(in)
	inspection, err := s.Inspect(context.Background(), in)
	if err != nil || len(inspection.Result.Texts) != 0 || calls != 0 {
		t.Fatal("read made inference")
	}
	if storeFor(root).Count() != 0 {
		t.Fatal("inspect wrote cache")
	}
	_, err = s.Translate(context.Background(), in)
	if !errors.Is(err, ErrRate) {
		t.Fatal(err)
	}
	inspection, err = s.Inspect(context.Background(), in)
	if err != nil || len(inspection.Result.Texts) != 32 {
		t.Fatalf("lost progress: %v %d", err, len(inspection.Result.Texts))
	}
	result, err := s.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 40 || calls != 3 {
		t.Fatal("resume repeated completed calls", err, calls)
	}
	if result.Texts[0] != "这是第0句译文。" || result.Texts[39] != "这是第39句译文。" {
		t.Fatal("wrong ordered simplified output")
	}
	result2, err := s.Translate(context.Background(), in)
	if err != nil || result2.Key != result.Key || calls != 3 {
		t.Fatal("cached result used paid inference")
	}
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("modified source cues")
	}
	changed := source(40)
	changed.Track.Cues[0].Text += "changed"
	if status, err := s.Inspect(context.Background(), changed); err != nil || status.Result.Key == result.Key || len(status.Result.Texts) != 0 {
		t.Fatal("stale translation reused", err)
	}
	// The same text is reused across timing, track label and content.
	for _, change := range []func(*Input){func(x *Input) { x.Track.Cues[0].Start += 0.1 }, func(x *Input) { x.Track.Language = "en-US" }, func(x *Input) { x.ContentID = "bilibili:BV17x411w7KC" }} {
		next := source(40)
		change(&next)
		status, err := s.Inspect(context.Background(), next)
		if err != nil || status.Result.Key != result.Key || len(status.Result.Texts) != 40 || status.Result.ContentID != next.ContentID || status.Result.Language != next.Track.Language {
			t.Fatal("identical source text not reused", err)
		}
	}
	// Cache and private settings survive a genuine new service instance.
	reopened, err := New(root, storeFor(root), translatorFunc(func(context.Context, Config, []Segment) ([]Segment, error) {
		t.Fatal("reopen inferred")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.Inspect(context.Background(), in)
	if err != nil || len(restored.Result.Texts) != 40 {
		t.Fatal("cache not persistent", err)
	}
	if info, err := os.Stat(filepath.Join(root, "settings.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private file permissions", err)
	}
}

func TestArticleSmallerBatchesResumeWithoutChangingCacheIdentity(t *testing.T) {
	calls := 0
	s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		if len(segments) > 8 {
			t.Fatal("article batch exceeds segment budget")
		}
		if calls == 2 {
			return nil, ErrGatewayTimeout
		}
		return translations(segments), nil
	}))
	in.Source = "article"
	in.Track.Language = "text-article"
	for i := range in.Track.Cues {
		in.Track.Cues[i].Text = strings.Repeat("text ", 50)
	}
	before, err := s.Inspect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Translate(context.Background(), in); !errors.Is(err, ErrGatewayTimeout) {
		t.Fatal(err)
	}
	partial, err := s.Inspect(context.Background(), in)
	if err != nil || len(partial.Result.Texts) != 8 || partial.Result.Key != before.Result.Key {
		t.Fatal("partial batch/cache identity lost", err)
	}
	result, err := s.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 40 || calls != 6 || result.Key != before.Result.Key {
		t.Fatal("article resume failed", err, calls)
	}
	if _, err = s.Translate(context.Background(), in); err != nil || calls != 6 {
		t.Fatal("cached article inferred again", err)
	}
}

func TestSubtitleByteBudgetBatchesWithoutSingleSentenceFallback(t *testing.T) {
	var sizes []int
	s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		sizes = append(sizes, len(segments))
		bytes := 0
		for _, segment := range segments {
			bytes += len(segment.Text)
		}
		if bytes > batchBytes {
			t.Fatalf("batch exceeds text budget: %d", bytes)
		}
		return translations(segments), nil
	}))
	in.Track.Cues = in.Track.Cues[:10]
	for i := range in.Track.Cues {
		in.Track.Cues[i].Text = strings.Repeat("long subtitle ", 100)
	}
	before, _ := json.Marshal(in.Track)
	result, err := s.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 10 || len(sizes) != 2 || sizes[0] != 5 || sizes[1] != 5 {
		t.Fatalf("expected two multi-segment requests: sizes=%v, err=%v", sizes, err)
	}
	for i, text := range result.Texts {
		if text != fmt.Sprintf("这是第%d句译文。", i) {
			t.Fatalf("translation lost original order at %d: %q", i, text)
		}
	}
	after, _ := json.Marshal(in.Track)
	if string(before) != string(after) {
		t.Fatal("batching modified source text or timings")
	}
	if _, err := s.Translate(context.Background(), in); err != nil || len(sizes) != 2 {
		t.Fatal("completed batches were requested again", err, sizes)
	}
}

func TestLongArticleParagraphUsesBoundedRequestsAndOriginalCacheIdentity(t *testing.T) {
	var received strings.Builder
	calls := 0
	s, in, root := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		if len(segments) != 1 || len(segments[0].Text) > articleBatchBytes || !utf8.ValidString(segments[0].Text) {
			t.Fatal("invalid long-paragraph request")
		}
		received.WriteString(segments[0].Text)
		return []Segment{{Index: segments[0].Index, Text: segments[0].Text}}, nil
	}))
	in.Source = "article"
	in.Track.Language = "text-article"
	in.Track.Cues = in.Track.Cues[:1]
	in.Track.Cues[0].Text = strings.Repeat("文字😀withoutbreak", 310) + " final sentence."
	before, err := s.Inspect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 1 || calls < 3 || received.String() != in.Track.Cues[0].Text || result.Key != before.Result.Key {
		t.Fatal("lost source or paragraph identity", err, calls)
	}
	previousCalls := calls
	reopened, err := New(root, storeFor(root), s.provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reused, err := reopened.Translate(context.Background(), in)
	if err != nil || calls != previousCalls || reused.Key != result.Key || reused.Texts[0] != result.Texts[0] {
		t.Fatal("reopened paragraph cache inferred again", err)
	}
}

func TestLongArticleParagraphFailureDoesNotCommitIncompleteText(t *testing.T) {
	calls := 0
	firstPart := ""
	s, in, root := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		if calls == 1 {
			firstPart = segments[0].Text
		} else if segments[0].Text == firstPart {
			t.Fatal("completed chunk inferred again")
		}
		if calls == 2 {
			return nil, ErrGatewayTimeout
		}
		return translations(segments), nil
	}))
	in.Source = "article"
	in.Track.Language = "text-article"
	in.Track.Cues = in.Track.Cues[:1]
	in.Track.Cues[0].Text = strings.Repeat("first paragraph. ", 130) + strings.Repeat("different continuation. ", 170)
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrGatewayTimeout) || calls != 2 {
		t.Fatal("failure retried or hidden", err, calls)
	}
	partial, err := s.Inspect(context.Background(), in)
	if err != nil || len(partial.Result.Texts) != 0 || partial.Active {
		t.Fatal("incomplete paragraph was committed", err)
	}
	reopened, err := New(root, storeFor(root), s.provider)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := reopened.Translate(context.Background(), in)
	if err != nil || len(result.Texts) != 1 {
		t.Fatal("explicit resume failed", err)
	}
	if storeFor(root).Count() < 3 {
		t.Fatal("chunk checkpoints missing")
	}
}

func TestLongParagraphRequestsHaveIndependentDeadlinesAndReleaseContexts(t *testing.T) {
	var contexts []context.Context
	s, in, _ := configured(t, translatorFunc(func(ctx context.Context, _ Config, segments []Segment) ([]Segment, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 90*time.Second || time.Until(deadline) < 85*time.Second {
			t.Fatal("request deadline missing or inherited from prior chunk")
		}
		for _, previous := range contexts {
			if previous == ctx || previous.Err() != context.Canceled {
				t.Fatal("prior request context reused or not released")
			}
		}
		contexts = append(contexts, ctx)
		return translations(segments), nil
	}))
	in.Source = "article"
	in.Track.Language = "text-article"
	in.Track.Cues = in.Track.Cues[:1]
	in.Track.Cues[0].Text = strings.Repeat("long sentence. ", 400)
	if _, err := s.Translate(context.Background(), in); err != nil || len(contexts) < 3 {
		t.Fatal("long paragraph failed", err)
	}
	for _, ctx := range contexts {
		if ctx.Err() != context.Canceled {
			t.Fatal("request context not released")
		}
	}
}

func TestCancellationBusyConfigurationAndClose(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int64
	s, in, _ := configured(t, translatorFunc(func(ctx context.Context, _ Config, segments []Segment) ([]Segment, error) {
		if calls.Add(1) == 1 {
			return translations(segments), nil
		}
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Translate(ctx, in); done <- err }()
	<-started
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.Configure(ConfigureInput{BaseURL: s.Status().BaseURL, Model: "new", Revision: s.Status().Revision}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	inspection, err := s.Inspect(context.Background(), in)
	if err != nil || !inspection.Active || !inspection.Settings.Translating || !s.Status().Translating || len(inspection.Result.Texts) != 32 {
		t.Fatal("bad live progress", err)
	}
	other := source(1)
	other.ContentID = "bilibili:BV17x411w7KC"
	otherInspection, err := s.Inspect(context.Background(), other)
	if err != nil || otherInspection.Active || !otherInspection.Settings.Translating {
		t.Fatal("global lock missing for another content", err)
	}
	summaryInspection, err := s.SummaryInspect(context.Background(), SummaryInput{ContentID: "rss:article-example", Source: "article:feed", Text: "A readable article paragraph."})
	if err != nil || summaryInspection.Active || !summaryInspection.Settings.Translating {
		t.Fatal("summary did not expose translation lock", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel stalled")
	}
	cached, err := s.Cached(context.Background(), inspection.Result.Key)
	if err != nil || len(cached.Texts) != 32 {
		t.Fatal("cancellation erased cache")
	}
	status := s.Status()
	if _, err := s.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: "new", Revision: status.Revision}); err != nil {
		t.Fatal("busy slot not released", err)
	}
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrConflict) {
		t.Fatal("outdated authorization called new provider", err)
	}
	s.Close()
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrStorage) {
		t.Fatal("closed service starts work")
	}
}
func TestProviderOutputsCannotDropDuplicateOrInventCueMappings(t *testing.T) {
	for _, bad := range []func([]Segment) []Segment{
		func(s []Segment) []Segment { return translations(s)[1:] },
		func(s []Segment) []Segment { r := translations(s); r[0].Index = r[1].Index; return r },
		func(s []Segment) []Segment { r := translations(s); r[0].Index = 100; return r },
		func(s []Segment) []Segment { r := translations(s); r[0].Text = " "; return r },
		func(s []Segment) []Segment { r := translations(s); r[0].Text = strings.Repeat("a", 24001); return r },
	} {
		s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) { return bad(segments), nil }))
		in.Track.Cues = in.Track.Cues[:3]
		if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrFormat) {
			t.Fatal(err)
		}
		result, err := s.Inspect(context.Background(), in)
		if err != nil || len(result.Result.Texts) != 0 {
			t.Fatal("bad output was committed")
		}
	}
}
func TestSourceValidationAndProviderFingerprint(t *testing.T) {
	var calls atomic.Int64
	s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls.Add(1)
		return translations(segments), nil
	}))
	for _, change := range []func(*Input){func(x *Input) { x.Track.Cues[0].Start = math.NaN() }, func(x *Input) { x.Track.Cues[0].End = 0 }, func(x *Input) { x.Track.Cues[1].Start = 0 }, func(x *Input) { x.Track.Cues[0].Text = "" }, func(x *Input) { x.ContentID = "pdf:" + strings.Repeat("a", 64) }, func(x *Input) { x.Track.Cues = nil }, func(x *Input) { x.Track.Language = "../key" }} {
		bad := source(40)
		bad.Revision = in.Revision
		change(&bad)
		if _, err := s.Translate(context.Background(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input called provider")
	}
	first, err := s.Translate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	next, err := s.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: "different", Revision: status.Revision})
	if err != nil {
		t.Fatal(err)
	}
	in.Revision = next.Revision
	inspection, err := s.Inspect(context.Background(), in)
	if err != nil || inspection.Result.Key != first.Key || len(inspection.Result.Texts) != 40 || inspection.Result.Model != status.Model {
		t.Fatal("model change discarded the stored translation", err)
	}
	reused, err := s.Translate(context.Background(), in)
	if err != nil || calls.Load() != 2 || reused.Model != status.Model {
		t.Fatal("model change paid for an existing translation", err, calls.Load())
	}
	in.Regenerate = true
	regenerated, err := s.Translate(context.Background(), in)
	if err != nil || regenerated.Key != first.Key || regenerated.Model != "different" || len(regenerated.Texts) != 40 || calls.Load() <= 2 {
		t.Fatal("explicit regeneration did not use the current model", err)
	}
}
func TestSettingsEndpointKeyIsolationAndRevision(t *testing.T) {
	s, _, root := configured(t, nil)
	status := s.Status()
	bytes, _ := json.Marshal(status)
	if strings.Contains(string(bytes), "synthetic-only-key") {
		t.Fatal("status leaked key")
	}
	for _, endpoint := range []string{"http://remote.example/v1", "https://user:password@host/v1", "https://host/v1?key=secret", "https://host/v1#fragment", "file:///tmp/private"} {
		empty := ""
		if _, err := s.Configure(ConfigureInput{BaseURL: endpoint, Model: "fixture", Key: &empty, Revision: status.Revision}); !errors.Is(err, ErrInvalid) {
			t.Fatal(endpoint, err)
		}
	}
	if _, err := s.Configure(ConfigureInput{BaseURL: "https://other.example/v1", Model: "fixture", Revision: status.Revision}); !errors.Is(err, ErrInvalid) {
		t.Fatal("retained secret across endpoints")
	}
	if _, err := s.Configure(ConfigureInput{BaseURL: status.BaseURL, Model: "fixture", Revision: 1}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	empty := ""
	local, err := s.Configure(ConfigureInput{BaseURL: "http://localhost:11434/v1/", Model: "local", Key: &empty, Revision: status.Revision})
	if err != nil || local.HasKey || !local.Ready {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	if strings.Contains(string(saved), "synthetic-only-key") {
		t.Fatal("clear retained old credential")
	}
	reopened, err := New(root, storeFor(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Status() != local {
		t.Fatal("settings not durable")
	}
}

func TestJSONInputRequiresActualBoundsAndKnownFields(t *testing.T) {
	bytes, _ := json.Marshal(source(1))
	valid := string(bytes)
	for _, bad := range []string{
		strings.Replace(valid, `"start":0.125,`, "", 1), strings.Replace(valid, `"start":0.125`, `"start":null`, 1), strings.Replace(valid, `"end":1.125`, `"end":null`, 1), strings.Replace(valid, `"language":"en"`, `"language":"en","extra":true`, 1), strings.Replace(valid, `"revision":0`, `"revision":null`, 1),
	} {
		var in Input
		if json.Unmarshal([]byte(bad), &in) == nil {
			t.Fatal("ambiguous input accepted")
		}
	}
	var in Input
	if json.Unmarshal(bytes, &in) != nil || !validate(in) {
		t.Fatal("complete original input rejected")
	}
	in.Source = "title"
	in.TargetLanguage = "en"
	in.Regenerate = true
	in.Track.Language = "text-title"
	in.Track.Cues = in.Track.Cues[:1]
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var title Input
	if err := json.Unmarshal(data, &title); err != nil {
		t.Fatal("title translation input rejected", err)
	}
	if title.Source != "title" || title.TargetLanguage != "en" || !title.Regenerate || title.Track.Language != "text-title" || !validate(title) {
		t.Fatalf("title translation fields were not preserved: %+v", title)
	}
}

func TestTextTranslationSharesCacheAndKeepsSourceIsolated(t *testing.T) {
	calls := 0
	s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		return translations(segments), nil
	}))
	in.ContentID = "rss:" + strings.Repeat("a", 64)
	in.Source = "article"
	in.Track.Language = "text-article"
	in.Track.Cues = in.Track.Cues[:2]
	data, _ := json.Marshal(in)
	var decoded Input
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Source != "article" {
		t.Fatal(err)
	}
	result, err := s.Translate(context.Background(), decoded)
	if err != nil || len(result.Texts) != 2 || result.Texts[0] != "这是第0句译文。" {
		t.Fatal(result, err)
	}
	if _, err := s.Translate(context.Background(), decoded); err != nil || calls != 1 {
		t.Fatal("cache called provider", err)
	}
	in.Source = "title"
	in.Track.Language = "text-title"
	in.Track.Cues = in.Track.Cues[:1]
	other, err := s.Translate(context.Background(), in)
	if err != nil || other.Key == result.Key || calls != 2 {
		t.Fatal("title reused article", err)
	}
	in.Source = ""
	if _, err := s.Translate(context.Background(), in); !errors.Is(err, ErrInvalid) {
		t.Fatal("synthetic track accepted as subtitle", err)
	}
}

var testStores sync.Map

// storeFor gives each test data directory one result store, so reopening a
// service on the same directory sees what the previous one persisted.
func storeFor(root string) *MemoryResults {
	store, _ := testStores.LoadOrStore(root, NewMemoryResults())
	return store.(*MemoryResults)
}
