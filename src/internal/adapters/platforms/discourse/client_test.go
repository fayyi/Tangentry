package discourse

import (
	"context"
	"embed"
	"net/http"
	"strings"
	"testing"
)

// JSON fixtures mirror the public Discourse response shapes with identifiers
// and text deliberately kept synthetic.
//
//go:embed testdata/*.json
var discourseFixtures embed.FS

func TestLatestTopics(t *testing.T) {
	raw, err := discourseFixtures.ReadFile("testdata/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	feed, err := Parse(raw, 1)
	if err != nil || len(feed.Items) != 1 || !feed.HasMore || feed.Items[0].ID != "linuxdo:42" || feed.Items[0].PublishedAt <= 0 || feed.Items[0].URL != "https://linux.do/t/42" || feed.Items[0].Category != "技术交流" || feed.Items[0].ReplyCount != 7 || feed.Items[0].ViewCount != 99 || feed.Items[0].LikeCount != 3 || feed.Items[0].Description != "摘要" {
		t.Fatalf("invalid topic mapping: %+v %v", feed, err)
	}
	for _, raw := range []string{`{}`, `<html>challenge</html>`, `{"topic_list":null}`} {
		if _, err := Parse([]byte(raw), 1); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestTopicIncludesFirstPostAndReplies(t *testing.T) {
	raw, err := discourseFixtures.ReadFile("testdata/topic.json")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, string(raw)), nil
	})}}
	item, err := c.Topic(context.Background(), "42")
	if err != nil || item.Author != "作者" || item.ReplyCount != 1 || !strings.Contains(item.Description, "正文") || !strings.Contains(item.Description, "回复 回复者：补充") || len(item.ArticleBlocks) != 1 {
		t.Fatalf("topic detail not mapped: %+v %v", item, err)
	}
}

func TestReadUsesStoredBrowserUserAgent(t *testing.T) {
	c := &Client{
		HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("User-Agent"); got != "Mozilla/5.0 (fixture)" {
				t.Fatalf("User-Agent = %q", got)
			}
			return response(r, 200, `{"user":null}`), nil
		})},
		UserAgent: func(string) string { return "Mozilla/5.0 (fixture)" },
	}
	if _, err := c.CurrentSession(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTopicNormalizesCookedImageURLs(t *testing.T) {
	raw := `{"id":42,"title":"图片","post_stream":{"posts":[{"post_number":1,"username":"作者","cooked":"<p>正文</p><p><img class=\"lightbox\" data-src=\"//cdn.example.test/a.jpg\"><img src=\"upload://short-key\"></p>"}]}}`
	c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 200, raw), nil
	})}}
	item, err := c.Topic(context.Background(), "42")
	if err != nil || len(item.ArticleBlocks) != 3 {
		t.Fatalf("topic blocks = %#v, err=%v", item.ArticleBlocks, err)
	}
	if item.ArticleBlocks[1].URL != "https://cdn.example.test/a.jpg" || item.ArticleBlocks[2].URL != "https://linux.do/uploads/short-url/short-key" {
		t.Fatalf("image URLs were not normalized: %#v", item.ArticleBlocks)
	}
}
