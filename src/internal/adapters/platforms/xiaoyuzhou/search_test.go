package xiaoyuzhou

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCreatorListsRecentPodcastEpisodes(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: episodeListTransport{}}}
	page, err := c.Creator(context.Background(), "podcast-1", "next-key")
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "最近一集" || page.Items[0].AuthorID != "podcast-1" || page.Cursor != "next-2" || !page.HasMore {
		t.Fatalf("unexpected creator page: %+v, %v", page, err)
	}
}

func TestCreatorPreservesObjectLoadMoreKey(t *testing.T) {
	transport := &objectCursorTransport{}
	c := &Client{HTTP: &http.Client{Transport: transport}}
	first, err := c.Creator(context.Background(), "podcast-1", "")
	if err != nil || first.Cursor != `{"offset":20,"token":"next"}` || !first.HasMore {
		t.Fatalf("unexpected first page: %+v, %v", first, err)
	}
	second, err := c.Creator(context.Background(), "podcast-1", first.Cursor)
	if err != nil || len(second.Items) != 1 || second.Items[0].Title != "第二集" {
		t.Fatalf("unexpected second page: %+v, %v", second, err)
	}
}

func TestCreatorRejectsInvalidPodcastID(t *testing.T) {
	c := &Client{HTTP: http.DefaultClient}
	if _, err := c.Creator(context.Background(), "../secret", ""); err == nil {
		t.Fatal("invalid podcast id accepted")
	}
}

func TestCreatorHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{HTTP: &http.Client{Transport: blockingEpisodeListTransport{}}}
	done := make(chan error, 1)
	go func() {
		_, err := c.Creator(ctx, "podcast-1", "")
		done <- err
	}()
	// Let the request reach the transport before cancelling, avoiding a false
	// positive where the context was already done before any network work.
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("creator returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("creator did not stop after cancellation")
	}
}

type episodeListTransport struct{}

func (episodeListTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/v1/episode/list" || req.Header.Get("OS") != "ios" {
		return nil, io.ErrUnexpectedEOF
	}
	var payload map[string]any
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload["pid"] != "podcast-1" || payload["loadMoreKey"] != "next-key" {
		return nil, io.ErrUnexpectedEOF
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"eid":"686000000000000000000001","title":"最近一集","podcast":{"title":"测试节目","pid":"podcast-1"},"pubDate":"2026-10-10T12:34:56Z"}],"loadMoreKey":"next-2"}`)), Header: make(http.Header)}, nil
}

type blockingEpisodeListTransport struct{}

func (blockingEpisodeListTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

type objectCursorTransport struct{ page int }

func (t *objectCursorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var payload map[string]any
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload["pid"] != "podcast-1" {
		return nil, io.ErrUnexpectedEOF
	}
	if cursor, ok := payload["loadMoreKey"].(map[string]any); ok {
		if cursor["offset"] != float64(20) || cursor["token"] != "next" {
			return nil, io.ErrUnexpectedEOF
		}
		t.page = 2
	}
	if t.page == 2 {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"eid":"686000000000000000000002","title":"第二集","podcast":{"title":"测试节目","pid":"podcast-1"}}]}`)), Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"eid":"686000000000000000000001","title":"第一集","podcast":{"title":"测试节目","pid":"podcast-1"}}],"loadMoreKey":{"offset":20,"token":"next"}}`)), Header: make(http.Header)}, nil
}

func TestCreatorUsesEncodedOfficialEndpoint(t *testing.T) {
	u, err := url.Parse("https://api.xiaoyuzhoufm.com/v1/episode/list")
	if err != nil || u.Host != "api.xiaoyuzhoufm.com" {
		t.Fatal("unexpected endpoint")
	}
}
