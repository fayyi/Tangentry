package xiaoyuzhou

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"tangentry/internal/discovery"
)

func recommendationResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestRecommendationPagesKeepCursorPrivateAndDeduplicate(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Header.Get("OS") != "ios" || r.Header.Get("BundleID") != "app.podcast.cosmos" {
			t.Fatal("inconsistent app identity")
		}
		switch calls {
		case 1:
			if body["loadMoreKey"] != "" {
				t.Fatal("first page carried stale cursor")
			}
			return recommendationResponse(`{"data":[{"eid":"686000000000000000000001","title":"第一集"}],"loadMoreKey":"private-next"}`), nil
		case 2:
			if body["loadMoreKey"] != "private-next" {
				t.Fatal("continuation missing")
			}
			return recommendationResponse(`{"data":[{"eid":"686000000000000000000001","title":"第一集"},{"eid":"686000000000000000000002","title":"第二集"}]}`), nil
		default:
			t.Fatal("cached page refetched")
			return nil, nil
		}
	})}}
	ctx := discovery.WithRecommendationSession(context.Background(), "window-a")
	first, err := c.Recommendations(ctx, 1, "private-token")
	if err != nil || !first.HasMore {
		t.Fatal(first, err)
	}
	second, err := c.Recommendations(ctx, 2, "rotated-token")
	if err != nil || second.HasMore || len(second.Items) != 1 || second.Items[0].Title != "第二集" || second.Total != 2 {
		t.Fatal(second, err)
	}
	if _, err = c.Recommendations(ctx, 2, "rotated-token"); err != nil || calls != 2 {
		t.Fatal("page retry did not reuse result", err)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "private-next") || strings.Contains(string(raw), "private-token") {
		t.Fatal("cursor or token leaked to browser")
	}
	if _, err = c.Recommendations(ctx, 3, "rotated-token"); err == nil || calls != 2 {
		t.Fatal("exhausted cursor requested upstream")
	}
}

func TestRecommendationRefreshAndRetryPreserveCurrentCursor(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch calls {
		case 1:
			return recommendationResponse(`{"data":[],"loadMoreKey":"retained"}`), nil
		case 2, 3:
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		case 4:
			if body["loadMoreKey"] != "retained" {
				t.Fatal("failed refresh or continuation consumed cursor")
			}
			return recommendationResponse(`{"data":[{"eid":"686000000000000000000002","title":"恢复后第二页"}]}`), nil
		default:
			t.Fatal("unexpected upstream request")
			return nil, nil
		}
	})}}
	ctx := context.Background()
	if _, err := c.Recommendations(ctx, 1, "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Recommendations(ctx, 1, "token"); err == nil {
		t.Fatal("failed refresh hidden")
	}
	if _, err := c.Recommendations(ctx, 2, "token"); err == nil {
		t.Fatal("failed continuation hidden")
	}
	if feed, err := c.Recommendations(ctx, 2, "token"); err != nil || len(feed.Items) != 1 {
		t.Fatal("continuation retry lost cursor", feed, err)
	}
}

func TestRecommendationSessionsAreIndependentBoundedAndExpire(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		next := "cursor-" + strconv.Itoa(calls)
		if calls == 3 {
			if body["loadMoreKey"] != "cursor-1" {
				t.Fatal("another window replaced the first cursor")
			}
			next = "cursor-1" // Repeated cursors must terminate browsing.
		}
		return recommendationResponse(`{"data":[],"loadMoreKey":"` + next + `"}`), nil
	})}}
	a := discovery.WithRecommendationSession(context.Background(), "a")
	b := discovery.WithRecommendationSession(context.Background(), "b")
	for _, ctx := range []context.Context{a, b} {
		if _, err := c.Recommendations(ctx, 1, "token"); err != nil {
			t.Fatal(err)
		}
	}
	if feed, err := c.Recommendations(a, 2, "token"); err != nil || feed.HasMore {
		t.Fatal("repeated cursor advertised another page", feed, err)
	}
	for i := 0; i < 6; i++ {
		if _, err := c.Recommendations(discovery.WithRecommendationSession(context.Background(), strconv.Itoa(i)), 1, "token"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Recommendations(discovery.WithRecommendationSession(context.Background(), "overflow"), 1, "token"); err == nil {
		t.Fatal("session capacity ignored")
	}
	c.recommendationSessions["a"].expires = time.Now().Add(-time.Minute)
	before := calls
	if _, err := c.Recommendations(a, 2, "token"); err == nil || before != calls {
		t.Fatal("expired session continued upstream")
	}
	if _, err := c.Recommendations(a, 1, "token"); err != nil {
		t.Fatal("expired session prevented refresh", err)
	}
}

func TestRecommendationRejectsMalformedCursorAndOversizedResponses(t *testing.T) {
	for _, body := range []string{
		`{"data":[],"loadMoreKey":{"invalid":"shape"}}`,
		`{"data":[],"loadMoreKey":"` + strings.Repeat("a", 4097) + `"}`,
		`{"data":null}`,
		`{"data":[]}` + strings.Repeat(" ", 4<<20),
	} {
		c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return recommendationResponse(body), nil
		})}}
		if _, err := c.Recommendations(context.Background(), 1, "token"); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestRecommendationConcurrentRequestsAndCanceledRefresh(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(discovery.WithRecommendationSession(context.Background(), "slow-window"))
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Context() == ctx {
			close(entered)
			<-release
		}
		return recommendationResponse(`{"data":[],"loadMoreKey":"next"}`), nil
	})}}
	done := make(chan error, 1)
	go func() {
		_, err := c.Recommendations(ctx, 1, "token")
		done <- err
	}()
	<-entered
	if _, err := c.Recommendations(discovery.WithRecommendationSession(context.Background(), "slow-window"), 1, "token"); err == nil {
		t.Fatal("same-window concurrent fetch accepted")
	}
	if _, err := c.Recommendations(discovery.WithRecommendationSession(context.Background(), "other-window"), 1, "token"); err != nil {
		t.Fatal("another window blocked", err)
	}
	cancel()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("canceled result committed")
	}
	if _, err := c.Recommendations(discovery.WithRecommendationSession(context.Background(), "slow-window"), 2, "token"); err == nil {
		t.Fatal("canceled first page established a continuation")
	}
}
