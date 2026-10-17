package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
)

var testItem = content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "测试视频", Author: "测试作者", URL: "https://www.bilibili.com/video/BV17x411w7KC/"}

type testProvider struct{}

func (testProvider) CreatorVideos(context.Context, string, int) (discovery.Feed, error) {
	return discovery.Feed{Items: []content.Item{testItem}, Page: 1, Total: 1}, nil
}
func (testProvider) Video(context.Context, string) (content.Item, error) { return testItem, nil }

func TestBrowseDoesNotSaveAndLibraryRoundTrip(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer(New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{"index.html": {Data: []byte("test app")}}))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/discovery/bilibili?q=12345")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("browse status %d", response.StatusCode)
	}
	items, _ := store.List(context.Background())
	if len(items) != 0 {
		t.Fatal("browsing saved content")
	}
	payload, _ := json.Marshal(testItem)
	for range 2 {
		response, err = http.Post(server.URL+"/api/library", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("save status %d", response.StatusCode)
		}
	}
	response, err = http.Get(server.URL + "/api/library")
	if err != nil {
		t.Fatal(err)
	}
	var library struct {
		Items []content.Item `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&library); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(library.Items) != 1 || library.Items[0].SavedAt == 0 {
		t.Fatalf("bad library %+v", library)
	}
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/library/"+testItem.ID, nil)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatalf("delete status %d", response.StatusCode)
	}
}

func TestUntrustedOriginsAndUnsafeContentAreRejected(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{"index.html": {Data: []byte("test app")}})
	for _, rawURL := range []string{"javascript:alert(1)", "", "https://user:secret@example.com/video"} {
		item := testItem
		item.URL = rawURL
		payload, _ := json.Marshal(item)
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/library", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("accepted unsafe URL %q: %d", rawURL, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/library", nil)
	req.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("untrusted origin accepted")
	}
	req = httptest.NewRequest(http.MethodGet, "http://untrusted.example/api/library", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("untrusted host accepted")
	}
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/library", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	data, _ := io.ReadAll(w.Result().Body)
	if w.Code != 200 || string(data) != "test app" {
		t.Fatal("SPA route fallback failed")
	}
}
