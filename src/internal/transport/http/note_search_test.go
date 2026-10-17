package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
	"testing"
)

func TestNoteSearchHTTPContractAndBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	h := New(nil, nil, nil, Options{Notes: service})
	send := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/notes/search?"+query, nil))
		return w
	}
	w := send("")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"notes":[]`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("empty contract wrong")
	}
	for _, id := range []string{"bilibili:BV17x411w7KC", "bilibili:BV1Pyta66EDh"} {
		if _, err := service.Create(context.Background(), notes.Input{ContentID: id, Body: "中文 Alpha 100%_"}); err != nil {
			t.Fatal(err)
		}
	}
	w = send("q=" + url.QueryEscape("alpha 100%_"))
	var page notes.SearchPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Notes) != 2 || page.NextCursor != "" || page.Query != "alpha 100%_" {
		t.Fatal("cross-source contract wrong")
	}
	for _, query := range []string{"q=" + url.QueryEscape(strings.Repeat("中", 201)), "cursor=bad", "q=%FF"} {
		if w := send(query); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_note_search") {
			t.Fatal("invalid search accepted")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/notes/search", nil)
	r.Header.Set("Origin", "https://external.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("source constraint lost")
	}
	w = httptest.NewRecorder()
	New(nil, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/notes/search", nil))
	if w.Code != 503 {
		t.Fatal("missing service succeeded")
	}
}

func TestMemoTagAndDateHTTPFilters(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	h := New(nil, nil, nil, Options{Notes: service})
	memo, err := service.Create(context.Background(), notes.Input{Body: "独立笔记", Tags: []string{"学习"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/notes/tags", "/api/notes/search?tag=" + url.QueryEscape("学习") + "&from=1&to=" + fmt.Sprint(memo.CreatedAt+1)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "学习") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"from=abc", "to=1.5", "from=-1", "from=10&to=10", "tag=bad%20tag"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/notes/search?"+query, nil))
		if w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
}
