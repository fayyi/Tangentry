package ai

import (
	"context"
	"strings"
	"tangentry/internal/subtitles"
	"testing"
)

func TestChineseSubtitlesNeverCallProviderOrDuplicateCache(t *testing.T) {
	calls := 0
	s, in, root := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		return translations(segments), nil
	}))
	in.Track.Cues = in.Track.Cues[:1]
	for _, lang := range []string{"zh-Hans", "zh-CN"} {
		in.Track.Language = lang
		in.Track.Cues[0].Text = "这是原来的中文字幕，API 和 AI 名称保持原样。"
		inspection, err := s.Inspect(context.Background(), in)
		if err != nil || len(inspection.Result.Texts) != 1 || inspection.Result.Texts[0] != in.Track.Cues[0].Text {
			t.Fatal(lang, inspection, err)
		}
		result, err := s.Translate(context.Background(), in)
		if err != nil || len(result.Texts) != 1 || result.Texts[0] != in.Track.Cues[0].Text || calls != 0 {
			t.Fatal(lang, result, err, calls)
		}
	}
	if storeFor(root).Count() != 0 {
		t.Fatal("Chinese source should not store duplicate translations")
	}
	in.TargetLanguage = "zh-Hans"
	for _, lang := range []string{"zh-Hant", "zh", "ai-zh", "cmn", "zho", "chi"} {
		in.Track.Language = lang
		in.Track.Cues[0].Text += " " + lang
		if _, err := s.Translate(context.Background(), in); err != nil || calls == 0 {
			t.Fatal("Traditional or ambiguous Chinese source should be translated for a Simplified target", lang, err, calls)
		}
	}
	in.Track.Language = "zh-Hant"
	in.TargetLanguage = "zh-Hant"
	if result, err := s.Translate(context.Background(), in); err != nil || len(result.Texts) != 1 || result.Texts[0] != in.Track.Cues[0].Text {
		t.Fatal("Traditional Chinese source should be reused for a Traditional target", result, err)
	}
	in.Track.Language = "en"
	in.TargetLanguage = "zh-Hans"
	in.Track.Cues[0].Text = "这是英语内容中的中文引用部分。"
	if _, err := s.Translate(context.Background(), in); err != nil || calls == 0 {
		t.Fatal("explicit English language must not be classified by Chinese text alone", err, calls)
	}
	for _, text := range []string{"An English sentence about 中文.", "これは日本語の字幕です。"} {
		in.Track.Cues[0].Text = text
		if chineseSubtitles(in) {
			t.Fatal("foreign subtitles misclassified", text)
		}
	}
	in.Source = "article"
	in.Track.Cues[0].Text = "这是文章内容。"
	if chineseSubtitles(in) {
		t.Fatal("subtitle rule leaked into article translation")
	}
	in.Source = "title"
	in.Track.Language = "text-title"
	if !chineseSubtitles(in) {
		t.Fatal("Chinese title should not be sent for translation")
	}
}

func TestUnknownSubtitleLanguageSamplesBeyondEnglishIntro(t *testing.T) {
	calls := 0
	s, in, _ := configured(t, translatorFunc(func(_ context.Context, _ Config, segments []Segment) ([]Segment, error) {
		calls++
		return translations(segments), nil
	}))
	in.Track.Language = "und"
	in.Track.Cues = make([]subtitles.Cue, 200)
	for i := range in.Track.Cues {
		text := "这是一段中文视频正文，介绍真实的内容与阅读方式。"
		if i < 20 {
			text = "Welcome to the video."
		}
		in.Track.Cues[i] = subtitles.Cue{Start: float64(i), End: float64(i + 1), Text: text}
	}
	if !chineseSubtitles(in) {
		t.Fatal("English intro hid the Chinese transcript")
	}
	result, err := s.Translate(context.Background(), in)
	if err != nil || calls != 0 || len(result.Texts) != len(in.Track.Cues) || result.Texts[0] != in.Track.Cues[0].Text {
		t.Fatal("Chinese transcript was translated or modified", err, calls)
	}
	for i := range in.Track.Cues {
		if i < 20 {
			in.Track.Cues[i].Text = strings.Repeat("中文介绍。", 4)
		} else {
			in.Track.Cues[i].Text = "The rest of this transcript is in English and needs translation."
		}
	}
	if chineseSubtitles(in) {
		t.Fatal("Chinese intro hid the English transcript")
	}
}
