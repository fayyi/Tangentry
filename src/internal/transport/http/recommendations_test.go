package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"testing"
	"testing/fstest"
	"time"
)

func TestRecommendationSourceWhitelistAndIndependentFailures(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "rec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := map[string]int{}
	h := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{"index.html": {Data: []byte("app")}}, Options{Recommendations: func(ctx context.Context, source string) (discovery.Feed, error) {
		calls[source]++
		if source == "youtube" {
			return discovery.Feed{}, discovery.ErrUnavailable
		}
		return discovery.Feed{Items: []content.Item{testItem}, Page: 1, Total: 1}, ctx.Err()
	}})
	for source, want := range map[string]int{"subscriptions": 200, "bilibili": 200, "youtube": 502, "search": 400} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/recommendations/"+source, nil))
		if w.Code != want {
			t.Fatalf("%s: %d", source, w.Code)
		}
	}
	if calls["search"] != 0 || calls["subscriptions"] != 1 || calls["bilibili"] != 1 {
		t.Fatal("source isolation", calls)
	}
	if items, _ := store.List(context.Background()); len(items) != 0 {
		t.Fatal("recommendation saved content")
	}
}

func TestRecommendationSwitchBlocksRemoteButPreservesInventory(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	scope := discovery.QueueScope{Platform: "bilibili", Account: discovery.PersonalAccount}
	if err := store.EnqueueRecommendations(ctx, scope, []content.Item{testItem}, 123); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{}, Options{
		RecommendationConfig: discovery.NewPreferences(store),
		RecommendationQueue: discovery.NewRecommendationQueue(store, func(source string) discovery.QueueScope {
			return discovery.QueueScope{Platform: source, Account: discovery.PersonalAccount}
		}),
		Recommendations: func(context.Context, string) (discovery.Feed, error) {
			calls++
			return discovery.Feed{Items: []content.Item{}}, nil
		},
	})
	for _, input := range []struct {
		source, body string
		want         int
	}{
		{"bilibili", `{"enabled":false}`, 200}, {"bad", `{"enabled":false}`, 400}, {"subscriptions", `{"enabled":false}`, 400},
		{"bilibili", `{}`, 400}, {"bilibili", `{"enabled":null}`, 400}, {"bilibili", `{"enabled":true,"extra":1}`, 400}, {"bilibili", `{"enabled":true} {}`, 400},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "http://127.0.0.1/api/settings/recommendations/"+input.source, strings.NewReader(input.body))
		r.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(w, r)
		if w.Code != input.want {
			t.Fatal(input, w.Code, w.Body.String())
		}
	}
	for _, input := range []struct {
		path string
		want int
	}{{"/api/recommendations/bilibili", 409}, {"/api/recommendations/bilibili/pending", 200}, {"/api/recommendations/subscriptions", 200}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+input.path, nil))
		if w.Code != input.want {
			t.Fatal(input, w.Code)
		}
		if strings.HasSuffix(input.path, "/pending") && !strings.Contains(w.Body.String(), testItem.ID) {
			t.Fatal("inventory removed")
		}
	}
	if calls != 1 {
		t.Fatal("disabled platform fetched", calls)
	}
}

func TestSimultaneousSourceRefreshQueuesAllPlatforms(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "parallel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var calls atomic.Int32
	h := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{}, Options{Recommendations: func(ctx context.Context, source string) (discovery.Feed, error) {
		calls.Add(1)
		select {
		case <-time.After(30 * time.Millisecond):
		case <-ctx.Done():
			return discovery.Feed{}, ctx.Err()
		}
		return discovery.Feed{Items: []content.Item{}}, nil
	}})
	var wg sync.WaitGroup
	for _, source := range []string{"subscriptions", "bilibili", "youtube", "xiaohongshu", "linuxdo", "xiaoyuzhou", "douyin"} {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/"+source, nil))
			if w.Code != 200 {
				t.Errorf("%s: %d", source, w.Code)
			}
		}(source)
	}
	wg.Wait()
	if calls.Load() != 7 {
		t.Fatal("refresh dropped sources", calls.Load())
	}
}

func TestRecommendationPageIsValidatedAndForwarded(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	h := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{}, Options{RecommendationPage: func(ctx context.Context, source string, page int) (discovery.Feed, error) {
		calls++
		if source != "bilibili" || page != 2 {
			t.Fatal(source, page)
		}
		return discovery.Feed{Items: []content.Item{testItem}, Page: page, HasMore: true}, nil
	}})
	for _, raw := range []string{"2", "0", "-1", "oops", "10001"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/bilibili?page="+raw, nil))
		want := 400
		if raw == "2" {
			want = 200
		}
		if w.Code != want {
			t.Fatal(raw, w.Code)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestRecommendationSessionHeaderIsValidatedAndScoped(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var received string
	h := New(content.NewService(store), discovery.NewService(testProvider{}), fstest.MapFS{}, Options{RecommendationPage: func(ctx context.Context, source string, page int) (discovery.Feed, error) {
		received = discovery.RecommendationSession(ctx)
		return discovery.Feed{Items: []content.Item{testItem}, Page: page}, nil
	}})
	for _, value := range []string{"bad", "short", "contains.dot"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/youtube?page=1", nil)
		req.Header.Set("X-Recommendation-Session", value)
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid session %q returned %d", value, w.Code)
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/recommendations/youtube?page=1", nil)
	req.Header.Set("X-Recommendation-Session", "window_session-1234")
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || received != "window_session-1234" {
		t.Fatal(w.Code, received)
	}
}
