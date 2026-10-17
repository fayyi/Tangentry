package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/config"
	"tangentry/internal/subtitles"
)

const testBVID = "BV17x411w7KC"
const sampleSRT = "\ufeff1\r\n00:00:01,250 --> 00:00:04,500\r\n<i>字幕 &amp; 文本</i>\r\n第二行\r\n\r\n2\r\n00:00:05,000 --> 00:00:07,000\r\n<script>literal text</script>\r\n"

func captionJSON(tracks map[string][]captionPayload) []byte {
	data, _ := json.Marshal(map[string]any{"id": testBVID + "_p1", "subtitles": tracks})
	return data
}

func TestSubtitleMappingExcludesDanmakuAndKeepsOnlyPlainContract(t *testing.T) {
	data := captionJSON(map[string][]captionPayload{"danmaku": {{Ext: "xml"}}, "ai-zh": {{Ext: "srt", Data: sampleSRT}}, "en": {{Ext: "srt", Data: sampleSRT}}})
	doc, err := parseSubtitles(data, "", testBVID)
	if err != nil || doc.Status != "available" || doc.Part != 1 || len(doc.Tracks) != 2 {
		t.Fatalf("unexpected captions: %+v %v", doc, err)
	}
	track := doc.Tracks[0]
	if track.Language != "ai-zh" || track.Label != "简体中文" || !track.Automatic || doc.Tracks[1].Automatic {
		t.Fatalf("bad track identity: %+v", doc.Tracks)
	}
	if !reflect.DeepEqual(track.Cues[0], subtitles.Cue{Start: 1.25, End: 4.5, Text: "字幕 & 文本\n第二行"}) || track.Cues[1].Text != "<script>literal text</script>" {
		t.Fatalf("bad text normalization: %+v", track.Cues)
	}
	encoded, _ := json.Marshal(doc)
	if strings.Contains(string(encoded), "danmaku") || strings.Contains(string(encoded), "http") {
		t.Fatal("extractor payload leaked")
	}
}

func TestMissingCaptionsDistinguishLoginAndFailures(t *testing.T) {
	data := captionJSON(map[string][]captionPayload{"danmaku": {{Ext: "xml"}}})
	for _, test := range []struct {
		warnings, status string
		err              error
	}{
		{"", "none", nil},
		{"WARNING: [BiliBili] Subtitles are only available when logged in. Cookie=secret", "login_required", nil},
		{"WARNING: Unable to download subtitle metadata secret-value", "", subtitles.ErrUnavailable},
		{"WARNING: Subtitles are only available when logged in\nWARNING: another failure", "", subtitles.ErrUnavailable},
	} {
		doc, err := parseSubtitles(data, test.warnings, testBVID)
		if !errors.Is(err, test.err) || (err == nil && (doc.Status != test.status || doc.Tracks == nil)) {
			t.Fatalf("%q: %+v %v", test.warnings, doc, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal("warning text exposed")
		}
	}
}

func TestSubtitleDefaultPrefersSimplifiedChinese(t *testing.T) {
	tracks := map[string][]captionPayload{}
	for _, lang := range []string{"ai-ar", "en", "zh-Hant", "ai-zh-Hans", "zh-CN"} {
		tracks[lang] = []captionPayload{{Ext: "srt", Data: sampleSRT}}
	}
	doc, err := parseSubtitles(captionJSON(tracks), "", testBVID)
	if err != nil || len(doc.Tracks) != 5 {
		t.Fatalf("%+v %v", doc, err)
	}
	for index, lang := range []string{"zh-CN", "ai-zh-Hans", "zh-Hant", "en", "ai-ar"} {
		if doc.Tracks[index].Language != lang {
			t.Fatalf("wrong preference: %+v", doc.Tracks)
		}
	}
}

func TestSubtitleParserRejectsInvalidAndPartialData(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"id":"BV17x411w7KC"}`),
		[]byte(`{"id":"other","subtitles":{}}`),
		[]byte(`{"id":"BV17x411w7KC_p2","subtitles":{}}`),
		[]byte(`{"id":"BV17x411w7KC","subtitles":{"zh":"bad"}}`),
		captionJSON(map[string][]captionPayload{"zh": {{Ext: "srt"}}}),
		captionJSON(map[string][]captionPayload{"zh": {{Ext: "vtt", Data: "not supported"}}}),
		captionJSON(map[string][]captionPayload{"../../path": {{Ext: "srt", Data: sampleSRT}}}),
	} {
		if _, err := parseSubtitles(data, "", testBVID); !errors.Is(err, subtitles.ErrFormat) {
			t.Fatalf("accepted malformed caption payload: %s %v", data, err)
		}
	}
	for _, raw := range []string{"", "1\n00:00:99,000 --> 00:01:02,000\ninvalid", "1\n00:00:05,000 --> 00:00:04,000\nreversed", "1\n00:00:01,000 --> 00:00:02,000\n", sampleSRT + "\nmalformed tail", strings.Repeat("x", (2<<20)+1)} {
		if _, err := parseSRT(raw); !errors.Is(err, subtitles.ErrFormat) {
			t.Fatal("malformed SRT accepted")
		}
	}
}

func TestSubtitleProcessIsSimulatedAndUsesOnlyYTDLP(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	root := t.TempDir()
	path := filepath.Join(root, "subtitle-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo fixture; exit 0; fi\n" +
		"printf '%s\\n' \"$@\" > '" + filepath.Join(root, "arguments") + "'\n" +
		"printf '%s\\n' '" + string(captionJSON(map[string][]captionPayload{"zh": {{Ext: "srt", Data: sampleSRT}}})) + "'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	tk := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(root, "missing-ffmpeg")})
	doc, err := tk.ReadSubtitles(context.Background(), testBVID)
	if err != nil || doc.Status != "available" || tk.Ready() {
		t.Fatalf("subtitle reader incorrectly requires FFmpeg: %+v %v", doc, err)
	}
	args, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--simulate", "--skip-download", "--ignore-config", "all,-danmaku", "https://www.bilibili.com/video/" + testBVID + "/"} {
		if !strings.Contains(string(args), expected) {
			t.Fatalf("missing safe option %s", expected)
		}
	}
	if strings.Contains(string(args), "--paths") || strings.Contains(string(args), "--cookies-from-browser") {
		t.Fatal("subtitle reader writes files or reads browser credentials")
	}
	if _, err := tk.ReadSubtitles(context.Background(), "https://localhost/private"); !errors.Is(err, subtitles.ErrInvalid) {
		t.Fatal("arbitrary URL accepted")
	}
}
