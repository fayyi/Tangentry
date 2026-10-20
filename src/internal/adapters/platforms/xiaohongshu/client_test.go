package xiaohongshu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"testing"
)

const noteID = "6411cf99000000001300b6d9"

func fixturePage(kind string) string {
	note := map[string]any{"noteId": noteID, "type": kind, "title": "undefined 不是空值", "desc": "第一段。\n第二段 with undefined。", "time": 1791504000123, "user": map[string]string{"nickname": "夹具作者", "userId": "author"}, "imageList": []any{map[string]any{"urlDefault": "http://sns-webpic-qc.xhscdn.com/first", "width": 900, "height": 1200}, map[string]any{"infoList": []any{map[string]string{"imageScene": "WB_DFT", "url": "https://sns-webpic-qc.xhscdn.com/second"}}, "width": 1200, "height": 800}}}
	payload, _ := json.Marshal(map[string]any{"note": map[string]any{"noteDetailMap": map[string]any{noteID: map[string]any{"note": note}}}, "unused": nil})
	return "<html><script>window.__INITIAL_STATE__=" + strings.Replace(string(payload), `"unused":null`, `"unused":undefined`, 1) + ";</script></html>"
}
func TestNoteAlbumPreservesOrderBodyAndExactIdentity(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture&tracking=discard")
	item, err := Parse([]byte(fixturePage("normal")), ref)
	if err != nil || item.ID != "xiaohongshu:"+noteID || item.Kind != "image" || len(item.Images) != 2 || item.Images[0].URL != "https://sns-webpic-qc.xhscdn.com/first" || item.Images[1].Width != 1200 || item.Title != "undefined 不是空值" || item.Description != "第一段。\n第二段 with undefined。" || item.PublishedAt != 1791504000 {
		t.Fatal(item, err)
	}
	if strings.Contains(item.URL, "discard") || !strings.Contains(item.URL, "xsec_token=fixture") {
		t.Fatal(item.URL)
	}
	video, err := Parse([]byte(fixturePage("video")), ref)
	if err != nil || video.Kind != "video" || len(video.Images) != 0 {
		t.Fatal(video, err)
	}
	c := &Client{}
	c.remember(video)
	if c.VideoLocator(noteID) != ref.URL {
		t.Fatal("successful video locator lost")
	}
	for i := 0; i < 130; i++ {
		c.remember(content.Item{Kind: "video", PlatformID: fmt.Sprint(i), URL: fmt.Sprint(i)})
	}
	if len(c.locators) != 128 || c.VideoLocator(noteID) == ref.URL {
		t.Fatal("unbounded session locators")
	}
}

func TestVideoUsesCombinedH264AtMost1080AndRejectsForeignMedia(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	streams := `"video":{"media":{"stream":{"h264":[
		{"masterUrl":"https://sns-video-bd.xhscdn.com/720.mp4","height":720,"duration":12301},
		{"masterUrl":"https://sns-video-bd.xhscdn.com/1080.mp4","height":1080,"duration":12301},
		{"masterUrl":"https://sns-video-bd.xhscdn.com/2160.mp4","height":2160},
		{"masterUrl":"https://evil.example/private.mp4","height":1080}
	],"h265":[{"masterUrl":"https://sns-video-bd.xhscdn.com/hevc.mp4","height":1080}]}}},`
	page := strings.Replace(fixturePage("video"), `"noteId":`, streams+`"noteId":`, 1)
	item, err := Parse([]byte(page), ref)
	if err != nil || item.MediaURL != "https://sns-video-bd.xhscdn.com/1080.mp4" || item.DurationSeconds != 13 {
		t.Fatal("video stream selection failed", err)
	}
	page = strings.Replace(page, "sns-video-bd.xhscdn.com", "evil.example", -1)
	item, err = Parse([]byte(page), ref)
	if err != nil || item.MediaURL != "" {
		t.Fatal("foreign media accepted", err)
	}
}

func TestVideoRecognizesCurrentEFStreamsAndPortraitResolution(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	streams := `"video":{"media":{"stream":{
		"EF4":[{"masterUrl":"https://sns-video-bd.xhscdn.com/portrait.mp4","videoCodec":"EF4","format":"mp4","width":720,"height":1280,"duration":15001}],
		"EF5":[{"masterUrl":"https://sns-video-bd.xhscdn.com/fragmented.mp4","format":"fmp4","width":1080,"height":1920}]
	}}},`
	page := strings.Replace(fixturePage("video"), `"noteId":`, streams+`"noteId":`, 1)
	item, err := Parse([]byte(page), ref)
	if err != nil || item.MediaURL != "https://sns-video-bd.xhscdn.com/portrait.mp4" || item.DurationSeconds != 16 {
		t.Fatal("portrait combined stream not recognized", err)
	}
}

func TestVideoMediaV2PreservesBodyWithoutAUsableCover(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	media := `{"video":{"drm_type":0},"stream":{"EF4":[
		{"master_url":"https://sns-video-bd.xhscdn.com/v2-720.mp4","video_codec":"h264","audio_codec":"aac","format":"mp4","width":720,"height":1280,"duration":15001},
		{"master_url":"https://sns-video-bd.xhscdn.com/v2-1080.mp4","video_codec":"h264","audio_codec":"aac","format":"mp4","width":1080,"height":1920,"duration":15001},
		{"master_url":"https://sns-video-bd.xhscdn.com/silent.mp4","video_codec":"h264","format":"mp4","width":1080,"height":1920},
		{"master_url":"https://sns-video-bd.xhscdn.com/fragmented.mp4","audio_codec":"aac","format":"fmp4","height":720}
	]}}`
	encoded, _ := json.Marshal(media)
	page := strings.Replace(fixturePage("video"), `"noteId":`, `"video":{"mediaV2":`+string(encoded)+`},"noteId":`, 1)
	page = strings.ReplaceAll(page, "sns-webpic-qc.xhscdn.com", "invalid-cover.example")
	item, err := Parse([]byte(page), ref)
	if err != nil || item.MediaURL != "https://sns-video-bd.xhscdn.com/v2-1080.mp4" || item.DurationSeconds != 16 || item.Description != "第一段。\n第二段 with undefined。" || item.ThumbnailURL != "" || len(item.Images) != 0 {
		t.Fatal("new video shape lost body or selected unusable media", item, err)
	}
	if _, err := Parse([]byte(strings.Replace(page, `"type":"video"`, `"type":"normal"`, 1)), ref); err == nil {
		t.Fatal("incomplete album accepted as a complete note")
	}
}

func TestVideoMediaV2UsesVerifiedBackupsAndNeverRevivesProtectedLegacyMedia(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	legacy := `"media":{"stream":{"h264":[{"masterUrl":"https://sns-video-bd.xhscdn.com/legacy.mp4","height":720}]}}`
	for _, tc := range []struct {
		name, media, want string
	}{
		{"backup", `{"stream":{"EF4":[{"master_url":"https://foreign.example/video.mp4","backup_urls":["https://foreign.example/backup.mp4","https://sns-video-bd.xhscdn.com/backup.mp4"],"format":"mp4","audio_codec":"aac","height":720}]}}`, "https://sns-video-bd.xhscdn.com/backup.mp4"},
		{"camel case", `{"stream":{"EF4":[{"masterUrl":"https://sns-video-bd.xhscdn.com/camel.mp4","format":"mp4","audioCodec":"aac","height":720}]}}`, "https://sns-video-bd.xhscdn.com/camel.mp4"},
		{"DRM number", `{"video":{"drm_type":1},"stream":{"EF4":[{"master_url":"https://sns-video-bd.xhscdn.com/protected.mp4","format":"mp4","audio_codec":"aac"}]}}`, ""},
		{"DRM boolean", `{"video":{"drmType":true},"stream":{}}`, ""},
		{"DRM text", `{"video":{"drm_type":"widevine"},"stream":{}}`, ""},
		{"unknown DRM", `{"video":{"drm_type":{}},"stream":{}}`, ""},
		{"malformed", `{"stream":`, ""},
		{"not object", `[]`, ""},
		{"oversize", `{"padding":"` + strings.Repeat("a", 500000) + `"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, _ := json.Marshal(tc.media)
			page := strings.Replace(fixturePage("video"), `"noteId":`, `"video":{`+legacy+`,"mediaV2":`+string(encoded)+`},"noteId":`, 1)
			item, err := Parse([]byte(page), ref)
			if err != nil || item.MediaURL != tc.want || item.Description == "" {
				t.Fatal("media boundary or body retention failed", item, err)
			}
		})
	}
}

func TestVideoCoverIsOptionalButAlbumStillRequiresEveryImage(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	page := strings.Replace(fixturePage("video"), "http://sns-webpic-qc.xhscdn.com/first", "https://invalid-cover.example/first", 1)
	item, err := Parse([]byte(page), ref)
	if err != nil || item.ThumbnailURL != "https://sns-webpic-qc.xhscdn.com/second" || item.Description == "" {
		t.Fatal("valid optional cover not retained", item, err)
	}
	if _, err := Parse([]byte(strings.Replace(page, `"type":"video"`, `"type":"normal"`, 1)), ref); err == nil {
		t.Fatal("bad album silently truncated")
	}
}
func TestInitializerNeverEvaluatesCodeOrImportsOtherNotes(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID)
	page := fixturePage("normal")
	for _, raw := range []string{"<html>verification required</html>", strings.Replace(page, noteID, "6411cf99000000001300b6d8", -1), strings.Replace(page, "\"type\":\"normal\"", "\"type\":\"live\"", 1), strings.Replace(page, "http://sns-webpic-qc.xhscdn.com/first", "https://127.0.0.1/private", 1), strings.Replace(page, "1791504000123", "9223372036854775807", 1), strings.Replace(page, ";</script>", ";alert('must not run');</script>", 1), strings.Replace(page, "undefined", "(()=>{return null})()", -1), page + page} {
		if _, err := Parse([]byte(raw), ref); err == nil {
			t.Fatal("bad page accepted", raw[:min(len(raw), 90)])
		}
	}
	for _, raw := range []string{`{"v":undefinedThing}`, `{"v":undefined};run()`, `{"v":NaN}`, `{"v":"unterminated}`} {
		if _, err := stateJSON(raw); err == nil {
			t.Fatal("not plain initializer", raw)
		}
	}
}

func TestInitializerAcceptsOnlyEmptyCollectionDataLiterals(t *testing.T) {
	raw := `{"map":new Map([]),"set":new Set(),"otherMap":new Map(),"otherSet":new Set([]),"text":"new Map() and undefined"}`
	normalized, err := stateJSON(raw)
	if err != nil || string(normalized) != `{"map":{},"set":[],"otherMap":{},"otherSet":[],"text":"new Map() and undefined"}` {
		t.Fatal(string(normalized), err)
	}
	for _, script := range []string{`{"map":new Map(run())}`, `{"set":new Set([run()])}`, `{"map":new Map()};run()`, `{"map":new Map([["key",secret()]])}`} {
		if _, err := stateJSON(script); err == nil {
			t.Fatal("executable initializer accepted")
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body string, header http.Header) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header, Request: r}
}
func TestShortShareIsAnonymousAndOnlyOwnPageReceivesCookie(t *testing.T) {
	calls := 0
	cookieCalls := 0
	c := &Client{Cookie: func(raw string) string {
		cookieCalls++
		if !strings.Contains(raw, "xsec_token=fixture") {
			t.Fatal("lost access locator")
		}
		return "session=own-fixture"
	}}
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Hostname() == "xhslink.com" {
			if r.Header.Get("Cookie") != "" {
				t.Fatal("cookie on share")
			}
			return response(r, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture"}}), nil
		}
		if calls == 2 && r.Header.Get("Cookie") != "session=own-fixture" {
			t.Fatal("missing own cookie")
		}
		return response(r, 200, fixturePage("normal"), nil), nil
	})}
	item, err := c.Note(context.Background(), "https://xhslink.com/a/Fixture")
	if err != nil || len(item.Images) != 2 || calls != 2 || cookieCalls != 1 {
		t.Fatal(item, err, calls, cookieCalls)
	}
}
func TestCookieDoesNotFollowRedirectAndFailuresAreSafe(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/private", "https://evil.example/?private=secret", "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d8", "https://www.xiaohongshu.com/404"} {
		calls := 0
		c := &Client{Cookie: func(string) string { return "session=private-secret" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(r, 302, "", http.Header{"Location": {target}}), nil
			}
			if r.Header.Get("Cookie") != "" {
				t.Fatal("redirect received Cookie")
			}
			return response(r, 200, fixturePage("normal"), nil), nil
		})}}
		_, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fixture-access")
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}}
	if _, err := c.Note(ctx, "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fixture-access"); err != context.Canceled {
		t.Fatal("cancellation not preserved", err)
	}
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, strings.Repeat("x", (4<<20)+1), nil), nil
	})
	if _, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fixture-access"); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestNoteRateLimitsAreClassifiedAtShareAndDetailBoundaries(t *testing.T) {
	for _, raw := range []string{"https://xhslink.com/a/Fixture", "https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture-access"} {
		for _, status := range []int{429, 401, 403, 404, 461, 471, 500} {
			calls := 0
			c := &Client{Cookie: func(string) string { return "session=private-fixture" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return response(r, status, "private platform response", nil), nil
			})}}
			_, err := c.Note(context.Background(), raw)
			var platform *discovery.PlatformError
			wantCode := "xiaohongshu_page_unavailable"
			if status == 429 || status == 461 || status == 471 {
				wantCode = "xiaohongshu_restricted"
			} else if status == 404 {
				wantCode = "xiaohongshu_note_unavailable"
			} else if status == 401 || status == 403 {
				wantCode = "xiaohongshu_access_denied"
			}
			if !errors.As(err, &platform) || platform.Code != wantCode || platform.Restricted != (status == 429 || status == 461 || status == 471) || calls != 1 {
				t.Fatal("wrong status or retry", status, calls, err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "session=") {
				t.Fatal("platform response or credential leaked")
			}
		}
	}
}

func TestDirectNoteWithoutShareTokenExplainsHowToRecover(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	_, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID)
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || platform.Code != "xiaohongshu_share_token_missing" || !strings.Contains(err.Error(), "完整分享链接") || calls != 0 {
		t.Fatalf("missing token was not classified: calls=%d err=%v", calls, err)
	}
}

func TestShortShareWithoutExpandedTokenExplainsHowToRecover(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(r, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/" + noteID}}), nil
	})}}
	_, err := c.Note(context.Background(), "https://xhslink.com/a/Fixture")
	var platform *discovery.PlatformError
	if !errors.As(err, &platform) || platform.Code != "xiaohongshu_share_token_missing" || !strings.Contains(err.Error(), "完整分享链接") || calls != 1 {
		t.Fatalf("short share without token was not classified: calls=%d err=%v", calls, err)
	}
}

func TestStructureDiagnosticsRedactAccessParametersAndCookies(t *testing.T) {
	secret := "fixture-token-secret"
	logged := ""
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, `<html>拦截页面 xsec_token=`+secret+` cookie=session=private-secret</html>`, http.Header{"Content-Type": {"text/html; charset=utf-8"}}), nil
	})}}
	_, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token="+secret)
	if err != nil {
		logged = err.Error()
	}
	if err == nil || !strings.Contains(err.Error(), "__INITIAL_STATE__") || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private-secret") || strings.Contains(logged, secret) || strings.Contains(logged, "private-secret") || len(err.Error()) > 600 {
		t.Fatalf("unsafe structure diagnostic: %v", err)
	}
}

func TestStructureDiagnosticsClipSafePageText(t *testing.T) {
	body := "<html><head><script>window.token='script-secret'</script></head><body>" + strings.Repeat("页面片段 ", 100) + "</body></html>"
	finalURL, _ := url.Parse("https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture-secret&xsec_source=share")
	message := pageStructureError([]byte(body), http.StatusOK, "text/html; charset=utf-8", finalURL).Error()
	if len([]rune(message)) > 400 || strings.Contains(message, "script-secret") || strings.Contains(message, "fixture-secret") || strings.Contains(message, "xsec_source=share") || !strings.Contains(message, "?[参数已省略]") || !strings.Contains(message, "页面片段") {
		t.Fatalf("page snippet was not clipped safely: length=%d message=%s", len([]rune(message)), message)
	}
}

func TestVideoWithoutSafeMediaKeepsNoteBodyAndMetadata(t *testing.T) {
	ref, _ := discovery.ParseXiaohongshu("https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture")
	item, err := Parse([]byte(fixturePage("video")), ref)
	if err != nil || item.Kind != "video" || item.MediaURL != "" || item.Description == "" || item.Title == "" || item.PlatformID != noteID {
		t.Fatalf("note without safe video media was not retained: %#v %v", item, err)
	}
}

func TestNoteUserAgentFallbackOnlyAppliesToFinalAnonymousPage(t *testing.T) {
	var agents []string
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		agents = append(agents, r.Header.Get("User-Agent"))
		if r.URL.Hostname() == "xhslink.com" {
			return response(r, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fresh"}}), nil
		}
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.Note(context.Background(), "https://xhslink.com/a/Fixture"); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0] == anonymousNoteUserAgent || agents[1] != anonymousNoteUserAgent {
		t.Fatalf("unexpected anonymous user agents: %#v", agents)
	}

	agents = nil
	c = &Client{Cookie: func(string) string { return "session=fixture" }, UserAgent: func(string) string { return "Browser/fixture" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		agents = append(agents, r.Header.Get("User-Agent"))
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fresh"); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0] != "Browser/fixture" {
		t.Fatalf("browser user agent was overwritten: %#v", agents)
	}
}

func TestNoteUserAgentIsPreservedWithCookieAndOnShareExpansion(t *testing.T) {
	var requests []string
	c := &Client{Cookie: func(string) string { return "session=fixture" }, UserAgent: func(string) string { return "Browser/fixture" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Hostname()+"|"+r.Header.Get("User-Agent")+"|"+r.Header.Get("Cookie"))
		if r.URL.Hostname() == "xhslink.com" {
			return response(r, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fresh"}}), nil
		}
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.Note(context.Background(), "https://xhslink.com/a/Fixture"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0] != "xhslink.com|Browser/fixture|" || requests[1] != "www.xiaohongshu.com|Browser/fixture|session=fixture" {
		t.Fatalf("share or cookie request headers changed: %#v", requests)
	}
}

func TestNoteWithCookieAndNoBrowserUserAgentKeepsDesktopDefault(t *testing.T) {
	var gotAgent, gotCookie string
	c := &Client{Cookie: func(string) string { return "session=fixture" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		gotAgent, gotCookie = r.Header.Get("User-Agent"), r.Header.Get("Cookie")
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fresh"); err != nil {
		t.Fatal(err)
	}
	if gotAgent != "Mozilla/5.0" || gotCookie != "session=fixture" {
		t.Fatalf("cookie request was treated as anonymous: agent=%q cookie=%q", gotAgent, gotCookie)
	}
}

func TestNoteWithoutCookiePreservesSavedBrowserUserAgent(t *testing.T) {
	var gotAgent, gotCookie string
	c := &Client{UserAgent: func(string) string { return "Browser/fixture" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		gotAgent, gotCookie = r.Header.Get("User-Agent"), r.Header.Get("Cookie")
		return response(r, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.Note(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=fresh"); err != nil {
		t.Fatal(err)
	}
	if gotAgent != "Browser/fixture" || gotCookie != "" {
		t.Fatalf("browser user agent was not preserved: agent=%q cookie=%q", gotAgent, gotCookie)
	}
}

func TestLoginAndUnavailableRedirectsExplainMissingDetailsWithoutFollowing(t *testing.T) {
	for _, target := range []string{"/login?redirect=private-locator", "/website-login", "/404?error_msg=private-response"} {
		for _, raw := range []string{"https://xhslink.com/a/Fixture", "https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fixture-access"} {
			calls := 0
			c := &Client{Cookie: func(string) string { return "session=private-credential" }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return response(r, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com" + target}}), nil
			})}}
			_, err := c.Note(context.Background(), raw)
			var platform *discovery.PlatformError
			if err == nil || !errors.As(err, &platform) || platform.Code != "xiaohongshu_note_unavailable" || calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal("followed unavailable page or leaked redirect", calls, err)
			}
			message := "分享访问参数可能已过期"
			if strings.HasPrefix(target, "/404") {
				message = "重新复制分享链接"
			}
			if !strings.Contains(err.Error(), message) {
				t.Fatal("missing next action", err)
			}
		}
	}
}

func TestReplacingShareLocatorRequiresTheSameNoteAndKeepsNewAccessParameters(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Hostname() == "xhslink.com" {
			return response(req, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/6411cf99000000001300b6d8?xsec_token=other"}}), nil
		}
		return response(req, 200, fixturePage("normal"), nil), nil
	})}}
	if _, err := c.NoteFor(context.Background(), noteID, "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d8"); err == nil || calls != 0 {
		t.Fatal("wrong direct note fetched")
	}
	if _, err := c.NoteFor(context.Background(), noteID, "https://xhslink.com/a/OtherNote"); err == nil || !strings.Contains(err.Error(), "不一致") || calls != 1 {
		t.Fatal("wrong short link accepted", err, calls)
	}
	item, err := c.NoteFor(context.Background(), noteID, "复制 https://www.xiaohongshu.com/explore/"+noteID+"?xsec_token=new-locator&xsec_source=pc_share 打开小红书")
	if err != nil || !strings.Contains(item.URL, "xsec_token=new-locator") || len(item.Images) != 2 || item.Description == "" {
		t.Fatal("fresh share locator was not used", err)
	}
}

func TestShortShareDoesNotOpenLoginGatedNoteBeforeUsingItsCookie(t *testing.T) {
	calls := 0
	c := &Client{Cookie: func(string) string { return "session=own-note" }, HTTP: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Hostname() == "xhslink.com" {
			if req.Header.Get("Cookie") != "" {
				t.Fatal("short link received credentials")
			}
			return response(req, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/explore/" + noteID + "?xsec_token=fresh"}}), nil
		}
		if req.Header.Get("Cookie") == "" {
			return response(req, 302, "", http.Header{"Location": {"https://www.xiaohongshu.com/login"}}), nil
		}
		return response(req, 200, fixturePage("normal"), nil), nil
	})}}
	item, err := c.Note(context.Background(), "https://xhslink.com/a/RestrictedNote")
	if err != nil || item.Description == "" || len(item.Images) != 2 || calls != 2 {
		t.Fatal("authenticated short-share note did not load", err, calls)
	}
}
