package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestOPMLHTTPRejectsTruncationBeforeAnySourceWrite(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "opml-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) {
		t.Fatal("import must not fetch sources")
		return subscriptions.Snapshot{}, nil
	})
	defer service.Close()
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	valid := `<opml><body><outline xmlUrl="https://example.com/feed"/></body></opml>`
	var large strings.Builder
	large.WriteString(`<opml><body>`)
	for i := 0; i < 501; i++ {
		fmt.Fprintf(&large, `<outline xmlUrl="https://example.com/%d"/>`, i)
	}
	large.WriteString(`</body></opml>`)
	for _, body := range []string{valid + valid, valid + " trailing", large.String()} {
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/subscriptions/opml", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "500") || strings.Contains(w.Body.String(), "example.com") {
			t.Fatal("unsafe or unclear OPML error", w.Code, w.Body.String())
		}
		sources, err := service.List(context.Background())
		if err != nil || len(sources) != 0 {
			t.Fatal("invalid import wrote sources", err)
		}
	}
}

func TestRSSAddressHTTPKeepsSourceIdentityAndReturnsSafeConflictErrors(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "address-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	service := subscriptions.NewService(store, nil, subscriptions.WithRSSFeeds(func(context.Context, subscriptions.Source, subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		calls++
		return subscriptions.ConditionalResult{}, nil
	}))
	defer service.Close()
	original := "https://rsshub.example/feed?key=original-fixture-key"
	source, _, err := service.Create(context.Background(), subscriptions.Input{Type: "rss", Query: original})
	if err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	path := "/api/subscriptions/" + source.ID + "/address"
	send := func(path, body, mime, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "http://127.0.0.1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", mime)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"url":"https://rsshub.example/feed?key=current-fixture-key","expectedUrl":"` + original + `"}`
	for _, bad := range []string{`{}`, `{"url":"https://example.com/new"}`, `{"url":null,"expectedUrl":"https://example.com/old"}`, body + `{}`, strings.Replace(body, `"url":`, `"unknown":`, 1)} {
		if w := send(path, bad, "application/json", ""); w.Code != 400 || strings.Contains(w.Body.String(), "fixture-key") {
			t.Fatal("invalid address input accepted or reflected")
		}
	}
	if w := send(path+"?unexpected=1", body, "application/json", ""); w.Code != 400 {
		t.Fatal("address query parameters accepted")
	}
	if w := send(path, body, "text/plain", ""); w.Code != 415 {
		t.Fatal("address MIME boundary")
	}
	if w := send(path, body, "application/json", "https://external.example"); w.Code != 403 {
		t.Fatal("address origin boundary")
	}
	w := send(path, body, "application/json", "")
	if w.Code != 200 || calls != 0 || strings.Contains(w.Body.String(), "original-fixture-key") {
		t.Fatal("address edit fetched remotely or exposed original key", w.Code)
	}
	var saved subscriptions.Source
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.ID != source.ID || saved.URL != "https://rsshub.example/feed?key=current-fixture-key" {
		t.Fatal("address edit changed source identity")
	}
	w = send(path, body, "application/json", "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"subscription_address_conflict"`) || strings.Contains(w.Body.String(), "fixture-key") {
		t.Fatal("stale address did not return a safe conflict")
	}
	other, _, err := service.Create(context.Background(), subscriptions.Input{Type: "rss", Query: "https://example.com/other"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _ := json.Marshal(subscriptions.AddressEdit{ExpectedURL: saved.URL, URL: other.URL})
	if w := send(path, string(duplicate), "application/json", ""); w.Code != 409 || !strings.Contains(w.Body.String(), `"subscription_address_exists"`) {
		t.Fatal("duplicate address HTTP contract")
	}
}

func TestSubscriptionHTTPPersistenceAndBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	var failure error
	service := subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) {
		calls++
		return subscriptions.Snapshot{Items: []content.Item{{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "http fixture", URL: "https://www.bilibili.com/video/BV17x411w7KC/"}}, Total: 1}, failure
	})
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	send := func(method, path, body, mime string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if mime != "" {
			r.Header.Set("Content-Type", mime)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("POST", "/api/subscriptions", `{"query":"12345","name":"source"}`, "application/json; charset=utf-8")
	if w.Code != 201 || calls != 0 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var result struct{ Source subscriptions.Source }
	json.Unmarshal(w.Body.Bytes(), &result)
	path := "/api/subscriptions/" + result.Source.ID
	if w := send("POST", "/api/subscriptions", `{"query":"https://space.bilibili.com/12345","name":"other"}`, "application/json"); w.Code != 200 || !strings.Contains(w.Body.String(), `"created":false`) {
		t.Fatal("duplicate status")
	}
	if w := send("GET", path, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("new source detail")
	}
	if w := send("POST", path+"/update", "", ""); w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "http fixture") {
		t.Fatal("update not saved")
	}
	failure = &discovery.PlatformError{Message: "limited fixture", Restricted: true}
	if w := send("POST", path+"/update", "", ""); w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("restriction not preserved")
	}
	if w := send("GET", path, "", ""); !strings.Contains(w.Body.String(), "http fixture") {
		t.Fatal("error lost cache")
	}
	if w := send("PATCH", path, `{"name":"renamed"}`, "application/json"); w.Code != 200 || !strings.Contains(w.Body.String(), "renamed") {
		t.Fatal("rename failed")
	}
	for _, body := range []string{`{"query":"BV17x411w7KC"}`, `{"query":"12345","unknown":"secret"}`, `{"query":"12345"}{}`, strings.Repeat(" ", 9<<10) + `{"query":"12345"}`} {
		if w := send("POST", "/api/subscriptions", body, "application/json"); w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("bad input accepted/leaked")
		}
	}
	if w := send("POST", "/api/subscriptions", `{"query":"12345"}`, "text/plain"); w.Code != 415 {
		t.Fatal("mime boundary")
	}
	if w := send("PATCH", path, `{"name":"ok","creatorId":"67890"}`, "application/json"); w.Code != 400 {
		t.Fatal("source identity mutable")
	}
	if w := send("GET", "/api/subscriptions/invalid", "", ""); w.Code != 400 {
		t.Fatal("invalid id accepted")
	}
	if w := send("GET", "/api/library", "", ""); !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("implicit collection")
	}
	if w := send("DELETE", path, "", ""); w.Code != 204 {
		t.Fatal("delete")
	}
	if w := send("POST", path+"/update", "", ""); w.Code != 404 {
		t.Fatal("deleted source fetched")
	}
	if w := send("GET", "/api/subscriptions", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"sources":[]`) {
		t.Fatal("empty list contract")
	}
}

func TestRSSRefreshPolicyHTTPRequiresExplicitChoice(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	service := subscriptions.NewService(store, func(context.Context, string) (subscriptions.Snapshot, error) {
		calls++
		return subscriptions.Snapshot{Items: []content.Item{}, Total: 0}, nil
	})
	defer service.Close()
	source, _, err := service.Create(context.Background(), subscriptions.Input{Type: "rss", Query: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	send := func(body, mime, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "http://127.0.0.1/api/subscriptions/"+source.ID+"/refresh-policy", strings.NewReader(body))
		r.Header.Set("Content-Type", mime)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"intervalSeconds":900}`, `{"enabled":null,"intervalSeconds":900}`, `{"enabled":true,"intervalSeconds":899}`, `{"enabled":true,"intervalSeconds":604801}`, `{"enabled":true,"intervalSeconds":900,"cookie":"secret"}`, `{"enabled":true,"intervalSeconds":900}{}`} {
		if w := send(body, "application/json", ""); w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	valid := `{"enabled":true,"intervalSeconds":900}`
	if w := send(valid, "text/plain", ""); w.Code != 415 {
		t.Fatal("MIME boundary")
	}
	if w := send(valid, "application/json", "https://evil.example"); w.Code != 403 {
		t.Fatal("Origin boundary", w.Code)
	}
	w := send(valid, "application/json", "")
	if w.Code != 200 || calls != 0 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("enable made platform request", w.Code, w.Body.String(), calls)
	}
	w = send(`{"enabled":false,"intervalSeconds":1800}`, "application/json", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"nextCheckAt":0`) {
		t.Fatal("disable failed")
	}
}

func TestSubscriptionBatchHTTPReportsPerSourceResults(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "batch-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil, subscriptions.WithRSSFeeds(func(_ context.Context, source subscriptions.Source, _ subscriptions.Validators) (subscriptions.ConditionalResult, error) {
		return subscriptions.ConditionalResult{NotModified: true}, nil
	}))
	defer service.Close()
	ctx := context.Background()
	first, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/first"})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/second"})
	if err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	send := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/subscriptions/batch", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := send(`{"action":"refresh-policy","ids":["` + first.ID + `","` + second.ID + `"],"enabled":false,"intervalSeconds":900}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"failed":0`) || strings.Count(w.Body.String(), `"ok":true`) != 2 {
		t.Fatalf("batch policy: %d %s", w.Code, w.Body.String())
	}
	missing := "rss:feed:" + strings.Repeat("a", 64)
	if w := send(`{"action":"delete","ids":["` + first.ID + `","` + missing + `"]}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"failed":1`) || !strings.Contains(w.Body.String(), `"subscription not found"`) {
		t.Fatalf("partial batch delete: %d %s", w.Code, w.Body.String())
	}
	if w := send(`{"action":"unknown","ids":["` + second.ID + `"]}`); w.Code != 400 {
		t.Fatalf("invalid batch action accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestYouTubeChannelHTTPResolveAndLocalCreate(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "youtube.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	id := "UCaaaaaaaaaaaaaaaaaaaaaa"
	service := subscriptions.NewService(store, nil, subscriptions.WithYouTubeResolver(func(_ context.Context, _ string) (discovery.YouTubeChannel, error) {
		calls++
		return discovery.YouTubeChannel{ID: id, Title: "channel fixture"}, nil
	}))
	defer service.Close()
	h := New(content.NewService(store), nil, nil, Options{Subscriptions: service})
	send := func(method, path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	resolve := "/api/subscriptions/youtube/resolve"
	w := send("POST", resolve, `{"query":"https://youtube.com/@fixture/videos"}`, "")
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), `"channelId":"`+id+`"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if sources, _ := service.List(context.Background()); len(sources) != 0 {
		t.Fatal("resolve created source")
	}
	for _, body := range []string{`{"query":"https://youtube.com/watch?v=abcdefghijk"}`, `{"query":"@fixture","cookies":"private"}`, `{"query":"@fixture"}{}`, `{"query":"https://youtube.com.evil.example/@fixture"}`} {
		w := send("POST", resolve, body, "")
		if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := send("POST", resolve, `{"query":"@fixture"}`, "https://evil.example"); w.Code != 403 || calls != 1 {
		t.Fatal("Origin boundary")
	}
	w = send("POST", resolve, `{"query":"`+id+`"}`, "")
	if w.Code != 200 || calls != 1 {
		t.Fatal("direct ID invoked network")
	}
	w = send("POST", "/api/subscriptions", `{"type":"youtube","query":"`+id+`","name":"my channel"}`, "")
	if w.Code != 201 || calls != 1 || !strings.Contains(w.Body.String(), `"id":"youtube:channel:`+id+`"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/api/subscriptions/youtube:channel:" + id
	w = send("PUT", path+"/refresh-policy", `{"enabled":true,"intervalSeconds":900}`, "")
	if w.Code != 200 || calls != 1 {
		t.Fatal("policy accessed platform", w.Code)
	}
	if w := send("GET", path, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("get has no empty cache")
	}
	if w := send("POST", "/api/subscriptions", `{"type":"youtube","query":"@fixture"}`, ""); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_youtube_channel") {
		t.Fatal("uncanonical channel accepted")
	}
}
