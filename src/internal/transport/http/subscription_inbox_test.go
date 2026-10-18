package httptransport

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
)

func TestSubscriptionInboxHTTPReadOnlyContractAndBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source, _, err := subscriptions.NewService(store, nil).Create(context.Background(), subscriptions.Input{Type: "rss", Query: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	item := content.Item{ID: "rss:" + hash, Platform: "rss", PlatformID: hash, Kind: "audio", Title: "Podcast", URL: "https://example.com/episode", MediaURL: "https://example.com/audio.mp3"}
	if _, err = store.UpdateSubscription(context.Background(), source.ID, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 10); err != nil {
		t.Fatal(err)
	}
	h := New(content.NewService(store), nil, nil, Options{SubscriptionInbox: subscriptions.NewInboxService(store)})
	send := func(path, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1"+path, nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := send("/api/subscription-inbox?kind=audio&read=unread", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Page  subscriptions.InboxPage `json:"page"`
		Count int                     `json:"count"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Page.Entries) != 1 || len(response.Page.Sources) != 1 || response.Count != 1 {
		t.Fatal(response, err)
	}
	if w = send("/api/subscription-inbox/"+item.ID, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "audio.mp3") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"?q=a&q=b", "?kind=unknown", "?cursor=invalid", "?sourceId=nope", "?foo=secret"} {
		if w = send("/api/subscription-inbox"+path, ""); w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(path, w.Code)
		}
	}
	if w = send("/api/subscription-inbox", "https://evil.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w = send("/api/subscription-inbox/rss:"+strings.Repeat("b", 64), ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if states, err := store.ListReadStates(context.Background(), []string{item.ID}); err != nil || len(states) != 1 || states[0].Read || states[0].UpdatedAt != 0 {
		t.Fatal("GET marked read", states, err)
	}
	if library, err := store.List(context.Background()); err != nil || len(library) != 0 {
		t.Fatal("GET bookmarked", err)
	}
	unavailable := New(content.NewService(store), nil, nil)
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/subscription-inbox", nil)
	w = httptest.NewRecorder()
	unavailable.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
