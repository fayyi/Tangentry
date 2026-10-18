package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
)

func TestSavedViewsHTTPCRUDAndQuery(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "saved-views.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	source, _, err := subscriptions.NewService(store, nil).Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/saved"})
	if err != nil {
		t.Fatal(err)
	}
	item := content.Item{ID: "rss:" + strings.Repeat("a", 64), Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "article", Title: "保存视图文章", Author: "作者", URL: "https://example.com/article", ThumbnailURL: "https://example.com/thumb.jpg"}
	if _, err = store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 10); err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{SubscriptionInbox: subscriptions.NewInboxService(store), SavedViews: subscriptions.NewSavedViewService(store)})
	send := func(method, path string, body any) *httptest.ResponseRecorder {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, bytes.NewReader(data))
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	created := send(http.MethodPost, "/api/saved-views", map[string]any{"name": "文章视图", "definition": map[string]any{"version": 1, "filters": map[string]any{"kind": "article", "read": "unread"}}, "icon": "file-text", "color": "teal"})
	if created.Code != http.StatusCreated {
		t.Fatal(created.Code, created.Body.String())
	}
	var view subscriptions.SavedView
	if err = json.Unmarshal(created.Body.Bytes(), &view); err != nil || view.ID == "" {
		t.Fatal(view, err)
	}
	name := "文章视图（改名）"
	updated := send(http.MethodPatch, "/api/saved-views/"+view.ID, map[string]any{"name": name, "default": true})
	if updated.Code != http.StatusOK || !bytes.Contains(updated.Body.Bytes(), []byte(name)) {
		t.Fatal(updated.Code, updated.Body.String())
	}
	page := send(http.MethodGet, "/api/saved-views/"+view.ID+"/inbox?limit=10", nil)
	if page.Code != http.StatusOK || !bytes.Contains(page.Body.Bytes(), []byte("保存视图文章")) || !bytes.Contains(page.Body.Bytes(), []byte(`"count":1`)) {
		t.Fatal(page.Code, page.Body.String())
	}
	if deleted := send(http.MethodDelete, "/api/saved-views/"+view.ID, nil); deleted.Code != http.StatusNoContent {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
}
