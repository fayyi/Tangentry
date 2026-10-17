package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/subtitles"
)

func TestYouTubeCaptionFailureSeparatesAccessAndRateLimitsWithoutForwardingLogs(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want error
	}{
		{"ERROR: Sign in to confirm you're not a bot. Cookie=private", subtitles.ErrRestricted},
		{"ERROR: HTTP Error 429: Too Many Requests https://private.example", subtitles.ErrRestricted},
		{"ERROR: Sign in to confirm your age", subtitles.ErrLogin},
		{"WARNING: Your cookies are no longer valid", subtitles.ErrLogin},
		{"WARNING: Unable to download subtitles: HTTP Error 503", subtitles.ErrUnavailable},
		{"WARNING: unknown caption failure with private token", subtitles.ErrUnavailable},
	} {
		if got := youtubeCaptionFailure(test.raw); !errors.Is(got, test.want) || strings.Contains(got.Error(), "private") {
			t.Fatalf("unsafe or incorrect failure classification: %v", got)
		}
	}
}

func TestYouTubeManyNativeLanguagesRemainReadable(t *testing.T) {
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{}, Automatic: map[string][]youtubeCaption{}}
	for index := 0; index < 49; index++ {
		lang := "lang-" + strconv.Itoa(index)
		data.Subtitles[lang] = []youtubeCaption{{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=" + lang}}
	}
	data.Subtitles["zh-Hans"] = []youtubeCaption{{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=zh-Hans"}}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 50 || preferredYouTubeLanguage(choices) != "zh-Hans" {
		t.Fatal("a multilingual video should still expose its real tracks", len(choices), err)
	}
	for index := 49; index < 201; index++ {
		lang := "lang-" + strconv.Itoa(index)
		data.Subtitles[lang] = []youtubeCaption{{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=" + lang}}
	}
	raw, _ = json.Marshal(data)
	if _, err := youtubeLanguages(raw, data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("unbounded language metadata accepted", err)
	}
}

func TestYouTubeLanguagePreferenceAndTranslationExclusion(t *testing.T) {
	native := func(lang string) []youtubeCaption {
		return []youtubeCaption{{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=" + lang}}
	}
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{"en": native("en"), "zh-Hant": native("zh-Hant")}, Automatic: map[string][]youtubeCaption{"zh-Hans": native("zh-Hans"), "en": native("en"), "en-orig": native("en"), "ja": {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en&tlang=ja"}}}}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 3 || preferredYouTubeLanguage(choices) != "zh-Hans" {
		t.Fatalf("simplified Chinese priority failed: %+v %v", choices, err)
	}
	for _, choice := range choices {
		if choice.Language == "ja" || choice.Language == "en-orig" || choice.Language == "en" && choice.Automatic {
			t.Fatal("translation or duplicate exposed")
		}
	}
	if preferredYouTubeLanguage([]subtitles.Language{{Language: "ja"}, {Language: "en", Automatic: true}}) != "en" {
		t.Fatal("English fallback failed")
	}
	if preferredYouTubeLanguage([]subtitles.Language{{Language: "en", Automatic: true}, {Language: "zh-Hant"}}) != "zh-Hant" {
		t.Fatal("available Chinese should precede English when simplified Chinese is absent")
	}
	if preferredYouTubeLanguage([]subtitles.Language{{Language: "en"}, {Language: "zh", Automatic: true}}) != "zh" {
		t.Fatal("Chinese ASR should precede English")
	}
	data.Subtitles["zh-Hant"] = []youtubeCaption{{Ext: "json3", URL: "http://127.0.0.1/private"}}
	raw, _ = json.Marshal(data)
	if _, err := youtubeLanguages(raw, data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("invalid caption metadata reported as empty")
	}
	if _, err := youtubeLanguages([]byte(`{"id":"other","subtitles":{}}`), data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("caption identity mismatch")
	}
}

func TestYouTubeNativeCaptionsSurviveHLSAndManyTranslatedVariants(t *testing.T) {
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{"en": {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"}}}, Automatic: map[string][]youtubeCaption{
		"en": {{Ext: "vtt", URL: "https://manifest.googlevideo.com/api/manifest/hls_timedtext_playlist/fixture/index.m3u8"}},
	}}
	for i := 0; i < 150; i++ {
		data.Automatic["en"] = append(data.Automatic["en"], youtubeCaption{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=other&tlang=en"})
	}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 1 || choices[0].Language != "en" || choices[0].Automatic {
		t.Fatal("unsupported variants hid native manual captions", choices, err)
	}
	data.Automatic["en"][0].URL = "https://evil.example/api/manifest/hls_timedtext_playlist/fixture"
	raw, _ = json.Marshal(data)
	if _, err := youtubeLanguages(raw, data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("foreign manifest accepted", err)
	}
}

func TestYouTubeManualHLSOnlyLanguageDoesNotHideReadableCaptions(t *testing.T) {
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{
		"en":      {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"}},
		"zh-Hans": {{Ext: "vtt", URL: "https://manifest.googlevideo.com/api/manifest/hls_timedtext_playlist/fixture/index.m3u8"}},
	}, Automatic: map[string][]youtubeCaption{}}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 1 || preferredYouTubeLanguage(choices) != "en" {
		t.Fatalf("unsupported manual HLS track hid readable captions: %+v %v", choices, err)
	}
	delete(data.Subtitles, "en")
	raw, _ = json.Marshal(data)
	if choices, err := youtubeLanguages(raw, data.ID); err != nil || len(choices) != 0 {
		t.Fatalf("HLS-only metadata should expose no supported tracks: %+v %v", choices, err)
	}
	data.Subtitles["zh-Hans"][0].URL = "https://evil.example/api/manifest/hls_timedtext_playlist/fixture"
	raw, _ = json.Marshal(data)
	if _, err := youtubeLanguages(raw, data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("foreign HLS URL accepted", err)
	}
}

func TestYouTubePreferenceMatchesBilibiliLanguagePriority(t *testing.T) {
	for _, test := range []struct {
		choices []subtitles.Language
		want    string
	}{
		{nil, ""},
		{[]subtitles.Language{{Language: "en"}, {Language: "zh-Hant", Automatic: true}}, "zh-Hant"},
		{[]subtitles.Language{{Language: "zh-Hant"}, {Language: "zh-Hans", Automatic: true}}, "zh-Hans"},
		{[]subtitles.Language{{Language: "zh-Hans", Automatic: true}, {Language: "zh-Hans"}}, "zh-Hans"},
		{[]subtitles.Language{{Language: "ja"}, {Language: "en-orig", Automatic: true}}, "en-orig"},
		{[]subtitles.Language{{Language: "en"}, {Language: "zh_CN", Automatic: true}}, "zh_CN"},
	} {
		if got := preferredYouTubeLanguage(test.choices); got != test.want {
			t.Fatalf("choices %+v selected %q, want %q", test.choices, got, test.want)
		}
	}
}

func TestYouTubeJSON3PlainTextAndStrictTimes(t *testing.T) {
	raw := []byte(`{"events":[{"tStartMs":5000,"dDurationMs":1000,"segs":[{"utf8":"second"}]},{"tStartMs":1250,"dDurationMs":2750,"segs":[{"utf8":"Hello &amp; "},{"utf8":"<script>plain text</script>"}]},{"tStartMs":0,"segs":[{"utf8":"\n"}]}]}`)
	cues, err := parseJSON3(raw)
	if err != nil || len(cues) != 2 || cues[0].Start != 1.25 || cues[0].End != 4 || cues[0].Text != "Hello & <script>plain text</script>" {
		t.Fatalf("JSON3 mapping: %+v %v", cues, err)
	}
	for _, raw := range []string{`{}`, `{"events":[]}`, `{"events":[{"tStartMs":-1,"dDurationMs":100,"segs":[{"utf8":"bad"}]}]}`, `{"events":[{"tStartMs":0,"segs":[{"utf8":"bad"}]}]}`, `{"events":[{"tStartMs":0,"dDurationMs":0,"segs":[{"utf8":"bad"}]}]}`, `{"events":[{"tStartMs":604800000,"dDurationMs":1,"segs":[{"utf8":"bad"}]}]}`} {
		if _, err := parseJSON3([]byte(raw)); !errors.Is(err, subtitles.ErrFormat) {
			t.Fatal("malformed JSON3 accepted", raw)
		}
	}
}

func TestYouTubeOriginalAliasSurvivesTranslatedBase(t *testing.T) {
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{}, Automatic: map[string][]youtubeCaption{
		"en":      {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=ja&tlang=en"}},
		"en-orig": {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"}},
	}}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 1 || choices[0].Language != "en-orig" {
		t.Fatalf("lost native original alias: %+v %v", choices, err)
	}
	data.Automatic["en-orig"][0].URL = "https://127.0.0.1/private"
	raw, _ = json.Marshal(data)
	if _, err := youtubeLanguages(raw, data.ID); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("invalid automatic caption reported as no captions")
	}
}

func TestSelectedYouTubeCaptionMustBeNative(t *testing.T) {
	for _, raw := range []string{"", "https://www.youtube.com/api/timedtext?lang=en&tlang=zh", "https://evil.example/api/timedtext?lang=en", "https://www.youtube.com:443/api/timedtext", "https://www.youtube.com/api/timedtext?lang=en&tlang=%zz", "http://www.youtube.com/api/timedtext"} {
		if nativeYouTubeCaptionURL(raw) {
			t.Fatal("unverified selected subtitle accepted", raw)
		}
	}
	for _, raw := range []string{"https://www.youtube.com/api/timedtext?lang=en", "https://youtube.com/api/timedtext?lang=zh-Hans&fmt=json3"} {
		if !nativeYouTubeCaptionURL(raw) {
			t.Fatal("native subtitle rejected", raw)
		}
	}
}

func TestYouTubeMixedBasePrefersOriginalDownloadKey(t *testing.T) {
	data := youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{}, Automatic: map[string][]youtubeCaption{
		"en": {
			{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"},
			{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=ja&tlang=en"},
		},
		"en-orig": {{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"}},
	}}
	for i := 0; i < 20; i++ {
		raw, _ := json.Marshal(data)
		choices, err := youtubeLanguages(raw, data.ID)
		if err != nil || len(choices) != 1 || choices[0].Language != "en-orig" || preferredYouTubeLanguage(choices) != "en-orig" {
			t.Fatalf("mixed base hid unambiguous original: %+v %v", choices, err)
		}
	}
	data.Subtitles["en"] = []youtubeCaption{{Ext: "json3", URL: "https://www.youtube.com/api/timedtext?lang=en"}}
	raw, _ := json.Marshal(data)
	choices, err := youtubeLanguages(raw, data.ID)
	if err != nil || len(choices) != 1 || choices[0].Automatic || choices[0].Language != "en" {
		t.Fatal("manual captions should still win", choices, err)
	}
}

func TestCaptionMetadataBufferKeepsLargeDocumentsBounded(t *testing.T) {
	ordinary := &metadataBuffer{}
	captions := &metadataBuffer{limit: 16 << 20}
	chunk := make([]byte, 1<<20)
	for i := 0; i < 11; i++ {
		ordinary.Write(chunk)
		captions.Write(chunk)
	}
	if !ordinary.exceeded || ordinary.Len() != 4<<20 || captions.exceeded || captions.Len() != 11<<20 {
		t.Fatal("caption limit leaked into ordinary metadata or truncated valid metadata")
	}
	for i := 0; i < 7; i++ {
		if n, err := captions.Write(chunk); err != nil || n != len(chunk) {
			t.Fatal("bounded writer must drain process output", n, err)
		}
	}
	if !captions.exceeded || captions.Len() != 16<<20 {
		t.Fatal("caption metadata became unbounded", captions.Len())
	}
}

func TestYouTubeToolFetchesOneLanguageAndRemovesPrivateFiles(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	root := t.TempDir()
	exe := filepath.Join(root, "fixture.py")
	script := `#!/usr/bin/env python3
import sys,pathlib,json
if '--version' in sys.argv: print('fixture');raise SystemExit
with open('ARG_LOG','a') as log: log.write(json.dumps(sys.argv[1:])+'\n')
id='jNQXAC9IVRw'
track={'ext':'json3','url':'https://www.youtube.com/api/timedtext?lang=en'}
data={'id':id,'subtitles':{'en':[track],'zh-Hans':[track]},'automatic_captions':{}}
if '--write-subs' in sys.argv:
    assert '--no-simulate' in sys.argv and '--skip-download' in sys.argv
    root=pathlib.Path(sys.argv[sys.argv.index('--paths')+1])
    (root/'caption.zh-Hans.json3').write_text(json.dumps({'events':[{'tStartMs':0,'dDurationMs':1000,'segs':[{'utf8':'Chinese fixture'}]}]}))
    data['requested_subtitles']={'zh-Hans':{'ext':'json3','url':track['url']}}
print(json.dumps(data))
`
	script = strings.ReplaceAll(script, "ARG_LOG", filepath.Join(root, "arguments"))
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	jar, err := credentials.Open(filepath.Join(root, "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := jar.Save("youtube", "header", "SID=synthetic-caption-secret"); err != nil {
		t.Fatal(err)
	}
	tk := New(config.Config{YTDLPPath: exe})
	tk.SetCredentials(jar)
	doc, err := tk.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
	if err != nil || doc.Tracks[0].Language != "zh-Hans" || doc.Status != "available" {
		t.Fatalf("tool caption retrieval: %+v %v", doc, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var args []string
		if json.Unmarshal([]byte(line), &args) != nil {
			t.Fatal("fixture arguments")
		}
		for i, arg := range args {
			if arg == "--cookies" || arg == "--paths" {
				if _, err := os.Stat(args[i+1]); !os.IsNotExist(err) {
					t.Fatal("private temporary artifact retained")
				}
			}
		}
	}
	for _, value := range []string{"--skip-download", "--simulate", "--no-simulate", "--sub-format", "json3/srt/vtt", "^zh-Hans$", "youtube:skip=translated_subs"} {
		if !strings.Contains(string(raw), value) {
			t.Fatal("missing controlled option", value)
		}
	}
	if strings.Contains(string(raw), "synthetic-caption-secret") || strings.Contains(string(raw), "--cookies-from-browser") {
		t.Fatal("secret exposed or browser read")
	}
	if _, err := tk.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "../../arbitrary"); !errors.Is(err, subtitles.ErrInvalid) {
		t.Fatal("invalid language reached CLI")
	}
}

func TestYouTubeCaptionFormatFallbacks(t *testing.T) {
	for _, tc := range []struct {
		ext  string
		body string
	}{
		{"srt", "1\n00:00:01,250 --> 00:00:04,000\nHello &amp; world\n"},
		{"vtt", "WEBVTT\nKind: captions\nLanguage: en\n\nNOTE an ignored comment\n\ncue-id\n00:01.250 --> 00:04.000 align:start position:0%\n<v Speaker><c> Hello &amp; <00:02.000>world</c></v>\n"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, "fixture.py")
			script := `#!/usr/bin/env python3
import sys,pathlib,json
if '--version' in sys.argv: print('fixture');raise SystemExit
ext=EXTENSION
data={'id':'jNQXAC9IVRw','subtitles':{'en':[{'ext':ext,'url':'https://www.youtube.com/api/timedtext?lang=en'}]},'automatic_captions':{}}
if '--write-subs' in sys.argv:
    assert sys.argv[sys.argv.index('--sub-format')+1]=='json3/srt/vtt'
    assert '--skip-download' in sys.argv and '--no-simulate' in sys.argv
    root=pathlib.Path(sys.argv[sys.argv.index('--paths')+1])
    (root/('caption.en.'+ext)).write_text(CAPTION)
    data['requested_subtitles']={'en':{'ext':ext,'url':'https://www.youtube.com/api/timedtext?lang=en'}}
print(json.dumps(data))
`
			script = strings.ReplaceAll(script, "EXTENSION", strconv.Quote(tc.ext))
			script = strings.ReplaceAll(script, "CAPTION", strconv.Quote(tc.body))
			if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			tk := New(config.Config{YTDLPPath: exe})
			doc, err := tk.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
			if err != nil || len(doc.Tracks) != 1 || len(doc.Tracks[0].Cues) != 1 {
				t.Fatalf("format fallback: %+v %v", doc, err)
			}
			cue := doc.Tracks[0].Cues[0]
			if cue.Start != 1.25 || cue.End != 4 || cue.Text != "Hello & world" {
				t.Fatalf("caption mapping: %+v", cue)
			}
		})
	}
	for _, ext := range []string{"json3", "srt", "vtt"} {
		raw, _ := json.Marshal(youtubeCaptionMetadata{ID: "jNQXAC9IVRw", Subtitles: map[string][]youtubeCaption{"en": {{Ext: ext, URL: "https://127.0.0.1/private"}}}})
		if _, err := youtubeLanguages(raw, "jNQXAC9IVRw"); !errors.Is(err, subtitles.ErrFormat) {
			t.Fatal("untrusted caption URL accepted", ext, err)
		}
	}
}

func TestWebVTTRejectsMalformedAndUnboundedCaptions(t *testing.T) {
	for _, body := range []string{
		"", "not WEBVTT", "WEBVTT\n\nNOTE empty captions\n",
		"WEBVTT\n\n00:61.000 --> 01:02.000\nbad",
		"WEBVTT\n\n-00:01.000 --> 00:02.000\nbad",
		"WEBVTT\n\n00:01.000 --> 00:01.000\nbad",
		"WEBVTT\n\n168:00:00.000 --> 168:00:01.000\nbad",
		"WEBVTT\n\n00:01.000 --> 00:02.000\n" + strings.Repeat("x", 8001),
		"WEBVTT\n\n00:01.000 --> 00:02.000\n" + strings.Repeat("x", 2<<20),
		"WEBVTT\n\n" + strings.Repeat("00:01.000 --> 00:02.000\ntext\n\n", 10001),
	} {
		if _, err := parseWebVTT(body); !errors.Is(err, subtitles.ErrFormat) {
			t.Fatal("malformed VTT accepted", err)
		}
	}
	body := "WEBVTT\n\nSTYLE\n::cue { color: lime; }\n\nREGION\nid:foo\n\n00:05.000 --> 00:06.000\nsecond\n\n00:01:01.250 --> 00:01:02.500\n<b>last</b>\n\n00:00.000 --> 00:01.000\nfirst"
	cues, err := parseWebVTT(body)
	if err != nil || len(cues) != 3 || cues[0].Text != "first" || cues[2].Start != 61.25 || cues[2].Text != "last" {
		t.Fatalf("VTT sorting and hours: %+v %v", cues, err)
	}
}
