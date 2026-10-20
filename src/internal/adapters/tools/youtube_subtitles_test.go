package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tangentry/internal/adapters/platforms/youtube"
	"tangentry/internal/config"
	"tangentry/internal/credentials"
	"tangentry/internal/subtitles"
)

type youtubeCaptionRoundTrip func(*http.Request) (*http.Response, error)

func (f youtubeCaptionRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func youtubeCaptionResponse(status int, body string, req *http.Request) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}
}

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

func TestYouTubeAutomaticOverlapMergeKeepsTimeAndNormalRepeats(t *testing.T) {
	cues := []subtitles.Cue{
		{Start: 4, End: 5, Text: "we are going home"},
		{Start: 2, End: 3, Text: "we are going"},
		{Start: 6, End: 7, Text: "we are really home"},
		{Start: 8, End: 9, Text: "really home today"},
		{Start: 10, End: 11, Text: "   "},
		{Start: 15, End: 16, Text: "we are going home"},
		{Start: 17, End: 18, Text: "你好世界，欢迎"},
		{Start: 18, End: 19, Text: "世界，欢迎大家"},
		{Start: 20, End: 21, Text: "thank you"},
		{Start: 21, End: 22, Text: "thank you very much"},
	}
	got := mergeYouTubeAutoCues(cues)
	if len(got) != 9 || got[0].Text != "we are going" || got[1].Text != "home" || got[1].Start != 4 || got[2].Text != "we are really home" || got[3].Text != "today" || got[3].Start != 8 || got[4].Text != "we are going home" || got[5].Text != "你好世界，欢迎" || got[6].Text != "大家" || got[6].Start != 18 || got[7].Text != "thank you" || got[8].Text != "thank you very much" {
		t.Fatalf("unexpected merged cues: %+v", got)
	}
}

func TestYouTubeInnerTubeCaptionsFallbackAndNoCaptionClassification(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Path)
		if req.URL.Path == "/sw.js_data" {
			return youtubeCaptionResponse(500, "bootstrap fixture", req), nil
		}
		if req.URL.Path == "/youtubei/v1/player" {
			return youtubeCaptionResponse(200, `{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw"},"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw","languageCode":"en","kind":"asr"},{"baseUrl":"https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw","languageCode":"zh-Hans","kind":"asr"}]}}}`, req), nil
		}
		if req.URL.Path == "/api/timedtext" {
			if req.URL.Query().Get("fmt") != "json3" {
				t.Fatal("timedtext format was not constrained")
			}
			return youtubeCaptionResponse(200, `{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"we are going home"}]},{"tStartMs":1000,"dDurationMs":1000,"segs":[{"utf8":"going home now"}]}]}`, req), nil
		}
		return nil, errors.New("unexpected local URL")
	})}
	kit := New(config.Config{})
	kit.SetYouTubeHTTPClient(client)
	doc, err := kit.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
	if err != nil || doc.Status != "available" || len(doc.Tracks) != 1 || !doc.Tracks[0].Automatic || doc.Tracks[0].Language != "zh-Hans" || len(doc.Tracks[0].Cues) != 2 || doc.Tracks[0].Cues[1].Text != "now" {
		t.Fatalf("InnerTube auto captions: %+v %v", doc, err)
	}
	if len(calls) != 3 || calls[0] != "/sw.js_data" || calls[1] != "/youtubei/v1/player" || calls[2] != "/api/timedtext" {
		t.Fatal("unexpected fallback order", calls)
	}

	kit.youtubePlayer = func(context.Context, string, string) youtube.PlayerResult {
		return youtube.PlayerResult{Tracks: []youtube.CaptionTrack{{Language: "en", URL: "https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"}}}
	}
	kit.status.Tools[0].Available = false
	kit.youtubeHTTP = &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
		return youtubeCaptionResponse(http.StatusOK, `{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"manual fallback"}]}]}`, req), nil
	})}
	doc, err = kit.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
	if err != nil || doc.Status != "available" || len(doc.Tracks) != 1 || doc.Tracks[0].Automatic || doc.Tracks[0].Cues[0].Text != "manual fallback" {
		t.Fatal("InnerTube manual captions were not usable without yt-dlp", doc, err)
	}
	kit.youtubePlayer = func(context.Context, string, string) youtube.PlayerResult {
		return youtube.PlayerResult{Tracks: []youtube.CaptionTrack{}}
	}
	doc, err = kit.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
	if err != nil || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatal("confirmed no captions was not returned as none", doc, err)
	}
	kit.youtubePlayer = func(context.Context, string, string) youtube.PlayerResult { return youtube.PlayerResult{Error: "auth"} }
	if _, err = kit.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", ""); !errors.Is(err, subtitles.ErrLogin) {
		t.Fatal("login failure was confused with no captions", err)
	}
}

func TestYouTubeInnerTubeManualPriorityAndExplicitLanguage(t *testing.T) {
	kit := New(config.Config{})
	kit.youtubePlayer = func(context.Context, string, string) youtube.PlayerResult {
		return youtube.PlayerResult{Tracks: []youtube.CaptionTrack{
			{Language: "zh-Hans", URL: "https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw", Kind: "asr"},
			{Language: "en", URL: "https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"},
			{Language: "zh-Hant", URL: "https://www.youtube.com/api/timedtext?lang=zh-Hant&v=jNQXAC9IVRw"},
		}}
	}
	if track, ok := choosePlayerCaption([]youtube.CaptionTrack{{Language: "zh-Hans", Kind: "asr"}, {Language: "en"}}, "", true); !ok || track.Language != "zh-Hans" {
		t.Fatal("ASR track language priority failed", track)
	}
	if track, ok := choosePlayerCaption([]youtube.CaptionTrack{{Language: "zh-Hans"}, {Language: "en"}}, "en", false); !ok || track.Language != "en" {
		t.Fatal("explicit language was not exact", track)
	}
}

func TestYouTubeTimedTextUsesScopedCookieAndRejectsUnsafeResponses(t *testing.T) {
	root := t.TempDir()
	jar, err := credentials.Open(filepath.Join(root, "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	const cookie = "SID=synthetic-timedtext-cookie"
	if err := jar.Save("youtube", "header", cookie); err != nil {
		t.Fatal(err)
	}
	track := youtube.CaptionTrack{Language: "en", URL: "https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw", Kind: "asr"}
	kit := New(config.Config{})
	kit.SetCredentials(jar)
	kit.youtubeUserAgent = func() string { return "Fixture Browser" }
	cookieHeader := jar.Header("youtube", track.URL)
	kit.youtubeHTTP = &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != cookie || req.Header.Get("Authorization") != "" || req.Header.Get("User-Agent") != "Fixture Browser" || req.URL.Query().Get("fmt") != "json3" {
			t.Fatal("timedtext request escaped its scoped headers or format")
		}
		return youtubeCaptionResponse(http.StatusOK, `{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"safe fixture"}]}]}`, req), nil
	})}
	doc, err := kit.readInnerTubeCaption(context.Background(), "jNQXAC9IVRw", track, cookieHeader, []youtube.CaptionTrack{track})
	if err != nil || len(doc.Tracks) != 1 || !doc.Tracks[0].Automatic || doc.Tracks[0].Cues[0].Text != "safe fixture" || strings.Contains(doc.Tracks[0].Cues[0].Text, cookie) {
		t.Fatalf("timedtext cookie handling: %+v %v", doc, err)
	}
	if err := jar.Clear("youtube"); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"login", http.StatusForbidden, "Sign in to confirm your age", subtitles.ErrLogin},
		{"restriction", http.StatusForbidden, "Confirm you're not a bot", subtitles.ErrRestricted},
		{"rate-limit", http.StatusTooManyRequests, "private fixture", subtitles.ErrRestricted},
		{"redirect", http.StatusFound, "redirect fixture", subtitles.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			kit.youtubeHTTP = &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
				response := youtubeCaptionResponse(test.status, test.body, req)
				if test.status == http.StatusFound {
					response.Header.Set("Location", "https://outside.example/caption")
				}
				return response, nil
			})}
			_, err := kit.readInnerTubeCaption(context.Background(), "jNQXAC9IVRw", track, cookieHeader, []youtube.CaptionTrack{track})
			if !errors.Is(err, test.want) || strings.Contains(err.Error(), cookie) || strings.Contains(err.Error(), test.body) {
				t.Fatalf("unsafe or misclassified timedtext error: %v", err)
			}
		})
	}

	kit.youtubeHTTP = &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
		return youtubeCaptionResponse(http.StatusOK, strings.Repeat("x", (2<<20)+1), req), nil
	})}
	if _, err := kit.readInnerTubeCaption(context.Background(), "jNQXAC9IVRw", track, cookieHeader, nil); !errors.Is(err, subtitles.ErrFormat) {
		t.Fatal("oversized timedtext response was accepted", err)
	}

	started := make(chan struct{})
	kit.youtubeHTTP = &http.Client{Transport: youtubeCaptionRoundTrip(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := kit.readInnerTubeCaption(ctx, "jNQXAC9IVRw", track, cookieHeader, nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("timedtext request ignored cancellation", err)
	}
}

func TestYouTubeFallbackReadsManualThenInnerTubeThenYTDLPAuto(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "yt-dlp-fixture.sh")
	logPath := filepath.Join(root, "calls")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'fixture\n'; exit 0; fi
write=0
directory=
next_paths=0
for argument in "$@"; do
  if [ "$argument" = "--write-subs" ] || [ "$argument" = "--write-auto-subs" ]; then write=1; fi
  if [ "$next_paths" = 1 ]; then directory=$argument; next_paths=0; fi
  if [ "$argument" = "--paths" ]; then next_paths=1; fi
done
if [ "$write" = 1 ]; then
  printf '%s\n' 'yt-write' >> 'CALL_LOG'
  printf '%s' '{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"automatic fallback"}]}]}' > "$directory/caption.zh-Hans.json3"
  printf '%s\n' '{"id":"jNQXAC9IVRw","requested_subtitles":{"zh-Hans":{"ext":"json3","url":"https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw"}}}'
else
  printf '%s\n' 'yt-metadata' >> 'CALL_LOG'
  printf '%s\n' '{"id":"jNQXAC9IVRw","subtitles":{},"automatic_captions":{"zh-Hans":[{"ext":"json3","url":"https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw"}]}}'
fi
`
	script = strings.ReplaceAll(script, "CALL_LOG", logPath)
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	kit := New(config.Config{YTDLPPath: exe})
	var stages []string
	kit.youtubePlayer = func(context.Context, string, string) youtube.PlayerResult {
		stages = append(stages, "player")
		return youtube.PlayerResult{Tracks: []youtube.CaptionTrack{}}
	}
	doc, err := kit.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "")
	if err != nil || doc.Status != "available" || len(doc.Tracks) != 1 || !doc.Tracks[0].Automatic || doc.Tracks[0].Language != "zh-Hans" || doc.Tracks[0].Cues[0].Text != "automatic fallback" {
		t.Fatalf("yt-dlp automatic fallback: %+v %v", doc, err)
	}
	if len(stages) != 1 || stages[0] != "player" {
		t.Fatalf("InnerTube did not run between yt-dlp stages: %v", stages)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil || strings.Split(strings.TrimSpace(string(raw)), "\n")[0] != "yt-metadata" || strings.Split(strings.TrimSpace(string(raw)), "\n")[1] != "yt-metadata" || strings.Split(strings.TrimSpace(string(raw)), "\n")[2] != "yt-write" {
		t.Fatalf("unexpected yt-dlp fallback invocations: %q %v", raw, err)
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
	captionDirsBefore, _ := filepath.Glob(filepath.Join(os.TempDir(), "tangentry-youtube-captions-*"))
	captionDirsExisting := make(map[string]bool, len(captionDirsBefore))
	for _, directory := range captionDirsBefore {
		captionDirsExisting[directory] = true
	}
	exe := filepath.Join(root, "fixture.sh")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'fixture\n'; exit 0; fi
printf '%s\n' "$*" >> 'ARG_LOG'
write=0
directory=
next_paths=0
for argument in "$@"; do
  if [ "$argument" = "--write-subs" ]; then write=1; fi
  if [ "$next_paths" = 1 ]; then directory=$argument; next_paths=0; fi
  if [ "$argument" = "--paths" ]; then next_paths=1; fi
done
if [ "$write" = 1 ]; then
  printf '%s' '{"events":[{"tStartMs":0,"dDurationMs":1000,"segs":[{"utf8":"English fixture"}]}]}' > "$directory/caption.en.json3"
  printf '%s\n' '{"id":"jNQXAC9IVRw","requested_subtitles":{"en":{"ext":"json3","url":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"}}}'
else
  printf '%s\n' '{"id":"jNQXAC9IVRw","subtitles":{"en":[{"ext":"json3","url":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"}]},"automatic_captions":{"zh-Hans":[{"ext":"json3","url":"https://www.youtube.com/api/timedtext?lang=zh-Hans&v=jNQXAC9IVRw"}]}}'
fi
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
	if err != nil || len(doc.Tracks) != 1 || doc.Tracks[0].Language != "en" || doc.Tracks[0].Automatic || doc.Status != "available" {
		t.Fatalf("tool caption retrieval: %+v %v", doc, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatal("fixture arguments")
	}
	for _, line := range lines {
		args := strings.Fields(line)
		for i, arg := range args {
			if (arg == "--cookies" || arg == "--paths") && i+1 < len(args) {
				if _, err := os.Stat(args[i+1]); !os.IsNotExist(err) {
					t.Fatal("private temporary artifact retained")
				}
			}
		}
	}
	captionDirsAfter, _ := filepath.Glob(filepath.Join(os.TempDir(), "tangentry-youtube-captions-*"))
	for _, directory := range captionDirsAfter {
		if !captionDirsExisting[directory] {
			t.Fatal("private caption directory retained", directory)
		}
	}
	for _, value := range []string{"--skip-download", "--simulate", "--no-simulate", "--sub-format", "json3/srt/vtt", "^en$", "youtube:skip=translated_subs"} {
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
			exe := filepath.Join(root, "fixture.sh")
			script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'fixture\n'; exit 0; fi
write=0
directory=
next_paths=0
for argument in "$@"; do
  if [ "$argument" = "--write-subs" ]; then write=1; fi
  if [ "$next_paths" = 1 ]; then directory=$argument; next_paths=0; fi
  if [ "$argument" = "--paths" ]; then next_paths=1; fi
done
if [ "$write" = 1 ]; then
			printf '%s' CAPTION > "$directory/caption.en.EXTENSION"
  printf '%s\n' '{"id":"jNQXAC9IVRw","requested_subtitles":{"en":{"ext":"EXTENSION","url":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"}}}'
else
  printf '%s\n' '{"id":"jNQXAC9IVRw","subtitles":{"en":[{"ext":"EXTENSION","url":"https://www.youtube.com/api/timedtext?lang=en&v=jNQXAC9IVRw"}]},"automatic_captions":{}}'
fi
`
			script = strings.ReplaceAll(script, "CAPTION", shellSingleQuote(tc.body))
			script = strings.ReplaceAll(script, "EXTENSION", tc.ext)
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

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
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
