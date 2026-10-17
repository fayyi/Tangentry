package youtube

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixtureCookie = "SAPISID=synthetic-private-secret"

var fixtureNow = time.Unix(1_800_000_000, 0)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// browseServer answers InnerTube browse requests with a handler; the service
// worker bootstrap fails, so sessions are generated locally as in YouTube.js.
type browseServer struct {
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
	handler  func(index int, body map[string]any) (int, string)
}

func (s *browseServer) home() *Home {
	return &Home{Now: func() time.Time { return fixtureNow }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/sw.js_data" {
			return nil, errors.New("offline bootstrap fallback")
		}
		if r.URL.Host != "www.youtube.com" || r.URL.Path != "/youtubei/v1/browse" {
			return nil, errors.New("unexpected endpoint " + r.URL.String())
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		s.mu.Lock()
		index := len(s.requests)
		s.requests = append(s.requests, body)
		s.headers = append(s.headers, r.Header.Clone())
		s.mu.Unlock()
		status, response := s.handler(index, body)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}, Request: r}, nil
	})}}
}

func object(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err, raw)
	}
	return value
}

func richGrid(items ...string) string {
	return `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":false}},"contents":{"richGridRenderer":{"contents":[` + strings.Join(items, ",") + `]}}}`
}

func appended(items ...string) string {
	return `{"onResponseReceivedActions":[{"appendContinuationItemsAction":{"continuationItems":[` + strings.Join(items, ",") + `]}}]}`
}

func rich(content string) string { return `{"richItemRenderer":{"content":` + content + `}}` }

func lockup(id, title string) string {
	return `{"lockupViewModel":{"contentId":"` + id + `","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"` + title + `"}}}}}`
}

func classic(id, title string) string {
	return `{"videoRenderer":{"videoId":"` + id + `","title":{"simpleText":"` + title + `"}}}`
}

func continuationItem(token string) string {
	return `{"continuationItemRenderer":{"trigger":"CONTINUATION_TRIGGER_ON_ITEM_SHOWN","continuationEndpoint":{"commandMetadata":{"webCommandMetadata":{"apiUrl":"/youtubei/v1/browse"}},"continuationCommand":{"token":"` + token + `","request":"CONTINUATION_REQUEST_TYPE_BROWSE"}}}}`
}

func numberedID(index int) string { return fmt.Sprintf("%011d", index) }

func titles(entries []HomeEntry) []string {
	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Title)
	}
	return result
}

func signature(scheme, sid string, timestamp int64) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d %s https://www.youtube.com", timestamp, sid)))
	return fmt.Sprintf("%s %d_%s", scheme, timestamp, hex.EncodeToString(sum[:]))
}

func TestSecureSIDFallbackSignsOnlyYouTubeAPIRequests(t *testing.T) {
	target, _ := url.Parse("https://www.youtube.com/youtubei/v1/browse")
	cookie := "__Secure-3PAPISID=fixture=private; SID=another"
	headers := http.Header{}
	signRequest(headers, target, cookie, time.Unix(100, 0))
	digest := signature("SAPISIDHASH", "fixture=private", 100)
	if headers.Get("Authorization") != digest+" "+strings.Replace(digest, "SAPISIDHASH", "SAPISID3PHASH", 1) {
		t.Fatal(headers.Get("Authorization"))
	}
	if headers.Get("X-Goog-Authuser") != "0" || headers.Get("Cookie") != cookie || headers.Get("X-Origin") != "https://www.youtube.com" {
		t.Fatal(headers)
	}
	for _, raw := range []string{"https://youtubei.googleapis.com/youtubei/v1/browse", "https://www.youtube.com/sw.js_data", "https://example.com/youtubei/v1/browse", "http://www.youtube.com/youtubei/v1/browse", "https://www.youtube.com:8443/youtubei/v1/browse", "https://fixture-user@www.youtube.com/youtubei/v1/browse"} {
		other, _ := url.Parse(raw)
		safe := http.Header{}
		signRequest(safe, other, cookie, time.Unix(100, 0))
		if len(safe) != 0 {
			t.Fatal("credentials attached outside the InnerTube API", raw, safe)
		}
	}
	existing := http.Header{"Authorization": {"existing-library-auth"}}
	signRequest(existing, target, cookie, time.Unix(100, 0))
	if existing.Get("Authorization") != "existing-library-auth" {
		t.Fatal("existing authorization replaced")
	}
	plain := http.Header{}
	signRequest(plain, target, cookie+"; SAPISID=plain", time.Unix(100, 0))
	if !strings.HasPrefix(plain.Get("Authorization"), signature("SAPISIDHASH", "plain", 100)) {
		t.Fatal(plain.Get("Authorization"))
	}
	selected := http.Header{"X-Goog-Authuser": {"2"}}
	signRequest(selected, target, "__Secure-3PAPISID=fixture", time.Unix(100, 0))
	if selected.Get("X-Goog-Authuser") != "2" || !strings.HasPrefix(selected.Get("Authorization"), "SAPISIDHASH 100_") {
		t.Fatal("account selection lost", selected)
	}
	for _, cookie := range []string{"", "SID=fixture; LOGIN_INFO=fixture", "__Secure-3PAPISID="} {
		headers := http.Header{}
		signRequest(headers, target, cookie, time.Unix(100, 0))
		if headers.Get("Authorization") != "" || headers.Get("X-Goog-Authuser") != "" {
			t.Fatal("unrelated cookies signed", cookie, headers)
		}
	}
}

func TestModernCookieSignaturesSupplementLibraryAuthAndPreserveOAuth(t *testing.T) {
	target, _ := url.Parse("https://www.youtube.com/youtubei/v1/browse")
	cookie := "SAPISID=plain; __Secure-1PAPISID=first; __Secure-3PAPISID=third"
	headers := http.Header{"Authorization": {"SAPISIDHASH 99_abcdef"}, "X-Goog-Authuser": {"2"}}
	signRequest(headers, target, cookie, time.Unix(100, 0))
	expected := strings.Join([]string{signature("SAPISIDHASH", "plain", 100), signature("SAPISID1PHASH", "first", 100), signature("SAPISID3PHASH", "third", 100)}, " ")
	if headers.Get("Authorization") != expected || headers.Get("X-Goog-Authuser") != "2" {
		t.Fatal(headers)
	}
	oauth := http.Header{"Authorization": {"Bearer fixture"}}
	signRequest(oauth, target, cookie, time.Unix(100, 0))
	if oauth.Get("Authorization") != "Bearer fixture" || oauth.Get("X-Origin") != "" {
		t.Fatal(oauth)
	}
}

func TestAnonymousResponsesRejectSavedCookiesOnly(t *testing.T) {
	anonymous := object(t, `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":true}}}`)
	if !rejectedCookieSession(anonymous, "SID=fixture") || rejectedCookieSession(anonymous, "") {
		t.Fatal("logged-out diagnosis is wrong")
	}
	if rejectedCookieSession(object(t, `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":false}}}`), "SID=fixture") || rejectedCookieSession(map[string]any{}, "SID=fixture") {
		t.Fatal("signed-in responses rejected")
	}
}

func TestLoggedOutHomeReportsAuthWithoutLeakingCookie(t *testing.T) {
	server := &browseServer{handler: func(int, map[string]any) (int, string) {
		return 200, `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":true}}}`
	}}
	result := server.home().Fetch(context.Background(), fixtureCookie, nil)
	if result.Error != "auth" || len(server.requests) != 1 {
		t.Fatal("rejected cookie session continued", result, len(server.requests))
	}
	if strings.Contains(fmt.Sprint(result), "synthetic-private-secret") {
		t.Fatal("cookie leaked into the result")
	}
}

func TestModernHomeResponseParsesInOneAuthenticatedRequest(t *testing.T) {
	short := rich(`{"shortsLockupViewModel":{"entityId":"not-a-watch-id","thumbnail":{"sources":[{"url":"https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg","width":480,"height":360}]},"onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"abcdefghijk"}}},"overlayMetadata":{"primaryText":{"content":"Parsed short"},"secondaryText":{"content":"12K views"}}}}`)
	video := rich(`{"lockupViewModel":{"contentId":"jNQXAC9IVRw","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Parsed full video"}}},"contentImage":{"thumbnailViewModel":{"image":{"sources":[{"url":"https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg"}]},"overlays":[{"thumbnailBottomOverlayViewModel":{"badges":[{"thumbnailBadgeViewModel":{"text":"12:34"}}]}}]}}}}`)
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if index != 0 || body["browseId"] != "FEwhat_to_watch" {
			t.Error("unnecessary continuation or fallback", body)
		}
		return 200, richGrid(short, video)
	}}
	result := server.home().Fetch(context.Background(), fixtureCookie, nil)
	if result.Mode != "account" || len(result.Entries) != 1 || result.Entries[0].ID != "jNQXAC9IVRw" || result.Entries[0].Title != "Parsed full video" || result.Entries[0].Duration != 754 {
		t.Fatal(result)
	}
	headers := server.headers[0]
	if headers.Get("Cookie") != fixtureCookie || !strings.HasPrefix(headers.Get("Authorization"), "SAPISIDHASH ") {
		t.Fatal("missing authentication", headers)
	}
	client, _ := path(server.requests[0], "context", "client").(map[string]any)
	if client["clientName"] != "WEB" || client["visitorData"] == "" || headers.Get("X-Goog-Visitor-Id") != client["visitorData"] {
		t.Fatal("session context missing", client)
	}
}

func TestHomeContinuationSendsPlatformTokenWithAuthentication(t *testing.T) {
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		switch index {
		case 0:
			if body["browseId"] != "FEwhat_to_watch" {
				t.Error("not home", body)
			}
			return 200, richGrid(continuationItem("fixture-next-page"))
		case 1:
			if body["continuation"] != "fixture-next-page" || body["browseId"] != nil {
				t.Error("not a genuine continuation", body)
			}
			return 200, appended(rich(lockup("jNQXAC9IVRw", "Actual continuation upload")))
		}
		t.Error("unexpected request")
		return 500, ""
	}}
	result := server.home().Fetch(context.Background(), fixtureCookie, nil)
	if result.Mode != "account" || fmt.Sprint(titles(result.Entries)) != "[Actual continuation upload]" {
		t.Fatal(result)
	}
	for _, headers := range server.headers {
		if headers.Get("Cookie") != fixtureCookie || !strings.HasPrefix(headers.Get("Authorization"), "SAPISIDHASH ") {
			t.Fatal("continuation lost authentication")
		}
	}
}

func fixtureState(token string, expires time.Time) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"token":%q,"context":{"client":{"clientName":"WEB","clientVersion":"fixture-version","visitorData":"fixture-visitor"},"user":{},"request":{}},"apiKey":"fixture-key","apiVersion":"v1","accountIndex":0,"mode":"account","expiresAt":%d}`, token, expires.UnixMilli()))
}

func TestSavedContinuationRequestsNextPageWithOriginalSession(t *testing.T) {
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if body["continuation"] != "fixture-next-page" || body["browseId"] != nil || path(body, "context", "client", "visitorData") != "fixture-visitor" {
			t.Error("lost continuation session", body)
		}
		return 200, appended(rich(lockup("jNQXAC9IVRw", "Next page without home")))
	}}
	home := server.home()
	result := home.Fetch(context.Background(), fixtureCookie, fixtureState("fixture-next-page", fixtureNow.Add(10*time.Minute)))
	if fmt.Sprint(titles(result.Entries)) != "[Next page without home]" || len(server.requests) != 1 {
		t.Fatal(result, len(server.requests))
	}
	if server.headers[0].Get("X-Goog-Visitor-Id") != "fixture-visitor" {
		t.Fatal("visitor header not from the saved session")
	}
	for _, state := range []json.RawMessage{
		fixtureState("fixture-next-page", fixtureNow.Add(-time.Millisecond)),
		fixtureState("", fixtureNow.Add(10*time.Minute)),
		fixtureState("fixture-next-page", fixtureNow.Add(21*time.Minute)),
		json.RawMessage(`"invalid"`),
		json.RawMessage(strings.Replace(string(fixtureState("t", fixtureNow.Add(time.Minute))), `"clientName":"WEB"`, `"clientName":"ANDROID"`, 1)),
		json.RawMessage(strings.Replace(string(fixtureState("t", fixtureNow.Add(time.Minute))), `"accountIndex":0`, `"accountIndex":1.5`, 1)),
	} {
		if result := home.Fetch(context.Background(), "", state); result.Error != "continuation_expired" {
			t.Fatal("invalid continuation accepted", string(state), result)
		}
	}
	if len(server.requests) != 1 {
		t.Fatal("invalid continuation reached the network")
	}
}

func TestContinuationCaptureExcludesHeaderAndBoundsExpiryAndSize(t *testing.T) {
	data := object(t, `{"header":{"feedTabbedHeaderRenderer":{"items":[`+continuationItem("header-only")+`]}},"contents":{"richGridRenderer":{"contents":[`+continuationItem("next-body-page")+`]}}}`)
	page := parseFeedPage(data)
	s := session{Context: map[string]any{"client": map[string]any{"clientName": "WEB"}}, APIKey: "key", APIVersion: "v1"}
	raw := captureContinuation(page, s, "account", time.UnixMilli(1000))
	var state continuationState
	if json.Unmarshal(raw, &state) != nil || state.Token != "next-body-page" || state.ExpiresAt != 1201000 {
		t.Fatal(string(raw))
	}
	if _, ok := state.session(time.UnixMilli(1000)); !ok {
		t.Fatal("captured continuation is invalid")
	}
	if captureContinuation(parseFeedPage(object(t, richGrid(rich(lockup("jNQXAC9IVRw", "Only"))))), s, "account", fixtureNow) != nil {
		t.Fatal("continuation invented")
	}
	oversized := parseFeedPage(object(t, richGrid(continuationItem(strings.Repeat("x", 16385)))))
	if captureContinuation(oversized, s, "account", fixtureNow) != nil {
		t.Fatal("oversized token kept")
	}
}

func TestEmptyContinuationRetainsNextTokenAfterBudget(t *testing.T) {
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		expected := "page-start"
		if index > 0 {
			expected = fmt.Sprintf("page-%d", index)
		}
		if body["continuation"] != expected || index > 2 {
			t.Error("unexpected continuation", body["continuation"])
		}
		return 200, appended(continuationItem(fmt.Sprintf("page-%d", index+1)))
	}}
	result := server.home().Fetch(context.Background(), "", fixtureState("page-start", fixtureNow.Add(10*time.Minute)))
	if result.Error != "" || len(result.Entries) != 0 || len(server.requests) != 3 {
		t.Fatal(result, len(server.requests))
	}
	var state continuationState
	if json.Unmarshal(result.Continuation, &state) != nil || state.Token != "page-3" {
		t.Fatal(string(result.Continuation))
	}
	if _, ok := state.session(fixtureNow); !ok {
		t.Fatal("retained continuation is invalid")
	}
}

func TestEmptyShellsFollowContinuationAndDeduplicate(t *testing.T) {
	video := rich(classic("jNQXAC9IVRw", "Actual upload"))
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		switch index {
		case 0:
			return 200, richGrid(continuationItem("middle"))
		case 1:
			return 200, appended(continuationItem("final"))
		}
		return 200, appended(video, video)
	}}
	result := server.home().Fetch(context.Background(), "", nil)
	if len(server.requests) != 3 || len(result.Entries) != 1 || result.Entries[0].ID != "jNQXAC9IVRw" || result.Mode != "anonymous" {
		t.Fatal(result, len(server.requests))
	}
}

func TestContinuationIsBoundedAndKeepsExistingVideos(t *testing.T) {
	endless := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		return 200, appended(continuationItem(fmt.Sprintf("endless-%d", index)))
	}}
	result := endless.home().Fetch(context.Background(), "", nil)
	if len(endless.requests) != 3 || result.Error != "empty" {
		t.Fatal("continuation not bounded", result, len(endless.requests))
	}
	broken := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if index > 0 {
			t.Error("feed with videos requested another page")
			return 500, ""
		}
		return 200, richGrid(rich(classic("jNQXAC9IVRw", "Kept upload")), continuationItem("next"))
	}}
	if result := broken.home().Fetch(context.Background(), "", nil); len(result.Entries) != 1 || result.Continuation == nil {
		t.Fatal(result)
	}
}

func TestRecommendationBatchesKeepAllVideosWithinBudget(t *testing.T) {
	var items []string
	for index := 0; index < 35; index++ {
		items = append(items, rich(classic(numberedID(index), fmt.Sprintf("Full batch %d", index))))
	}
	items = append(items, items[0], continuationItem("must-not-skip"))
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) { return 200, richGrid(items...) }}
	result := server.home().Fetch(context.Background(), "", nil)
	if len(result.Entries) != 35 || result.Entries[34].ID != numberedID(34) || len(server.requests) != 1 {
		t.Fatal(len(result.Entries), len(server.requests))
	}
	var oversized []string
	for index := 0; index < 201; index++ {
		oversized = append(oversized, rich(classic(numberedID(index), "Bounded")))
	}
	server = &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if body["browseId"] == "FEsubscriptions" {
			t.Error("oversized home batch substituted subscription uploads")
		}
		return 200, richGrid(oversized...)
	}}
	if result := server.home().Fetch(context.Background(), fixtureCookie, nil); result.Error != "batch_size" {
		t.Fatal(result)
	}
}

func TestHomeFailureRecoversAccountSubscriptions(t *testing.T) {
	handler := func(index int, body map[string]any) (int, string) {
		if body["browseId"] == "FEwhat_to_watch" {
			return 500, "unsupported home"
		}
		return 200, richGrid(rich(classic("jNQXAC9IVRw", "Subscribed upload")))
	}
	server := &browseServer{handler: handler}
	result := server.home().Fetch(context.Background(), fixtureCookie, nil)
	if result.Mode != "subscriptions" || len(result.Entries) != 1 {
		t.Fatal(result)
	}
	anonymous := &browseServer{handler: handler}
	if result := anonymous.home().Fetch(context.Background(), "", nil); result.Error != "network" || len(anonymous.requests) != 1 {
		t.Fatal("anonymous sessions must not request account subscriptions", result)
	}
}

func TestStatusCodesBecomeDiagnoses(t *testing.T) {
	for status, code := range map[int]string{401: "auth", 403: "auth", 429: "restricted", 500: "network"} {
		server := &browseServer{handler: func(int, map[string]any) (int, string) { return status, "" }}
		if result := server.home().Fetch(context.Background(), "", nil); result.Error != code {
			t.Fatal(status, result)
		}
	}
}

func TestHomeNudgeIsDistinguishedFromEmptyAndPreservedOverFallbackFailure(t *testing.T) {
	nudge := richGrid(`{"richSectionRenderer":{"content":{"feedNudgeRenderer":{"title":{"runs":[{"text":"Start watching"}]}}}}}`)
	for _, test := range []struct {
		home, subscriptions string
		subscriptionStatus  int
		cookie, expected    string
	}{
		{nudge, "", 200, "", "home_nudge"},
		{richGrid(), "", 200, "", "empty"},
		{nudge, "", 500, fixtureCookie, "home_nudge"},
		{richGrid(), "", 500, fixtureCookie, "network"},
	} {
		server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
			if body["browseId"] == "FEsubscriptions" {
				return test.subscriptionStatus, richGrid()
			}
			return 200, test.home
		}}
		if result := server.home().Fetch(context.Background(), test.cookie, nil); result.Error != test.expected {
			t.Fatal(test.expected, result)
		}
	}
	primary := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if body["browseId"] == "FEsubscriptions" {
			return 500, ""
		}
		return 429, ""
	}}
	if result := primary.home().Fetch(context.Background(), fixtureCookie, nil); result.Error != "restricted" || len(primary.requests) != 2 {
		t.Fatal("subscription fallback did not run after a home failure", result)
	}
}

func TestRejectedSubscriptionFallbackStops(t *testing.T) {
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if body["browseId"] == "FEwhat_to_watch" {
			return 200, `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":false}},"contents":{}}`
		}
		return 200, `{"responseContext":{"mainAppWebResponseContext":{"loggedOut":true}}}`
	}}
	if result := server.home().Fetch(context.Background(), fixtureCookie, nil); result.Error != "auth" {
		t.Fatal(result)
	}
}

func normalized(t *testing.T, items ...string) []HomeEntry {
	t.Helper()
	entries, err := normalizeCards(parseFeedPage(object(t, richGrid(items...))).cards, homeResultLimit)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestShortsPlaylistsAndStationsAreExcluded(t *testing.T) {
	var items []string
	for index, kind := range []string{"SHORT", "MOVIE", "PLAYLIST", "STATION"} {
		id := []string{"abcdefghijk", "jNQXAC9IVRw", "dQw4w9WgXcQ", "zyxwvutsrqp"}[index]
		items = append(items, `{"lockupViewModel":{"contentId":"`+id+`","contentType":"LOCKUP_CONTENT_TYPE_`+kind+`","metadata":{"lockupMetadataViewModel":{"title":{"content":"`+kind+`"}}}}}`)
	}
	items = append(items, `{"reelItemRenderer":{"videoId":"abcdefghijk","headline":{"simpleText":"Old short"}}}`)
	items = append(items, classic("zyxwvutsrqp", "Brief regular upload"))
	if got := fmt.Sprint(titles(normalized(t, items...))); got != "[MOVIE Brief regular upload]" {
		t.Fatal(got)
	}
}

func TestShortsOnlyHomeFallsBackAndShortsDoNotUseTheCap(t *testing.T) {
	short := rich(`{"shortsLockupViewModel":{"entityId":"shorts-entity-not-a-video-id","onTap":{"innertubeCommand":{"reelWatchEndpoint":{"videoId":"abcdefghijk"}}},"overlayMetadata":{"primaryText":{"content":"Shorts actual title"}}}}`)
	fallback := 0
	server := &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if body["browseId"] == "FEsubscriptions" {
			fallback++
			return 200, richGrid()
		}
		return 200, richGrid(short)
	}}
	if result := server.home().Fetch(context.Background(), fixtureCookie, nil); result.Error != "empty" || fallback != 1 {
		t.Fatal(result, fallback)
	}
	var shorts []string
	for index := 0; index < 22; index++ {
		shorts = append(shorts, rich(`{"reelItemRenderer":{"videoId":"`+numberedID(index)+`","headline":{"simpleText":"Short"}}}`))
	}
	video := rich(`{"lockupViewModel":{"contentId":"jNQXAC9IVRw","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Normal upload"},"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[{"text":{"content":"Actual channel"}}]}]}}}}}}`)
	server = &browseServer{handler: func(index int, body map[string]any) (int, string) {
		if index == 0 {
			return 200, richGrid(append(shorts, continuationItem("more"))...)
		}
		return 200, appended(video)
	}}
	result := server.home().Fetch(context.Background(), fixtureCookie, nil)
	if result.Mode != "account" || len(result.Entries) != 1 || result.Entries[0].Author != "Actual channel" || len(server.requests) != 2 {
		t.Fatal(result, len(server.requests))
	}
}

func TestClassicAndModernCardsNormalizeWithoutFabricatedValues(t *testing.T) {
	entries := normalized(t,
		`{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"runs":[{"text":"Clas"},{"text":"sic"}]},"ownerText":{"runs":[{"text":"Author"}]},"lengthText":{"simpleText":"0:19"},"thumbnail":{"thumbnails":[{"url":"https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg"}]}}}`,
		`{"lockupViewModel":{"contentId":"dQw4w9WgXcQ","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Modern"},"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[{"text":{"content":"Channel"}}]}]}}}}}}`,
		classic("jNQXAC9IVRw", "duplicate"),
		`{"lockupViewModel":{"contentId":"PLnotVideo","contentType":"LOCKUP_CONTENT_TYPE_PLAYLIST"}}`,
		classic("bad", "bad"),
	)
	if len(entries) != 2 || entries[0].Title != "Classic" || entries[0].Duration != 19 || entries[0].Author != "Author" || entries[1].Duration != 0 || entries[1].Author != "Channel" {
		t.Fatal(entries)
	}
	if entries[1].Thumbnail != "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg" {
		t.Fatal(entries[1].Thumbnail)
	}
}

func TestDurationsFromBadgesAndOverlaysIgnoreLiveLabels(t *testing.T) {
	entries := normalized(t,
		`{"lockupViewModel":{"contentId":"dQw4w9WgXcQ","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Modern"}}},"contentImage":{"thumbnailViewModel":{"overlays":[{"thumbnailOverlayBadgeViewModel":{"thumbnailBadges":[{"thumbnailBadgeViewModel":{"text":"12:34"}}]}}]}}}}`,
		`{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"simpleText":"Long"},"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"text":{"simpleText":"1:02:03"}}}]}}`,
		`{"gridVideoRenderer":{"videoId":"abcdefghijk","title":{"simpleText":"Live"},"thumbnailOverlays":[{"thumbnailOverlayTimeStatusRenderer":{"text":{"simpleText":"LIVE"}}}]}}`,
		`{"playlistVideoRenderer":{"videoId":"zyxwvutsrqp","title":{"simpleText":"Seconds"},"lengthSeconds":"1209600"}}`,
	)
	var durations []int
	for _, entry := range entries {
		durations = append(durations, entry.Duration)
	}
	if fmt.Sprint(durations) != "[754 3723 0 604800]" {
		t.Fatal(durations)
	}
}

func TestPublicationLabelsSurviveWithoutInventingDates(t *testing.T) {
	entries := normalized(t,
		`{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"simpleText":"Classic"},"publishedTimeText":{"simpleText":"2 weeks ago"}}}`,
		`{"lockupViewModel":{"contentId":"dQw4w9WgXcQ","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"发布时间"},"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[{"text":{"content":"Author"}}]},{"metadataParts":[{"text":{"content":"12万次观看"}},{"text":{"content":"3天前"}}]}]}}}}}}`,
		`{"lockupViewModel":{"contentId":"abcdefghijk","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Missing"},"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[{"text":{"content":"Author"}}]},{"metadataParts":[{"text":{"content":"12万次观看"}}]}]}}}}}}`,
	)
	if entries[0].PublishedText != "2 weeks ago" || entries[1].PublishedText != "3天前" || entries[2].PublishedText != "" {
		t.Fatal(entries)
	}
}

func TestAvatarsComeFromChannelImagesNotCovers(t *testing.T) {
	entries := normalized(t,
		`{"lockupViewModel":{"contentId":"dQw4w9WgXcQ","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Modern avatar"},"image":{"decoratedAvatarViewModel":{"avatar":{"avatarViewModel":{"image":{"sources":[{"url":"https://yt3.googleusercontent.com/channel-avatar","width":48,"height":48}]}}}}}}}}}`,
		`{"lockupViewModel":{"contentId":"jNQXAC9IVRw","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Shared upload"},"image":{"avatarStackViewModel":{"avatars":[{"avatarViewModel":{"image":{"sources":[{"url":"https://yt3.ggpht.com/primary-author"}]}}}]}}}}}}`,
		`{"videoRenderer":{"videoId":"abcdefghijk","title":{"simpleText":"Unsafe avatar"},"channelThumbnailSupportedRenderers":{"channelThumbnailWithLinkRenderer":{"thumbnail":{"thumbnails":[{"url":"https://evil.example/avatar"}]}}}}}`,
	)
	if entries[0].Avatar != "https://yt3.googleusercontent.com/channel-avatar" || entries[0].Avatar == entries[0].Thumbnail {
		t.Fatal(entries[0])
	}
	if entries[1].Avatar != "https://yt3.ggpht.com/primary-author" || entries[2].Avatar != "" || entries[2].Author != "" {
		t.Fatal(entries[1:])
	}
}

func TestImagesPreferTheLargestTrustedSource(t *testing.T) {
	entries := normalized(t, `{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"simpleText":"Clear cover"},
		"thumbnail":{"thumbnails":[{"url":"https://i.ytimg.com/vi/jNQXAC9IVRw/small.jpg","width":120},{"url":"https://evil.example/cover.jpg","width":8000},{"url":"https://i.ytimg.com/vi/jNQXAC9IVRw/large.jpg","width":1280},{"url":"https://i.ytimg.com/vi/jNQXAC9IVRw/medium.jpg","width":320}]},
		"channelThumbnailSupportedRenderers":{"channelThumbnailWithLinkRenderer":{"thumbnail":{"thumbnails":[{"url":"https://yt3.ggpht.com/small","width":24},{"url":"https://yt3.googleusercontent.com/large","width":96}]}}}}}`)
	if entries[0].Thumbnail != "https://i.ytimg.com/vi/jNQXAC9IVRw/large.jpg" || entries[0].Avatar != "https://yt3.googleusercontent.com/large" {
		t.Fatal(entries[0])
	}
}

func TestRecommendationsKeepRealChannelIdentities(t *testing.T) {
	channel := "UC" + strings.Repeat("a", 22)
	entries := normalized(t,
		`{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"simpleText":"Classic"},"ownerText":{"runs":[{"text":"Author","navigationEndpoint":{"browseEndpoint":{"browseId":"`+channel+`"}}}]}}}`,
		`{"lockupViewModel":{"contentId":"dQw4w9WgXcQ","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Modern"},"metadata":{"contentMetadataViewModel":{"metadataRows":[{"metadataParts":[{"text":{"content":"Author","commandRuns":[{"onTap":{"innertubeCommand":{"browseEndpoint":{"browseId":"`+channel+`"}}}}]}}]}]}}}}}}`,
		`{"lockupViewModel":{"contentId":"zyxwvutsrqp","contentType":"LOCKUP_CONTENT_TYPE_VIDEO","metadata":{"lockupMetadataViewModel":{"title":{"content":"Avatar link"},"image":{"decoratedAvatarViewModel":{"rendererContext":{"commandContext":{"onTap":{"innertubeCommand":{"browseEndpoint":{"browseId":"`+channel+`"}}}}}}}}}}}`,
		`{"videoRenderer":{"videoId":"abcdefghijk","title":{"simpleText":"Invalid"},"ownerText":{"runs":[{"text":"Author","navigationEndpoint":{"browseEndpoint":{"browseId":"PLplaylist"}}}]}}}`,
	)
	if entries[0].AuthorID != channel || entries[1].AuthorID != channel || entries[2].AuthorID != channel || entries[3].AuthorID != "" {
		t.Fatal(entries)
	}
}

func TestMissingOwnerUsesBylineWithoutPlaceholders(t *testing.T) {
	entries := normalized(t,
		`{"videoRenderer":{"videoId":"jNQXAC9IVRw","title":{"simpleText":"Fallback"},"longBylineText":{"runs":[{"text":"Real byline author"}]}}}`,
		`{"videoRenderer":{"videoId":"dQw4w9WgXcQ","title":{"simpleText":"No owner"},"ownerText":{"runs":[{"text":"N/A"}]}}}`,
	)
	if entries[0].Author != "Real byline author" || entries[1].Author != "" {
		t.Fatal(entries)
	}
}

func TestLocalVisitorDataEncodesTheVisitorProtobuf(t *testing.T) {
	encoded := localVisitorData(time.Unix(1_700_000_000, 0))
	if !regexp.MustCompile(`^[A-Za-z0-9_%-]+$`).MatchString(encoded) {
		t.Fatal(encoded)
	}
	raw, _ := url.QueryUnescape(encoded)
	message, err := base64.URLEncoding.DecodeString(raw)
	if err != nil || len(message) < 14 || message[0] != 0x0a || message[1] != 11 || message[13] != 0x28 {
		t.Fatal("visitor data is not {1: id, 5: timestamp}", raw, err)
	}
	if timestamp, _ := binary.Uvarint(message[14:]); timestamp != 1_700_000_000 {
		t.Fatal("visitor timestamp lost", timestamp)
	}
}
