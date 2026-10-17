package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"tangentry/internal/discovery"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

type bodyReader func([]byte) (int, error)

func (f bodyReader) Read(p []byte) (int, error) { return f(p) }
func (f bodyReader) Close() error               { return nil }

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const fixtureFeed = `<rss version="2.0"><channel><title>fixture</title><link>https://example.com</link><description>feed</description><item><guid>one</guid><title>article</title><link>https://example.com/a</link></item></channel></rss>`

func TestEditedFeedFetchesOnlyCurrentAddressAndKeepsOriginalItemIdentity(t *testing.T) {
	ctx := context.Background()
	original := "https://rsshub.example/feed?key=original-fixture-key"
	current := "https://rsshub.example/feed?key=current-fixture-key"
	expected, err := Parse([]byte(fixtureFeed), original)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != current || strings.Contains(r.Header.Get("Referer"), "original-fixture-key") {
			t.Fatal("request used the private identity seed instead of the current address")
		}
		if calls == 1 {
			if r.Header.Get("If-None-Match") != "" {
				t.Fatal("old-address validators were sent to the replacement")
			}
			return response(r, 200, http.Header{"Etag": {`"current"`}}, fixtureFeed), nil
		}
		if r.Header.Get("If-None-Match") != `"current"` {
			t.Fatal("current-address validators were lost")
		}
		return response(r, 304, http.Header{}, ""), nil
	})}}
	first, err := client.FetchConditionalIdentity(ctx, current, original, Validators{ETag: `"old"`, ResourceURL: original})
	if err != nil || len(first.Feed.Items) != 1 || first.Feed.Items[0].ID != expected.Items[0].ID || first.Feed.Query != current || first.Validators.ResourceURL != current {
		t.Fatal("replacement changed identity or validator scope", err)
	}
	second, err := client.FetchConditionalIdentity(ctx, current, original, first.Validators)
	if err != nil || !second.NotModified || second.Validators != first.Validators || calls != 2 {
		t.Fatal("replacement conditional request failed", err)
	}
	if _, err := client.FetchConditionalIdentity(ctx, current, "invalid", Validators{}); err == nil || calls != 2 {
		t.Fatal("invalid identity was fetched")
	}
}

func TestTransportTimeoutIsDistinctAndNeverLeaksPrivateURLOrRetries(t *testing.T) {
	const secret = "private-timeout-key"
	for _, scenario := range []struct {
		err      error
		expected string
	}{
		{&net.DNSError{Err: "private resolver details " + secret, IsTimeout: true}, "响应超时"},
		{errors.New("private network details " + secret), "暂时无法连接"},
	} {
		calls := 0
		client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return nil, scenario.err
		})}}
		_, err := client.FetchConditional(context.Background(), "https://rsshub.example/feed?key="+secret, Validators{})
		if err == nil || !strings.Contains(err.Error(), scenario.expected) || strings.Contains(err.Error(), secret) || calls != 1 {
			t.Fatal("incorrect private transport diagnostic or unexpected retry", calls)
		}
	}
}

func TestPrivateRSSHubQueryAndSafeFailureMessages(t *testing.T) {
	const secret = "private-fixture-key"
	raw := "https://rsshub.example/xiaohongshu/user/example/notes?key=" + secret
	for _, scenario := range []struct {
		status   int
		expected string
	}{
		{200, ""}, {401, "访问密钥"}, {403, "访问密钥"}, {404, "HTTP 404"}, {503, "HTTP 503"},
	} {
		c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("key") != secret {
				t.Fatal("RSSHub key was lost")
			}
			body := fixtureFeed
			if scenario.status != 200 {
				body = "<html>upstream error " + secret + "</html>"
			}
			return response(r, scenario.status, http.Header{}, body), nil
		})}}
		result, err := c.FetchConditional(context.Background(), raw, Validators{})
		if scenario.status == 200 {
			if err != nil || len(result.Feed.Items) != 1 {
				t.Fatal("private feed failed", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), scenario.expected) || strings.Contains(err.Error(), secret) {
			t.Fatal("missing diagnostic or leaked key", scenario.status)
		}
	}
}

func TestHTMLSuccessResponseIsDiagnosedWithoutRejectingMislabeledFeeds(t *testing.T) {
	const secret = "private-html-key"
	for _, scenario := range []struct {
		body  string
		valid bool
	}{
		{"<!doctype html><!--proxy--><html><body>Login " + secret + "</body></html>", false},
		{fixtureFeed, true},
	} {
		calls := 0
		client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return response(r, 200, http.Header{"Content-Type": {"text/html"}}, scenario.body), nil
		})}}
		result, err := client.FetchConditional(context.Background(), "https://rsshub.example/feed?key="+secret, Validators{})
		if scenario.valid {
			if err != nil || len(result.Feed.Items) != 1 {
				t.Fatal("mislabeled valid feed rejected", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "返回了网页") || strings.Contains(err.Error(), secret) {
			t.Fatal("missing safe HTML diagnostic")
		}
		if calls != 1 {
			t.Fatal("unexpected retry")
		}
	}
}

func response(r *http.Request, status int, h http.Header, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func TestConditionalFetchUsesExactResourceAndPreserves304Validators(t *testing.T) {
	raw := "https://example.com/feed"
	modified := "Wed, 01 Oct 2025 10:00:00 GMT"
	calls := 0
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if r.Header.Get("If-None-Match") != "" {
				t.Fatal("first request conditional")
			}
			return response(r, 200, http.Header{"Etag": {`W/"one"`}, "Last-Modified": {modified}}, fixtureFeed), nil
		}
		if r.Header.Get("If-None-Match") != `W/"one"` || r.Header.Get("If-Modified-Since") != modified {
			t.Fatal("validators not sent")
		}
		return response(r, 304, http.Header{}, "must not parse"), nil
	})}}
	first, err := client.FetchConditional(context.Background(), raw, Validators{})
	if err != nil || len(first.Feed.Items) != 1 || first.Validators.ResourceURL != raw {
		t.Fatal(first, err)
	}
	second, err := client.FetchConditional(context.Background(), raw, first.Validators)
	if err != nil || !second.NotModified || second.Validators != first.Validators || second.Feed.Items != nil {
		t.Fatal(second, err)
	}
}
func TestConditionalRedirectScopesValidatorsAndKeepsSharedPolicy(t *testing.T) {
	original := "https://example.com/start"
	final := "https://cdn.example.com/feed"
	checks := 0
	httpClient := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { checks++; return nil }, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("cookie leaked")
		}
		if r.URL.String() == original {
			if r.Header.Get("If-None-Match") != "" {
				t.Fatal("validator leaked to original resource")
			}
			return response(r, 302, http.Header{"Location": {final}}, ""), nil
		}
		if r.URL.String() != final || r.Header.Get("If-None-Match") != `"final"` {
			t.Fatal("wrong final validators")
		}
		return response(r, 304, http.Header{}, ""), nil
	})}
	client := &Client{HTTP: httpClient}
	result, err := client.FetchConditional(context.Background(), original, Validators{ETag: `"final"`, ResourceURL: final})
	if err != nil || !result.NotModified || checks != 1 {
		t.Fatal(result, err, checks)
	}
	// Changing the target removes even same-host validators.
	client.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/feed" {
			return response(r, 302, http.Header{"Location": {"https://cdn.example.com/changed"}}, ""), nil
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Fatal("validator leaked across paths")
		}
		return response(r, 200, http.Header{}, fixtureFeed), nil
	})}
	changed, err := client.FetchConditional(context.Background(), final, result.Validators)
	if err != nil || changed.Validators.ETag != "" || changed.Validators.ResourceURL != "https://cdn.example.com/changed" {
		t.Fatal(changed, err)
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("blocked fixture redirect") }
	if _, err = (&Client{HTTP: httpClient}).FetchConditional(context.Background(), original, Validators{}); err == nil {
		t.Fatal("redirect policy lost")
	}
}

func TestRedirectLimitAndAnonymousRequestsSurviveCustomPolicy(t *testing.T) {
	requests, checks := 0, 0
	client := &Client{HTTP: &http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			checks++
			r.Header.Set("Authorization", "Bearer private-fixture")
			r.Header.Set("Cookie", "session=private-fixture")
			r.Header.Set("Referer", "https://feeds.example/?key=private-fixture")
			return nil
		},
		Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
				t.Fatal("custom redirect policy leaked credentials")
			}
			if requests > 5 {
				t.Fatal("unbounded redirects")
			}
			return response(r, 302, http.Header{"Location": {"https://feeds.example/loop"}}, ""), nil
		}),
	}}
	_, err := client.FetchConditional(context.Background(), "https://feeds.example/start", Validators{})
	if err == nil || requests != 5 || checks != 4 || strings.Contains(err.Error(), "private-fixture") {
		t.Fatal("redirect limit not enforced", requests, checks)
	}
}

func TestRedirectDoesNotExposePrivateFeedURLThroughAutomaticReferer(t *testing.T) {
	for _, target := range []string{"https://feeds.example/target", "https://other.example/target"} {
		calls := 0
		client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if r.URL.Query().Get("key") != "private-query-key" {
					t.Fatal("source key removed")
				}
				return response(r, 302, http.Header{"Location": {target}}, ""), nil
			}
			if r.URL.String() != target || r.Header.Get("Referer") != "" {
				t.Fatal("private source URL leaked through redirect")
			}
			return response(r, 200, http.Header{}, fixtureFeed), nil
		})}}
		result, err := client.FetchConditional(context.Background(), "https://feeds.example/start?key=private-query-key", Validators{})
		if err != nil || calls != 2 || len(result.Feed.Items) != 1 {
			t.Fatal("redirected feed unavailable", err, calls)
		}
	}
}
func TestConditionalInvalidHeadersRetryAndBoundedBody(t *testing.T) {
	raw := "https://example.com/feed"
	for _, tag := range []string{"plain", `"bad space"`, `"bad"quote"`, "\"bad\r\nvalue\"", strings.Repeat("x", 1025)} {
		if cleanETag(tag) != "" {
			t.Fatal("unsafe ETag accepted", tag)
		}
	}
	for _, tag := range []string{`""`, `"one"`, `W/"one"`} {
		if cleanETag(tag) != tag {
			t.Fatal("valid ETag rejected")
		}
	}
	if cleanModified("not a date") != "" {
		t.Fatal("invalid modified date")
	}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for raw, want := range map[string]int64{"120": 120, "-1": 0, "999999999": 604800, "invalid": 0, now.Add(120 * time.Second).Format(http.TimeFormat): 121} {
		if got := retryAfter(raw, now); got != want {
			t.Fatal(raw, got, want)
		}
	}
	for _, scenario := range []struct {
		status  int
		headers http.Header
		body    string
		wait    int64
	}{{304, http.Header{}, "", 0}, {429, http.Header{"Retry-After": {"7200"}}, "", 7200}, {503, http.Header{"Retry-After": {"60"}}, "", 60}, {200, http.Header{}, strings.Repeat("x", maxFeedBytes+1), 0}} {
		c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return response(r, scenario.status, scenario.headers, scenario.body), nil
		})}}
		_, err := c.FetchConditional(context.Background(), raw, Validators{ETag: "invalid", LastModified: "invalid", ResourceURL: raw})
		var failure *FetchError
		if !errors.As(err, &failure) || failure.RetryAfterSeconds != scenario.wait {
			t.Fatal(scenario.status, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}}
	if _, err := c.FetchConditional(ctx, raw, Validators{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation hidden", err)
	}
}

func TestFullTextFeedAboveFourMegabytesRetainsEveryArticleEnding(t *testing.T) {
	body := strings.Repeat("完整正文段落。", 28000) + "正文末尾"
	entry := `<item><guid>%d</guid><title>Article</title><link>https://example.com/article</link><description><![CDATA[<p>` + body + `</p>]]></description></item>`
	var raw strings.Builder
	raw.WriteString(`<rss version="2.0"><channel><title>全文源</title><link>https://example.com</link><description>feed</description>`)
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&raw, entry, i)
	}
	raw.WriteString(`</channel></rss>`)
	if raw.Len() <= 4<<20 || raw.Len() > maxFeedBytes {
		t.Fatal("fixture outside intended size")
	}
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, http.Header{}, raw.String()), nil
	})}}
	result, err := client.FetchConditional(context.Background(), "https://example.com/fulltext", Validators{})
	if err != nil || len(result.Feed.Items) != 8 {
		t.Fatal("full-text feed unavailable", err)
	}
	for _, item := range result.Feed.Items {
		if !strings.HasSuffix(item.Description, "正文末尾") {
			t.Fatal("article ending lost")
		}
	}
}

func TestBodyCancellationAndInterruptionDoNotParseOrExposeErrors(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		parsed := false
		client := &Client{Parser: func([]byte, string) (discovery.Feed, error) {
			parsed = true
			return discovery.Feed{}, nil
		}}
		res := &http.Response{Body: bodyReader(func([]byte) (int, error) {
			if canceled {
				cancel()
			}
			return 0, errors.New("private-body-key")
		})}
		_, err := client.parseResponse(ctx, res, "https://example.com/feed")
		cancel()
		if parsed || err == nil || strings.Contains(err.Error(), "private-body-key") {
			t.Fatal("unsafe parsing or diagnostic")
		}
		if canceled && !errors.Is(err, context.Canceled) {
			t.Fatal("body cancellation hidden", err)
		}
		if !canceled && !strings.Contains(err.Error(), "读取中断") {
			t.Fatal("interruption mislabeled", err)
		}
	}
}
