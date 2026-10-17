package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/history"
)

func TestLibraryAndHistoryHTTP(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	library := content.NewService(store)
	h := New(library, nil, nil, Options{History: history.New(store)})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	article := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "article", Title: "收藏的长文章", URL: "https://example.com/a", ArticleBlocks: []content.ArticleBlock{{Kind: "paragraph", Text: "正文段落"}}}
	video := content.Item{ID: "youtube:abc", Platform: "youtube", PlatformID: "abc", Kind: "video", Title: "Video", URL: "https://www.youtube.com/watch?v=abc"}
	for _, item := range []content.Item{article, video} {
		if _, err := library.Save(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	w := send("GET", "/api/library?kind=article&q=长文章&limit=10", "")
	var page content.Page
	if json.Unmarshal(w.Body.Bytes(), &page); w.Code != 200 || page.Total != 1 || page.Counts["video"] != 0 || page.Items[0].ArticleBlocks != nil || !page.Items[0].Partial || strings.Contains(w.Body.String(), "正文段落") {
		t.Fatalf("filtered cards: %d %s", w.Code, w.Body.String())
	}
	if w := send("GET", "/api/library", ""); !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatal("unfiltered list", w.Body.String())
	}
	if w := send("GET", "/api/library/"+article.ID, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "正文段落") {
		t.Fatal("full item", w.Code, w.Body.String())
	}
	if w := send("GET", "/api/library/youtube:missing", ""); w.Code != 404 {
		t.Fatal("missing item", w.Code)
	}
	for _, query := range []string{"kind=movie", "limit=201", "limit=-1", "offset=x", "platform=a:b"} {
		if w := send("GET", "/api/library?"+query, ""); w.Code != 400 {
			t.Fatal("invalid query accepted", query, w.Code)
		}
	}
	partial, _ := json.Marshal(page.Items[0])
	if w := send("POST", "/api/library", string(partial)); w.Code != 400 {
		t.Fatal("partial card saved over the full item", w.Code)
	}

	entry, _ := json.Marshal(map[string]any{"entries": []any{map[string]any{"item": video, "openedAt": 1000}, map[string]any{"item": map[string]any{"id": "bad"}, "openedAt": 5}}})
	if w := send("POST", "/api/history/viewing", string(entry)); w.Code != 200 || !strings.Contains(w.Body.String(), `"kept":1`) {
		t.Fatal("record", w.Code, w.Body.String())
	}
	if w := send("GET", "/api/history/viewing", ""); !strings.Contains(w.Body.String(), `"openedAt":1000`) {
		t.Fatal("viewing list", w.Body.String())
	}
	if w := send("DELETE", "/api/history/viewing/"+video.ID, ""); w.Code != 204 {
		t.Fatal("remove", w.Code)
	}
	if w := send("POST", "/api/history/searches", `{"records":[{"id":"a1","query":"go","source":"youtube","createdAt":5}]}`); w.Code != 200 {
		t.Fatal("search add", w.Code, w.Body.String())
	}
	if w := send("GET", "/api/history/searches", ""); !strings.Contains(w.Body.String(), `"query":"go"`) {
		t.Fatal("search list", w.Body.String())
	}
	for _, body := range []string{`{"records":[]}`, `{"records":[{"id":"a1"}],"x":1}`, `{"records":[{"id":"a2","query":"q","source":"Bad Source","createdAt":5}]}`} {
		if w := send("POST", "/api/history/searches", body); w.Code != 400 {
			t.Fatal("invalid search accepted", body, w.Code)
		}
	}
	if w := send("DELETE", "/api/history/searches", ""); w.Code != 204 {
		t.Fatal("clear", w.Code)
	}
	if w := send("GET", "/api/history/searches", ""); !strings.Contains(w.Body.String(), `"records":[]`) {
		t.Fatal("cleared list", w.Body.String())
	}
}
