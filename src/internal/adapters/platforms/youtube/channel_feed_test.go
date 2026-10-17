package youtube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"tangentry/internal/adapters/feeds"
	"tangentry/internal/discovery"
	"tangentry/internal/netpolicy"
	"testing"
)

const fixtureChannel = "UCaaaaaaaaaaaaaaaaaaaaaa"
const fixtureEntry = `<entry><id>yt:video:abcdefghijk</id><yt:videoId>abcdefghijk</yt:videoId><yt:channelId>UCaaaaaaaaaaaaaaaaaaaaaa</yt:channelId><title>视频 &amp; 阅读</title><link rel="alternate" href="https://www.youtube.com/watch?v=abcdefghijk"/><author><name>测试作者</name><uri>https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa</uri></author><published>2026-10-01T12:00:00+00:00</published><media:group><media:thumbnail url="https://i1.ytimg.com/vi/abcdefghijk/hqdefault.jpg"/><media:description><![CDATA[<p>正文</p><script>secret</script>]]></media:description><media:content url="https://www.youtube.com/v/abcdefghijk" type="application/x-shockwave-flash"/></media:group></entry>`

func fixture(body string) string {
	return `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/"><yt:channelId>aaaaaaaaaaaaaaaaaaaaaa</yt:channelId><title>测试频道</title><author><name>测试作者</name><uri>https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa</uri></author>` + body + `</feed>`
}
func TestChannelFeedUsesVideoIdentitiesAndConfirmsOwnership(t *testing.T) {
	got, err := Parse([]byte(fixture(fixtureEntry)), discovery.YouTubeChannelFeed(fixtureChannel))
	if err != nil || len(got.Items) != 1 || got.Query != fixtureChannel || got.SourceTitle != "测试频道" {
		t.Fatal(got, err)
	}
	item := got.Items[0]
	if item.ID != "youtube:abcdefghijk" || item.Platform != "youtube" || item.Kind != "video" || item.AuthorID != fixtureChannel || item.Title != "视频 & 阅读" || item.PublishedAt == 0 || item.DurationSeconds != 0 || item.MediaURL != "" || item.Description != "正文" {
		t.Fatal("bad video mapping", item)
	}
	empty, err := Parse([]byte(fixture("")), discovery.YouTubeChannelFeed(fixtureChannel))
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("valid empty channel rejected", err)
	}
	fullID := strings.Replace(fixture(fixtureEntry), "<yt:channelId>aaaaaaaaaaaaaaaaaaaaaa", "<yt:channelId>"+fixtureChannel, 1)
	if _, err = Parse([]byte(fullID), discovery.YouTubeChannelFeed(fixtureChannel)); err != nil {
		t.Fatal("full channel ID rejected", err)
	}
}
func TestChannelFeedRejectsPartialWrongAndUnsafeResults(t *testing.T) {
	valid := fixture(fixtureEntry)
	for name, raw := range map[string]string{
		"other-channel":         strings.Replace(valid, "aaaaaaaaaaaaaaaaaaaaaa</yt:channelId>", "bbbbbbbbbbbbbbbbbbbbbb</yt:channelId>", 1),
		"other-author":          strings.Replace(valid, "https://www.youtube.com/channel/"+fixtureChannel, "https://www.youtube.com/channel/UCbbbbbbbbbbbbbbbbbbbbbb", 1),
		"missing-id":            strings.Replace(valid, "<yt:videoId>abcdefghijk</yt:videoId>", "", 1),
		"video-id-mismatch":     strings.Replace(valid, "<id>yt:video:abcdefghijk</id>", "<id>yt:video:different11</id>", 1),
		"link-mismatch":         strings.Replace(valid, "watch?v=abcdefghijk", "watch?v=different11", 1),
		"missing-link":          strings.Replace(valid, `<link rel="alternate" href="https://www.youtube.com/watch?v=abcdefghijk"/>`, "", 1),
		"invalid-date":          strings.Replace(valid, "2026-10-01T12:00:00+00:00", "invalid", 1),
		"unsafe-thumbnail":      strings.Replace(valid, "https://i1.ytimg.com/vi/abcdefghijk/hqdefault.jpg", "http://127.0.0.1/private", 1),
		"lookalike-thumbnail":   strings.Replace(valid, "i1.ytimg.com", "i1.ytimg.com.evil.example", 1),
		"duplicate":             fixture(fixtureEntry + fixtureEntry),
		"oversized-count":       fixture(strings.Repeat(fixtureEntry, 101)),
		"truncated":             strings.TrimSuffix(valid, "</feed>"),
		"extra-document":        valid + `<feed/>`,
		"non-feed":              `<html>consent page</html>`,
		"wrong-namespace":       strings.Replace(valid, "http://www.w3.org/2005/Atom", "https://evil.example/Atom", 1),
		"wrong-entry-namespace": strings.Replace(valid, "<entry>", `<entry xmlns="https://evil.example/Atom">`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw), discovery.YouTubeChannelFeed(fixtureChannel)); err == nil {
				t.Fatal("invalid feed accepted")
			}
		})
	}
	for _, url := range []string{"https://example.com/feed", "https://www.youtube.com/@name", discovery.YouTubeChannelFeed(fixtureChannel) + "&x=1"} {
		if _, err := Parse([]byte(valid), url); err == nil {
			t.Fatal("wrong resource accepted")
		}
	}
}

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestChannelConditionalClientUsesSharedBoundedFetcher(t *testing.T) {
	calls := 0
	c := New(&http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != discovery.YouTubeChannelFeed(fixtureChannel) || r.Header.Get("Cookie") != "" {
			t.Fatal("wrong request")
		}
		status, body := 200, fixture(fixtureEntry)
		if calls == 2 {
			if r.Header.Get("If-None-Match") != `"channel-one"` {
				t.Fatal("missing cache validator")
			}
			status, body = 304, ""
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Etag": {`"channel-one"`}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	first, err := c.FetchConditional(context.Background(), fixtureChannel, feeds.Validators{})
	if err != nil || len(first.Feed.Items) != 1 {
		t.Fatal(first, err)
	}
	same, err := c.FetchConditional(context.Background(), fixtureChannel, first.Validators)
	if err != nil || !same.NotModified || same.Validators != first.Validators {
		t.Fatal(same, err)
	}
	if _, err = c.FetchConditional(context.Background(), "invalid", feeds.Validators{}); err == nil || calls != 2 {
		t.Fatal("invalid channel fetched")
	}
}

// Opt-in anonymous smoke test: no cookies, source writes, player requests or downloads.
func TestRealPublicYouTubeChannelFeed(t *testing.T) {
	if os.Getenv("TANGENTRY_YOUTUBE_CHANNEL_SMOKE") != "1" {
		t.Skip("explicit public-feed smoke test")
	}
	ctx := context.Background()
	c := New(netpolicy.NewClient())
	result, err := c.FetchConditional(ctx, "UCXuqSBlHAE6Xw-yeJA0Tunw", feeds.Validators{})
	if err != nil || len(result.Feed.Items) == 0 {
		t.Fatalf("public feed unavailable: %v", err)
	}
	fmt.Printf("Public YouTube channel: %s, %d videos, canonical identity %s\n", result.Feed.SourceTitle, len(result.Feed.Items), result.Feed.Query)
}
