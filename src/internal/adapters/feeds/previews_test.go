package feeds

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"tangentry/internal/content"
	"testing"
	"time"
)

func TestCommentLinksAreNotSummaries(t *testing.T) {
	raw := strings.Replace(fixtureFeed, "</item>", `<description><![CDATA[<a href="https://example.com/comments">Comments</a>]]></description></item>`, 1)
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Description != "" {
		t.Fatal(feed, err)
	}
	if got := summaryText("<p>Comments on the design explain its history.</p>"); got != "Comments on the design explain its history." {
		t.Fatal("removed actual summary", got)
	}
}

func TestConcurrentSubscriptionsShareArticlePreview(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return response(r, 200, http.Header{"Content-Type": {"text/html"}}, `<article><h1>Shared article</h1><p>`+strings.Repeat("A real article paragraph shared by several independent RSS subscriptions. ", 20)+`</p></article>`), nil
	})}}
	item := content.Item{Platform: "rss", Kind: "article", URL: "https://example.com/shared"}
	first, second := []content.Item{item}, []content.Item{item}
	done := make(chan struct{})
	go func() { client.EnrichSummaries(context.Background(), first); close(done) }()
	<-entered
	other := make(chan struct{})
	go func() { client.EnrichSummaries(context.Background(), second); close(other) }()
	// A canceled subscriber must not cancel the original fetch or cache failure.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	client.EnrichSummaries(canceled, []content.Item{item})
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not finish")
	}
	select {
	case <-other:
	case <-time.After(3 * time.Second):
		t.Fatal("subscriber did not finish")
	}
	if calls.Load() != 1 || first[0].Description == "" || first[0].Description != second[0].Description {
		t.Fatal("duplicate fetch or missing summary", calls.Load(), first, second)
	}
	client.previewMu.Lock()
	defer client.previewMu.Unlock()
	if len(client.previewPending) != 0 {
		t.Fatal("completed requests retained")
	}
}

func TestCanceledPreviewOwnerDoesNotPoisonOtherSubscription(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return response(r, 200, http.Header{"Content-Type": {"text/html"}}, `<article><p>`+strings.Repeat("The surviving subscriber can still retrieve the original article paragraph. ", 20)+`</p></article>`), nil
	})}}
	item := content.Item{Platform: "rss", Kind: "article", URL: "https://example.com/canceled"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { client.EnrichSummaries(ctx, []content.Item{item}); close(done) }()
	<-entered
	second := []content.Item{item}
	other := make(chan struct{})
	go func() { client.EnrichSummaries(context.Background(), second); close(other) }()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled owner did not finish")
	}
	select {
	case <-other:
	case <-time.After(3 * time.Second):
		t.Fatal("surviving subscription did not finish")
	}
	if calls.Load() != 2 || !strings.HasPrefix(second[0].Description, "The surviving subscriber") {
		t.Fatal("cancellation cached as an upstream failure", calls.Load(), second)
	}
}

func TestMissingSummariesUseRealParagraphsAndCacheFailures(t *testing.T) {
	var calls atomic.Int32
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("article preview must be anonymous")
		}
		if r.URL.Path == "/blocked" {
			return response(r, 403, http.Header{}, ""), nil
		}
		return response(r, 200, http.Header{"Content-Type": {"text/html"}}, `<html><head><title>Original article</title></head><body><nav>Not the summary</nav><article><h1>Original article</h1><p>`+strings.Repeat("This paragraph comes from the original article and contains useful details. ", 20)+`</p></article></body></html>`), nil
	})}}
	items := []content.Item{
		{Platform: "rss", Kind: "article", URL: "https://example.com/story", Description: "Comments"},
		{Platform: "rss", Kind: "article", URL: "https://example.com/blocked", Description: "Comments"},
		{Platform: "rss", Kind: "article", URL: "https://example.com/provided", Description: "The feed's own useful summary."},
		{Platform: "rss", Kind: "audio", URL: "https://example.com/audio"},
		{Platform: "rss", Kind: "article", URL: "https://example.com/story", Description: "Comments"},
		{Platform: "rss", Kind: "article", URL: "https://example.com/blocked", Description: "Comments"},
	}
	client.EnrichSummaries(context.Background(), items)
	if items[4].Description != items[0].Description || items[5].Description != "" {
		t.Fatal("duplicate summaries differ", items)
	}
	if !strings.HasPrefix(items[0].Description, "This paragraph") || len([]rune(items[0].Description)) > 280 || items[1].Description != "" || items[2].Description != "The feed's own useful summary." || calls.Load() != 2 {
		t.Fatal(items, calls.Load())
	}
	items[0].Description = "Comments"
	client.EnrichSummaries(context.Background(), items)
	if !strings.HasPrefix(items[0].Description, "This paragraph") || calls.Load() != 2 {
		t.Fatal("previews or failures not cached", items, calls.Load())
	}
}
