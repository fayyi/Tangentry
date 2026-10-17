package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

func TestRecommendationHTTPPersistsFetchedItems(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{
		"TANGENTRY_DOWNLOAD_DIR":         filepath.Join(root, "downloads"),
		"TANGENTRY_YTDLP_PATH":           filepath.Join(root, "absent-tool"),
		"TANGENTRY_FFMPEG_PATH":          filepath.Join(root, "absent-tool"),
		"TANGENTRY_BILIBILI_COOKIE_FILE": "", "TANGENTRY_BILIBILI_COOKIE": "",
	} {
		t.Setenv(name, value)
	}
	item := content.Item{ID: "youtube:abcdefghijk", Platform: "youtube", PlatformID: "abcdefghijk", Kind: "video", Title: "队列测试", Author: "测试作者", URL: "https://www.youtube.com/watch?v=abcdefghijk"}
	application, err := New(root, fstest.MapFS{}, Options{Recommendations: func(context.Context, string) (discovery.Feed, error) {
		return discovery.Feed{Items: []content.Item{item}, Page: 1, Total: 1}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	w := httptest.NewRecorder()
	application.HTTPHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/youtube", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	store, err := sqlite.Open(filepath.Join(root, "tangentry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queued, err := store.ListQueuedRecommendations(context.Background(), discovery.QueueScope{Platform: "youtube", Account: discovery.PersonalAccount}, 20)
	if err != nil || len(queued) != 1 || queued[0].Item.ID != item.ID {
		t.Fatal("HTTP recommendation was not persisted", queued, err)
	}
	readPending := func() []discovery.QueuedItem {
		t.Helper()
		w := httptest.NewRecorder()
		application.HTTPHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/youtube/pending", nil))
		var result struct {
			Items []discovery.QueuedItem `json:"items"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return result.Items
	}
	if len(readPending()) != 1 {
		t.Fatal("pending recommendation missing")
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/recommendations/youtube/pending?limit=501", ""},
		{"GET", "/api/recommendations/subscriptions/pending", ""},
		{"PUT", "/api/recommendations/youtube/marks", `{"contentId":"bilibili:BV17x411w7KC","opened":true}`},
		{"PUT", "/api/recommendations/youtube/marks", `{"contentId":"youtube:abcdefghijk"}`},
		{"PUT", "/api/recommendations/youtube/marks", `{"contentId":"youtube:abcdefghijk","opened":true,"extra":true}`},
	} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		application.HTTPHandler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("invalid queue request accepted", tc.path, w.Code)
		}
	}
	for _, opened := range []bool{false, true} {
		body, _ := json.Marshal(map[string]any{"contentId": item.ID, "opened": opened})
		r := httptest.NewRequest("PUT", "http://127.0.0.1/api/recommendations/youtube/marks", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		application.HTTPHandler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		pending := readPending()
		if !opened && (len(pending) != 1 || pending[0].SeenAt == 0) {
			t.Fatal("seen mark missing", pending)
		}
		if opened && len(pending) != 0 {
			t.Fatal("opened recommendation still pending", pending)
		}
	}
}
