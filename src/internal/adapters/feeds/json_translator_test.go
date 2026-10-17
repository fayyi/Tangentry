package feeds

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestJSONFeedConditionalRefresh(t *testing.T) {
	client := &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.Header.Get("Accept"), "application/feed+json") {
			t.Error("JSON feed not accepted")
		}
		if r.Header.Get("If-None-Match") == `"json-v1"` {
			return response(r, 304, http.Header{}, ""), nil
		}
		return response(r, 200, http.Header{"Content-Type": {"application/feed+json"}, "Etag": {`"json-v1"`}}, `{"version":"https://jsonfeed.org/version/1.1","title":"JSON subscription","items":[{"id":"1","url":"https://example.com/story","title":"Story","content_text":"Complete plain text."}]}`), nil
	})}}
	first, err := client.FetchConditional(context.Background(), "https://example.com/feed.json", Validators{})
	if err != nil || len(first.Feed.Items) != 1 || first.Feed.Items[0].Description != "Complete plain text." {
		t.Fatal(first, err)
	}
	same, err := client.FetchConditional(context.Background(), "https://example.com/feed.json", first.Validators)
	if err != nil || !same.NotModified || same.Validators != first.Validators {
		t.Fatal(same, err)
	}
}

func TestJSONFeedPreservesPlainTextAuthorsAndMediaDuration(t *testing.T) {
	data := []byte(`{"version":"https://jsonfeed.org/version/1.1","title":"Feed <notes>","authors":[{"name":"Feed <author>","avatar":"https://example.com/feed-avatar.png"}],"items":[
	{"id":"article","url":"https://example.com/story","title":"Literal <title>","content_text":"Use <tag> literally.\n\nKeep the second paragraph.","date_published":"2026-10-12T10:00:00Z"},
	{"id":"podcast","url":"https://example.com/episode","title":"Episode","content_html":"<p>The full episode description.</p>","authors":[{"name":"Episode author","avatar":"https://example.com/episode-avatar.png"}],"author":{"name":"Deprecated author"},"attachments":[{"url":"https://example.com/cover.jpg","mime_type":"image/jpeg","duration_in_seconds":123},{"url":"https://example.com/episode.mp3","mime_type":"audio/mpeg","duration_in_seconds":240,"size_in_bytes":99999}]},
	{"url":"https://example.com/missing-id","content_text":"Missing identities must not be invented."},
	{"id":"unsafe","url":"https://example.com/unsafe","title":"Unsafe avatar","content_text":"The text is still readable.","authors":[{"name":"Safe author","avatar":"javascript:alert(1)"}]}
	]}`)
	feed, err := Parse(data, "https://example.com/feed.json")
	if err != nil || len(feed.Items) != 3 {
		t.Fatal(feed, err)
	}
	article, episode, unsafe := feed.Items[0], feed.Items[1], feed.Items[2]
	if feed.SourceTitle != "Feed <notes>" || article.Title != "Literal <title>" || article.Description != "Use <tag> literally.\n\nKeep the second paragraph." || article.Author != "Feed <author>" || article.AuthorAvatarURL != "https://example.com/feed-avatar.png" || article.PublishedAt != 1791799200 {
		t.Fatal("lost plain text, date or inherited author", feed)
	}
	if episode.Kind != "audio" || episode.DurationSeconds != 240 || episode.MediaURL != "https://example.com/episode.mp3" || episode.Author != "Episode author" || episode.AuthorAvatarURL != "https://example.com/episode-avatar.png" || strings.Contains(episode.Description, "<p>") {
		t.Fatal("wrong attachment or author metadata", episode)
	}
	if unsafe.AuthorAvatarURL != "" {
		t.Fatal("unsafe avatar retained", unsafe)
	}
	changed, err := Parse([]byte(strings.Replace(string(data), "Literal <title>", "Changed title", 1)), "https://example.com/feed.json")
	if err != nil || changed.Items[0].ID != article.ID {
		t.Fatal("title update changed stable identity", err)
	}
}

func TestJSONFeedLegacyAuthorAndLiteralSummary(t *testing.T) {
	feed, err := Parse([]byte(`{"version":"https://jsonfeed.org/version/1","title":"Legacy","author":{"name":"Legacy author","avatar":"https://example.com/avatar.png"},"items":[{"id":"1","url":"https://example.com/1","title":"Summary","summary":"Literal <summary> text","content_text":""}]}`), "https://example.com/feed.json")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Author != "Legacy author" || feed.Items[0].Description != "Literal <summary> text" || feed.Items[0].AuthorAvatarURL == "" {
		t.Fatal(feed, err)
	}
}
