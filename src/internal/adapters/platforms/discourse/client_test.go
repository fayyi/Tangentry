package discourse

import (
	"context"
	"embed"
	"encoding/json"
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
	if err != nil || item.Author != "作者" || item.ReplyCount != 1 || !strings.Contains(item.Description, "正文") || strings.Contains(item.Description, "回复 回复者：补充") || item.Discussion == nil || len(item.Discussion.Posts) != 2 {
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
	raw := `{"id":42,"title":"图片","posts_count":1,"post_stream":{"stream":[99],"posts":[{"id":99,"post_number":1,"username":"作者","cooked":"<p>正文</p><p><img class=\"lightbox\" data-src=\"//cdn.example.test/a.jpg\"><img src=\"upload://short-key\"></p>"}]}}`
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

func TestTopicParsesReplyRelationsAndStreamIDs(t *testing.T) {
	raw := `{"id":"42","title":"楼层","reply_count":"3","posts_count":4,"category_name":null,"actions_summary":[{"id":2,"count":"4"}],"link_counts":[{"url":"https://example.test/","clicks":1}],"polls":{},"reactions":{"heart":{"count":1}},"bookmarked":true,"user_title":null,"flair_name":42,"post_stream":{"stream":[201,202,203,204],"posts":[{"id":201,"post_number":1,"username":"楼主","cooked":"<p>主帖</p>","reactions":null},{"id":"202","post_number":"2","username":"回复者","reply_to_post_number":1,"reply_to_user":{"username":"楼主","name":"楼主名称","avatar_template":"/user_avatar/linux.do/a/64/1.png"},"reply_to_post_id":"201","like_count":"4","cooked":"<p>回复</p>","user_title":{"name":"版主"},"flair_color":null},{"id":"203","post_number":"3","username":"第三层","reply_to_user":null,"reply_to_post_number":null,"reply_to_post_id":null,"like_count":null,"cooked":"<p>第三层</p>","actions_summary":null},{"id":"204","post_number":"4","username":"第四层","reply_to_user":{"name":"仅显示名称"},"reply_to_post_number":{"unexpected":true},"reply_to_post_id":{},"like_count":{"count":9},"cooked":"<p>第四层</p>"}]}}`
	c := &Client{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { return response(r, 200, raw), nil })}}
	item, err := c.Topic(context.Background(), "42")
	if err != nil || item.Discussion == nil || item.Discussion.TotalPosts != 4 || len(item.Discussion.PostIDs) != 4 || len(item.Discussion.Posts) != 4 {
		t.Fatalf("discussion metadata lost: %+v, %v", item.Discussion, err)
	}
	reply := item.Discussion.Posts[1]
	if reply.ReplyToNumber != 1 || reply.ReplyToUser != "楼主" || reply.ReplyToPostID != 201 || reply.LikeCount != 4 {
		t.Fatalf("reply relation lost: %+v", reply)
	}
	if got := item.Discussion.Posts[2]; got.ReplyToUser != "" || got.LikeCount != 0 || got.ID != 203 || got.Number != 3 {
		t.Fatalf("null and mixed scalar fields were not tolerated: %+v", got)
	}
	if got := item.Discussion.Posts[3]; got.ReplyToUser != "仅显示名称" || got.ReplyToNumber != 0 || got.ReplyToPostID != 0 || got.LikeCount != 0 {
		t.Fatalf("unexpected optional field types were not isolated: %+v", got)
	}
}

func TestDiscourseReplyUserAcceptsStringObjectAndNull(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string", raw: `"旧版用户名"`, want: "旧版用户名"},
		{name: "object username", raw: `{"username":"用户名","name":"显示名"}`, want: "用户名"},
		{name: "object name fallback", raw: `{"name":"显示名"}`, want: "显示名"},
		{name: "null", raw: `null`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := discourseReplyUser(json.RawMessage(test.raw)); got != test.want {
				t.Fatalf("reply user = %q, want %q", got, test.want)
			}
		})
	}
}
