package bilibili

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"tangentry/internal/subtitles"
)

func TestReadSubtitlesFallsBackToSignedWBIAndKeepsPerPartFailures(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/caption.json" && r.Header.Get("Cookie") != "synthetic=redacted" {
			t.Error("B站 API request did not use configured fixture credentials")
		}
		if r.URL.Path == "/caption.json" && r.Header.Get("Cookie") != "" {
			t.Error("subtitle CDN request unexpectedly received credentials")
		}
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"bvid":"BV17x411w7KC","title":"课程标题","pages":[{"cid":111,"page":1,"part":"第一课"},{"cid":222,"page":2,"part":"第二课"}]}}`))
		case "/x/player/v2":
			_, _ = w.Write([]byte(`{"code":-101,"message":"请先登录"}`))
		case "/x/web-interface/nav":
			_, _ = w.Write([]byte(`{"code":0,"data":{"wbi_img":{"img_url":"https://fixture.invalid/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://fixture.invalid/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
		case "/x/player/wbi/v2":
			if r.URL.Query().Get("cid") == "222" {
				_, _ = w.Write([]byte(`{"code":-101,"message":"请先登录"}`))
				return
			}
			query := r.URL.Query()
			timestamp, parseErr := strconv.ParseInt(query.Get("wts"), 10, 64)
			expected := url.Values{"bvid": {"BV17x411w7KC"}, "cid": {"111"}}
			if parseErr == nil {
				sign(expected, mustFixtureMixinKey(t), timestamp)
			}
			if query.Get("cid") != "111" || parseErr != nil || query.Get("w_rid") != expected.Get("w_rid") {
				t.Errorf("missing or invalid WBI signature: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"subtitle":{"list":[{"lan":"zh-CN","lan_doc":"中文 CC","subtitle_url":"https://subtitle.hdslb.com/caption.json","type":0},{"lan":"zh-Hans","lan_doc":"中文 AI","subtitle_url":"https://aisubtitle.hdslb.com/caption.json","ai_type":1},{"lan":"en-US","lan_doc":"English","subtitle_url":"https://subtitle.hdslb.com/caption.json","type":0}]}}}`))
		case "/caption.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1,"content":"课程标题，欢迎学习这门课程。"}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	client := NewClient(subtitleFixtureClient(handler), "synthetic=redacted")
	client.baseURL = "https://api.fixture.invalid"

	doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Parts) != 2 || doc.Status != "cc" || doc.Parts[0].Status != "cc" || len(doc.Tracks) != 3 || doc.Tracks[0].Language != "zh-CN" || doc.Tracks[1].Language != "zh-Hans" || doc.Parts[1].Status != "error" || doc.Parts[1].ErrorCode != -101 || doc.Parts[1].ErrorMessage != "请先登录" {
		t.Fatalf("unexpected subtitle result: %+v", doc)
	}
	if strings.Contains(doc.Parts[1].ErrorMessage, "cookie") || doc.Tracks[0].Source != "cc" || doc.Tracks[1].Source != "ai" {
		t.Fatalf("missing safe source or error metadata: %+v", doc)
	}
}

func TestReadSubtitlesDistinguishesMissingURLFromNoCaptionsAndRetainsAIMismatch(t *testing.T) {
	for _, scenario := range []string{"url_empty", "none", "ai_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			wbiCalls := 0
			v2Calls := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/x/web-interface/view":
					_, _ = w.Write([]byte(`{"code":0,"data":{"bvid":"BV17x411w7KC","title":"与字幕完全无关的标题","cid":111,"pages":[]}}`))
				case "/x/player/v2":
					v2Calls++
					var body string
					switch scenario {
					case "url_empty":
						body = `{"code":0,"data":{"subtitle":{"list":[{"lan":"zh-CN","lan_doc":"中文","subtitle_url":""}]}}}`
					case "none":
						body = `{"code":0,"data":{"subtitle":{"list":[]}}}`
					case "ai_mismatch":
						body = `{"code":0,"data":{"subtitle":{"list":[{"lan":"zh-Hans","lan_doc":"中文 AI","subtitle_url":"https://aisubtitle.hdslb.com/caption.json","ai_type":1}]}}}`
					}
					_, _ = w.Write([]byte(body))
				case "/x/web-interface/nav":
					_, _ = w.Write([]byte(`{"code":0,"data":{"wbi_img":{"img_url":"https://fixture.invalid/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://fixture.invalid/4932caff0ff746eab6f01bf08b70ac45.png"}}}`))
				case "/x/player/wbi/v2":
					wbiCalls++
					if r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("wts") == "" {
						t.Errorf("missing WBI signature: %s", r.URL.RawQuery)
					}
					_, _ = w.Write([]byte(`{"code":0,"data":{"subtitle":{"list":[]}}}`))
				case "/caption.json":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1,"content":"完全不同的一段内容，重复多次也对不上。"},{"from":1,"to":2,"content":"另一句没有关联的内容。"},{"from":2,"to":3,"content":"第三句无关文本。"}]}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			client := NewClient(subtitleFixtureClient(handler), "")
			client.baseURL = "https://api.fixture.invalid"
			doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"url_empty": "api_empty", "none": "none", "ai_mismatch": "ai_mismatch"}[scenario]
			if doc.Parts[0].Status != want {
				t.Fatalf("got %s, want %s", doc.Parts[0].Status, want)
			}
			if scenario == "url_empty" && (len(doc.Parts[0].Languages) != 1 || doc.Parts[0].Languages[0].HasURL) {
				t.Fatal("empty URL candidate was lost during WBI fallback")
			}
			wantWBICalls, wantV2Calls := 1, 1
			if scenario == "ai_mismatch" {
				wantWBICalls, wantV2Calls = 0, 1
			}
			if wbiCalls != wantWBICalls || v2Calls != wantV2Calls {
				t.Fatalf("player API calls v2=%d WBI=%d, want v2=%d WBI=%d", v2Calls, wbiCalls, wantV2Calls, wantWBICalls)
			}
			if scenario == "ai_mismatch" && len(doc.Parts[0].Tracks) != 1 {
				t.Fatal("mismatched AI track was dropped")
			}
		})
	}
}

func TestReadSubtitlesRateLimitMarksRemainingPartsWithoutBypassingCooldown(t *testing.T) {
	var paths []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?cid="+r.URL.Query().Get("cid"))
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"title":"课程","pages":[{"cid":111,"page":1,"part":"第一课"},{"cid":222,"page":2,"part":"第二课"},{"cid":333,"page":3,"part":"第三课"}]}}`))
		case "/x/player/v2":
			if r.URL.Query().Get("cid") == "111" {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			t.Errorf("cooldown was bypassed with player request: %s", r.URL.RawQuery)
		case "/x/web-interface/nav", "/x/player/wbi/v2":
			t.Errorf("cooldown was bypassed with WBI request: %s", r.URL.Path)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	client := NewClient(subtitleFixtureClient(handler), "")
	client.baseURL = "https://api.fixture.invalid"
	doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Parts) != 3 || doc.Parts[0].Status != "rate_limited" || doc.Parts[1].Status != "rate_limited" || doc.Parts[2].Status != "rate_limited" {
		t.Fatalf("rate-limited part states not preserved: %+v", doc.Parts)
	}
	if len(paths) != 2 || paths[1] != "/x/player/v2?cid=111" {
		t.Fatalf("subsequent P requests bypassed cooling period: %v", paths)
	}
}

func TestReadSubtitlesOnlyDownloadsFirstPartBodyAndMarksMixedAIMismatch(t *testing.T) {
	bodyRequests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"title":"量子计算课程","pages":[{"cid":111,"page":1,"part":"第一课"},{"cid":222,"page":2,"part":"第二课"}]}}`))
		case "/x/player/v2":
			if r.URL.Query().Get("cid") == "111" {
				_, _ = w.Write([]byte(`{"code":0,"data":{"subtitle":{"list":[{"lan":"zh-CN","lan_doc":"中文 CC","subtitle_url":"https://subtitle.hdslb.com/cc.json"},{"lan":"zh-Hans","lan_doc":"中文 AI","subtitle_url":"https://aisubtitle.hdslb.com/ai.json","ai_type":1}]}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"subtitle":{"list":[{"lan":"zh-CN","lan_doc":"中文 CC","subtitle_url":"https://subtitle.hdslb.com/cc.json"},{"lan":"zh-Hans","lan_doc":"中文 AI","subtitle_url":"https://aisubtitle.hdslb.com/ai.json","ai_type":1}]}}}`))
		case "/cc.json":
			bodyRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1,"content":"量子计算课程欢迎学习"}]}`))
		case "/ai.json":
			bodyRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1,"content":"完全无关的话题文本内容"},{"from":1,"to":2,"content":"另外一些不相关句子信息"},{"from":2,"to":3,"content":"更多无关文本继续展开"}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	})
	client := NewClient(subtitleFixtureClient(handler), "")
	client.baseURL = "https://api.fixture.invalid"
	doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil {
		t.Fatal(err)
	}
	if bodyRequests != 2 || len(doc.Parts) != 2 || doc.Status != "cc" || doc.Parts[0].Status != "cc" || !doc.Parts[0].AIMismatch || !doc.Parts[0].Tracks[1].Mismatch {
		t.Fatalf("mixed status or body request policy was wrong: requests=%d document=%+v", bodyRequests, doc)
	}
	if len(doc.Parts[1].Tracks) != 0 || len(doc.Parts[1].Languages) != 2 || doc.Parts[1].Status != "cc" || doc.Parts[1].AIMismatch {
		t.Fatalf("non-current part bodies were fetched or status lost: %+v", doc.Parts[1])
	}
}

func TestSubtitleURLValidationAndSafeErrorText(t *testing.T) {
	for _, raw := range []string{"https://evil.invalid/a", "https://subtitle.hdslb.com:8443/a", "https://user:fixture@subtitle.hdslb.com/a"} {
		if _, err := validateSubtitleURL(raw); err == nil {
			t.Errorf("accepted unsafe subtitle URL %q", raw)
		}
	}
	for raw, want := range map[string]string{
		"//aisubtitle.hdslb.com/caption.json?fixture=1": "https://aisubtitle.hdslb.com/caption.json?fixture=1",
		"http://subtitle.hdslb.com/caption.json":        "https://subtitle.hdslb.com/caption.json",
	} {
		if got, err := validateSubtitleURL(raw); err != nil || got != want {
			t.Errorf("validateSubtitleURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if safe := safeSubtitleText("Cookie=secret bili_jct=synthetic-value https://subtitle.hdslb.com/private?token=x 请登录", 100); strings.Contains(safe, "secret") || strings.Contains(safe, "synthetic-value") || strings.Contains(safe, "https://") || !strings.Contains(safe, "已隐藏") {
		t.Fatalf("unsafe message was not redacted: %q", safe)
	}
}

func TestReadSubtitlesAcceptsProtocolRelativeAIChineseTrack(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"title":"课程","cid":111,"pages":[]}}`))
		case "/x/player/v2":
			_, _ = w.Write([]byte(`{"code":0,"data":{"subtitle":{"list":[{"lan":"ai-zh","lan_doc":"简体中文 AI","subtitle_url":"//aisubtitle.hdslb.com/caption.json?fixture=1"},{"lan":"en-US","lan_doc":"English","subtitle_url":"https://subtitle.hdslb.com/en.json"}]}}}`))
		case "/caption.json", "/en.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1,"content":"夹具字幕正文"}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	client := NewClient(subtitleFixtureClient(handler), "")
	client.baseURL = "https://api.fixture.invalid"
	doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || doc.Status != "ai" || len(doc.Tracks) != 2 {
		t.Fatalf("protocol-relative subtitle track was not read: %+v, %v", doc, err)
	}
	if doc.Tracks[0].Language != "ai-zh" || doc.Tracks[0].Source != "ai" || !doc.Tracks[0].Automatic || doc.Tracks[0].Label != "简体中文 AI" || doc.Tracks[0].Cues[0].Text != "夹具字幕正文" {
		t.Fatalf("AI Chinese identity or content lost: %+v", doc.Tracks[0])
	}
	if subtitlePriority("ai-zh", "ai") >= subtitlePriority("en-US", "cc") {
		t.Fatal("ai-zh was not prioritized as Chinese")
	}
}

func TestReadSubtitlesMaps412HTMLAndBiliRestrictionCodes(t *testing.T) {
	t.Run("412 html view", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte("<html>fixture block page</html>"))
		})
		client := NewClient(subtitleFixtureClient(handler), "")
		client.baseURL = "https://api.fixture.invalid"
		doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
		if err != nil || doc.Status != "rate_limited" || doc.ErrorCode != http.StatusPreconditionFailed || !strings.Contains(doc.ErrorMessage, "HTTP 412 text/html") || strings.Contains(doc.ErrorMessage, "fixture block page") {
			t.Fatalf("412 HTML response was not safely classified: %+v, %v", doc, err)
		}
	})
	t.Run("api -352", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/x/web-interface/view":
				_, _ = w.Write([]byte(`{"code":0,"data":{"title":"夹具","cid":111}}`))
			case "/x/player/v2":
				_, _ = w.Write([]byte(`{"code":-352,"message":"risk fixture"}`))
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
			}
		})
		client := NewClient(subtitleFixtureClient(handler), "")
		client.baseURL = "https://api.fixture.invalid"
		doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
		if err != nil || doc.Status != "rate_limited" || doc.ErrorCode != -352 || strings.Contains(doc.ErrorMessage, "risk fixture") {
			t.Fatalf("-352 response was not safely classified: %+v, %v", doc, err)
		}
	})
}

func TestSubtitleCDNRedirectIsNotFollowed(t *testing.T) {
	requests := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "https://evil.invalid/redirected-caption.json", http.StatusFound)
	})
	client := NewClient(subtitleFixtureClient(handler), "synthetic=redacted")
	cues, err := client.fetchSubtitleCues(context.Background(), "https://subtitle.hdslb.com/redirect.json")
	if err == nil || len(cues) != 0 || requests != 1 {
		t.Fatalf("CDN redirect was not rejected: cues=%d requests=%d err=%v", len(cues), requests, err)
	}
}

func TestReadSubtitlesCapsPrefetchAndLoadsSelectedTrackOnDemand(t *testing.T) {
	bodyReads := map[string]int{}
	var payload strings.Builder
	payload.WriteString(`{"code":0,"data":{"subtitle":{"list":[`)
	for i := 0; i < maxSubtitleTracks; i++ {
		if i > 0 {
			payload.WriteByte(',')
		}
		language, label := fmt.Sprintf("lang-%02d", i), fmt.Sprintf("Language %02d", i)
		switch i {
		case 0:
			language, label = "zh-Hans", "简体中文 CC"
		case 1:
			language, label = "ai-zh", "简体中文 AI"
		case 20:
			language, label = "ai-en", "英语 AI"
		case 21:
			language, label = "ai-ja", "日语 AI"
		}
		fmt.Fprintf(&payload, `{"lan":%q,"lan_doc":%q,"subtitle_url":"https://subtitle.hdslb.com/%02d.json"}`, language, label, i)
	}
	payload.WriteString(`]}}}`)
	validBody := `{"body":[{"from":0,"to":1,"content":"on demand"}]}`
	validBody += strings.Repeat(" ", (900<<10)-len(validBody))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write([]byte(`{"code":0,"data":{"title":"Fixture","cid":111,"pages":[]}}`))
		case "/x/player/v2":
			_, _ = w.Write([]byte(payload.String()))
		default:
			bodyReads[r.URL.Path]++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validBody))
		}
	})
	client := NewClient(subtitleFixtureClient(handler), "")
	client.baseURL = "https://api.fixture.invalid"
	doc, err := client.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || len(doc.Languages) != maxSubtitleTracks || len(doc.Tracks) != 18 || bodyReads["/00.json"] != 1 || bodyReads["/20.json"] != 0 {
		t.Fatalf("prefetch budget or metadata contract failed: tracks=%d langs=%d reads=%v err=%v", len(doc.Tracks), len(doc.Languages), bodyReads, err)
	}
	if doc.Tracks[0].Language != "zh-Hans" || doc.Tracks[1].Language != "ai-zh" {
		t.Fatalf("default language priority did not select Simplified CC then AI: %+v", doc.Tracks[:2])
	}
	if !hasSubtitleLanguage(doc.Languages, "ai-en") || !hasSubtitleLanguage(doc.Languages, "ai-ja") {
		t.Fatal("AI language metadata was omitted after the body budget")
	}
	selected, err := client.ReadSubtitlesLanguage(context.Background(), "BV17x411w7KC", "ai-en")
	if err != nil || len(selected.Tracks) != 1 || selected.Tracks[0].Language != "ai-en" || selected.Tracks[0].Cues[0].Text != "on demand" {
		t.Fatalf("selected track was not loaded: %+v %v", selected, err)
	}
	if _, err := client.ReadSubtitlesLanguage(context.Background(), "BV17x411w7KC", "../private"); err == nil {
		t.Fatal("unsafe language selector accepted")
	}
}

func hasSubtitleLanguage(values []subtitles.Language, language string) bool {
	for _, value := range values {
		if value.Language == language {
			return true
		}
	}
	return false
}

func TestSubtitlePriorityAndMismatchArePure(t *testing.T) {
	if subtitlePriority("zh-CN", "cc") >= subtitlePriority("zh-Hans", "ai") || subtitlePriority("zh-Hans", "ai") >= subtitlePriority("en", "cc") {
		t.Fatal("subtitle priority is not CC zh, AI zh, then other")
	}
	if subtitlePriority("zh-TW", "cc") <= subtitlePriority("zh-Hans", "ai") || subtitlePriority("zh", "cc") <= subtitlePriority("zh-Hans", "ai") || subtitlePriority("ai-zh", "ai") != subtitlePriority("zh-Hans", "ai") || subtitlePriority("ai-zh", "ai") >= subtitlePriority("en", "cc") {
		t.Fatal("Simplified Chinese must outrank unspecified or Traditional Chinese, and ai-zh must remain ahead of other languages")
	}
	for _, language := range []string{"ai-zh", "zh-CN", "zh-Hans", "zh-Hant"} {
		if !isChineseSubtitleLanguage(language) {
			t.Errorf("%q was not recognized as Chinese", language)
		}
	}
	for _, language := range []string{"ai-zh-Hant", "zh-TW", "zh-HK"} {
		if !isTraditionalChineseSubtitleLanguage(language) {
			t.Errorf("%q was not recognized as traditional Chinese", language)
		}
	}
	track := subtitlesTrackForMismatchFixture()
	if !aiSubtitleMismatch(track, "量子计算机的奇妙运行方式") || aiSubtitleMismatch(track, "课程讲解欢迎大家学习") {
		t.Fatal("AI title relevance heuristic did not discriminate")
	}
}

func mustFixtureMixinKey(t *testing.T) string {
	t.Helper()
	key, err := deriveKey("7cd084941338484aae1ad9425b84077c4932caff0ff746eab6f01bf08b70ac45")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func subtitlesTrackForMismatchFixture() subtitles.Track {
	return subtitles.Track{Automatic: true, Cues: []subtitles.Cue{{Text: "课程讲解欢迎大家学习相关内容"}, {Text: "这里继续介绍课程讲解的主要内容"}, {Text: "第三段学习内容给出复习建议"}}}
}

func subtitleFixtureClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: subtitleFixtureTransport{handler: handler}}
}

type subtitleFixtureTransport struct {
	handler http.Handler
}

func (t subtitleFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	if request.URL.Hostname() == "subtitle.hdslb.com" || request.URL.Hostname() == "aisubtitle.hdslb.com" {
		copy.URL = &url.URL{Path: request.URL.Path, RawQuery: request.URL.RawQuery}
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, copy)
	return recorder.Result(), nil
}
