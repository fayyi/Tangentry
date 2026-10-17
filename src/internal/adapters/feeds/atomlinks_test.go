package feeds

import (
	"testing"
)

func TestAtomReadingLinkSelection(t *testing.T) {
	for _, test := range []struct {
		name, links, want string
	}{
		{"html after api", `<link rel="alternate" type="application/json" href="data.json"/><link rel="alternate" type="text/html" href="article"/>`, "https://example.com/posts/article"},
		{"html after unspecified", `<link href="unknown"/><link type="text/html; charset=utf-8" href="article"/>`, "https://example.com/posts/article"},
		{"xhtml", `<link type="application/pdf" href="paper.pdf"/><link type="application/xhtml+xml" href="article"/>`, "https://example.com/posts/article"},
		{"unsafe first", `<link type="text/html" href="javascript:alert(1)"/><link type="text/html" href="article"/>`, "https://example.com/posts/article"},
		{"unknown before api", `<link type="application/json" href="data.json"/><link href="article"/>`, "https://example.com/posts/article"},
		{"stable equal priority", `<link type="text/html" href="first?access=article-token"/><link type="text/html" href="second"/>`, "https://example.com/posts/first?access=article-token"},
		{"explicit nonhtml fallback", `<link type="application/pdf" href="paper.pdf"/>`, "https://example.com/posts/paper.pdf"},
		{"nonreading links ignored", `<link rel="replies" type="text/html" href="comments"/><link rel="self" type="text/html" href="entry"/><link rel="enclosure" type="image/jpeg" href="cover.jpg"/><link type="application/json" href="data.json"/><link type="text/html" href="article"/>`, "https://example.com/posts/article"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := `<feed xmlns="http://www.w3.org/2005/Atom" xml:base="https://example.com/"><id>urn:feed</id><title>feed</title><entry xml:base="posts/"><id>urn:entry:one</id><title>article</title>` + test.links + `<summary>正文摘要</summary></entry></feed>`
			canonical := "https://example.com/private-feed?key=feed-token"
			result, err := Parse([]byte(raw), canonical)
			if err != nil || len(result.Items) != 1 {
				t.Fatalf("entry lost: %+v, %v", result, err)
			}
			item := result.Items[0]
			if item.URL != test.want || item.ID != "rss:"+Hash(canonical+"\nurn:entry:one") || item.Description != "正文摘要" {
				t.Fatalf("reading URL, identity or summary changed: %+v", item)
			}
			if test.name == "nonreading links ignored" && item.MediaURL != "https://example.com/posts/cover.jpg" {
				t.Fatalf("attachment lost: %+v", item)
			}
		})
	}
}

func TestAtomDoesNotUseCommentsAsMissingArticleLink(t *testing.T) {
	for _, links := range []string{
		`<link rel="replies" type="text/html" href="https://example.com/comments"/><link rel="self" href="https://example.com/entry"/>`,
		`<link type="text/html" href="javascript:alert(1)"/><link rel="replies" type="text/html" href="https://example.com/comments"/>`,
	} {
		raw := `<feed xmlns="http://www.w3.org/2005/Atom"><id>urn:feed</id><title>feed</title><entry><id>urn:entry:one</id><title>article</title>` + links + `</entry></feed>`
		result, err := Parse([]byte(raw), "https://example.com/private-feed?key=feed-token")
		if err != nil || len(result.Items) != 0 {
			t.Fatalf("unreadable entry became a comments link: %+v, %v", result, err)
		}
	}
}
