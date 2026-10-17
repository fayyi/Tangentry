package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/favoritesync"
)

type favoriteHTTPFixture struct{}

func (favoriteHTTPFixture) Folders(ctx context.Context) ([]favoritesync.Folder, error) {
	return []favoritesync.Folder{{ID: "12345", Title: "test folder", Count: 1}}, ctx.Err()
}
func (favoriteHTTPFixture) Page(ctx context.Context, id string, page int) (favoritesync.Page, error) {
	return favoritesync.Page{Folder: favoritesync.Folder{ID: id, Title: "test folder", Count: 1}, Items: []content.Item{{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "HTTP fixture", URL: "https://www.bilibili.com/video/BV17x411w7KC/"}}, Entries: 1}, ctx.Err()
}
func TestFavoriteSynchronizationHTTPContract(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "favorites.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := favoritesync.New(store, favoriteHTTPFixture{}, nil)
	defer service.Close()
	handler := New(content.NewService(store), nil, nil, Options{Favorites: service})
	send := func(method, path, body, kind string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		if kind != "" {
			r.Header.Set("Content-Type", kind)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := send("GET", "/api/favorite-sync", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"sources":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("GET", "/api/favorite-folders", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"12345"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("POST", "/api/favorite-sync", `{"query":"12345"}`, "application/json"); w.Code != 201 || !strings.Contains(w.Body.String(), `"autoDownload":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("POST", "/api/favorite-sync/12345/sync", "", ""); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		source, _ := service.Get(context.Background(), "12345")
		if !source.Active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if w := send("GET", "/api/favorite-sync", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"itemCount":1`) || strings.Contains(w.Body.String(), `"items"`) || strings.Contains(w.Body.String(), `"transfers"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send("GET", "/api/favorite-sync/12345", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "HTTP fixture") {
		t.Fatal(w.Code, w.Body.String())
	}
	policy := favoritesync.DefaultPolicy()
	policy.Enabled = true
	policy.TitleInclude, policy.TitleExclude, policy.AuthorInclude = "HTTP", "预告", "测试"
	data, _ := json.Marshal(policy)
	if w := send("PUT", "/api/favorite-sync/12345", string(data), "application/json"); w.Code != 200 || !strings.Contains(w.Body.String(), `"titleInclude":"HTTP"`) || !strings.Contains(w.Body.String(), `"titleExclude":"预告"`) || !strings.Contains(w.Body.String(), `"authorInclude":"测试"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"query":"12345","cookie":"must not accept"}`, `{"query":"../bad"}`, `{"query":"12345"}{}`, `{"query":"12345","policy":{"enabled":true}}`} {
		if w := send("POST", "/api/favorite-sync", body, "application/json"); w.Code != 400 || strings.Contains(w.Body.String(), "must not accept") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := send("POST", "/api/favorite-sync", `{"query":"12345"}`, "text/plain"); w.Code != 415 {
		t.Fatal(w.Code)
	}
	if w := send("DELETE", "/api/favorite-sync/12345", "", ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if items, _ := store.List(context.Background()); len(items) != 0 {
		t.Fatal("sync changed library")
	}
	if jobs, _ := store.ListDownloads(context.Background()); len(jobs) != 0 {
		t.Fatal("default sync started download")
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/favorite-sync/12345/sync", nil)
	r.Header.Set("Origin", "https://external.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("same-origin constraint lost", w.Code)
	}
}
