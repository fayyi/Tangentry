package feeds

import (
	"strings"
	"testing"
	"unicode/utf8"

	"tangentry/internal/content"
)

func TestRSSUnusableEncodedContentFallsBackToDescription(t *testing.T) {
	for _, encoded := range []string{"", "  \n ", "<p></p>", "<script>hidden</script>", "<a href='/comments'>Comments</a>"} {
		raw := `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>one</guid><title>article</title><link>https://example.com/one</link><content:encoded><![CDATA[` + encoded + `]]></content:encoded><description><![CDATA[<p>订阅源提供的有效正文与摘要。</p>]]></description></item></channel></rss>`
		feed, err := Parse([]byte(raw), "https://example.com/feed")
		if err != nil || len(feed.Items) != 1 || feed.Items[0].Description != "订阅源提供的有效正文与摘要。" {
			t.Fatalf("unusable encoded content %q hid description: %+v, %v", encoded, feed, err)
		}
	}
	raw := `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>one</guid><title>article</title><link>https://example.com/one</link><content:encoded><![CDATA[<p>完整正文优先。</p>]]></content:encoded><description>较短摘要</description></item></channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Description != "完整正文优先。" {
		t.Fatalf("useful full body lost priority: %+v, %v", feed, err)
	}
}

func TestFeedTextPreservesBlockAndSelfClosingBreaks(t *testing.T) {
	raw := `<h2>标题</h2><div>第一行<br/>第二行<br>第三行</div><blockquote>引用</blockquote><pre>  code\nnext</pre><section>末尾 <strong>强调</strong></section><style>hidden</style><script>hidden</script>`
	text := Text(raw)
	for _, joined := range []string{"标题第一行", "第一行第二行", "第二行第三行", "第三行引用", "引用  code", "next末尾"} {
		if strings.Contains(text, joined) {
			t.Fatalf("block boundary lost: %q in %q", joined, text)
		}
	}
	if !strings.Contains(text, "第一行\n第二行\n第三行") || !strings.Contains(text, "末尾 强调") || strings.Contains(text, "hidden") {
		t.Fatalf("unexpected feed text: %q", text)
	}
	feed, err := Parse([]byte(`<rss version="2.0"><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>blocks</guid><title>blocks</title><link>https://example.com/blocks</link><description><![CDATA[`+raw+`]]></description></item></channel></rss>`), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Description != text {
		t.Fatalf("structured feed text not retained: %+v, %v", feed, err)
	}
}

func TestAtomArticleImagesAndStructureArePreservedSafely(t *testing.T) {
	body := `<p>封面说明。</p><p><img src="https://cdn.example.com/cover.webp" alt="封面" width="800"/></p><h2>正文</h2><p>前文<img data-src="/media/body.jpg" alt="正文配图"/>后文</p><img src="javascript:alert(1)"/><script><img src="https://evil.example/x.png"/></script>`
	raw := `<feed xmlns="http://www.w3.org/2005/Atom"><title>feed</title><id>urn:feed</id><entry><id>urn:entry</id><title>含图文章</title><link href="https://example.com/story" type="text/html"/><content type="html"><![CDATA[` + body + `]]></content></entry></feed>`
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("cannot parse image article: %+v, %v", feed, err)
	}
	item := feed.Items[0]
	if item.ThumbnailURL != "https://cdn.example.com/cover.webp" || len(item.ArticleBlocks) != 6 {
		t.Fatalf("article image blocks missing or unsafe image accepted: %+v", item.ArticleBlocks)
	}
	if item.ArticleBlocks[1].Kind != "image" || item.ArticleBlocks[1].Alt != "封面" || item.ArticleBlocks[1].Width != 800 || item.ArticleBlocks[4].URL != "https://example.com/media/body.jpg" {
		t.Fatalf("image order, base resolution or attributes lost: %+v", item.ArticleBlocks)
	}
	if item.ArticleBlocks[3].Text != "前文" || item.ArticleBlocks[5].Text != "后文" {
		t.Fatalf("text around image not retained in order: %+v", item.ArticleBlocks)
	}
}

func TestDiscourseCookedStructureKeepsLightboxListsAndInlineEmoji(t *testing.T) {
	body := `<h1><a class="anchor" href="#ignored"></a><strong>脱敏标题</strong></h1>` +
		`<p><div class="lightbox-wrapper"><a class="lightbox" href="https://img.example.test/original.png"><img src="https://img.example.test/thumb.png" alt="示例图" width="640" height="360" srcset="https://img.example.test/thumb.png, https://img.example.test/large.png 2x"><div class="meta"><svg></svg><span class="filename">不应显示</span><span class="informations">640×360</span></div></a></div></p>` +
		`<ul><li><p>一级</p><ul><li><p>二级</p></li></ul></li></ul>` +
		`<p>表情 <img class="emoji" src="https://img.example.test/emoji.png" alt=":wave:"/> 之后</p>` +
		`<aside class="onebox"><a href="https://example.test/post">链接标题</a></aside>` +
		`<aside class="quote"><p>引用内容</p></aside><pre><code>const x = 1</code></pre>`
	blocks := ParseArticleBlocks(body, "https://forum.example.test/t/1")
	if len(blocks) != 8 {
		t.Fatalf("unexpected block count: %#v", blocks)
	}
	if blocks[0].Kind != "heading" || blocks[0].Text != "脱敏标题" {
		t.Fatalf("heading/anchor handling failed: %#v", blocks[0])
	}
	if blocks[1].Kind != "image" || blocks[1].URL != "https://img.example.test/original.png" || blocks[1].Alt != "示例图" || blocks[1].Width != 640 || strings.Contains(blocks[1].Text, "不应显示") {
		t.Fatalf("lightbox handling failed: %#v", blocks[1])
	}
	if blocks[2].Kind != "list" || blocks[2].Level != 0 || blocks[3].Kind != "list" || blocks[3].Level != 1 {
		t.Fatalf("nested list handling failed: %#v", blocks[2:4])
	}
	if len(blocks[4].InlineImages) != 1 || blocks[4].InlineImages[0].Alt != ":wave:" {
		t.Fatalf("inline emoji handling failed: %#v", blocks[4])
	}
	if blocks[5].Kind != "onebox" || blocks[5].URL != "https://example.test/post" || blocks[6].Kind != "quote" || blocks[7].Kind != "code" {
		t.Fatalf("structured block handling failed: %#v", blocks[5:])
	}
}

func TestRSSArticleTableCellsRemainDistinct(t *testing.T) {
	body := `<table><tr><th>项目</th><th>金额</th></tr><tr><td>订阅<strong>A</strong></td><td>12.50</td></tr><tr><td>订阅B</td><td>100</td></tr></table>`
	raw := `<rss version="2.0"><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>table</guid><title>table</title><link>https://example.com/table</link><description><![CDATA[` + body + `]]></description></item></channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("cannot parse table fixture: %+v, %v", feed, err)
	}
	text := feed.Items[0].Description
	for _, row := range []string{"项目\t金额", "订阅A\t12.50", "订阅B\t100"} {
		if !strings.Contains(text, row) {
			t.Fatalf("cell separation missing: %q in %q", row, text)
		}
	}
	if strings.Contains(text, "12.50订阅B") || strings.Contains(text, "项目金额") {
		t.Fatalf("table contents merged: %q", text)
	}
}

func TestFeedXMLBaseSupportedLinksAndIdentity(t *testing.T) {
	for name, raw := range map[string]string{
		"rss":  `<rss version="2.0" xml:base="https://example.com/feeds/"><channel><title>base feed</title><link>https://example.com</link><description>feed</description><image><url>../cover.jpg</url></image><item xml:base="episodes/"><guid>episode-one</guid><title>episode</title><link>one</link><enclosure url="one.mp3" type="audio/mpeg"/></item><item><guid>unsafe</guid><title>unsafe</title><link>javascript:alert(1)</link></item></channel></rss>`,
		"atom": `<feed xmlns="http://www.w3.org/2005/Atom" xml:base="https://example.com/feeds/"><title>base feed</title><id>urn:feed</id><updated>2026-10-01T00:00:00Z</updated><logo>../cover.jpg</logo><entry xml:base="episodes/"><id>episode-one</id><title>episode</title><updated>2026-10-01T00:00:00Z</updated><link href="one"/><link rel="enclosure" href="one.mp3" type="audio/mpeg"/></entry><entry><id>unsafe</id><title>unsafe</title><link href="javascript:alert(1)"/></entry></feed>`,
	} {
		t.Run(name, func(t *testing.T) {
			canonical := "https://example.com/private-feed?key=fixture"
			feed, err := Parse([]byte(raw), canonical)
			if err != nil || len(feed.Items) != 1 {
				t.Fatalf("relative feed not retained or unsafe entry accepted: %+v, %v", feed, err)
			}
			item := feed.Items[0]
			if item.URL != "https://example.com/feeds/episodes/one" || item.ThumbnailURL != "https://example.com/cover.jpg" {
				t.Fatalf("xml base inheritance lost: %+v", item)
			}
			identity := "episode-one"
			if name == "atom" {
				identity = "https://example.com/feeds/episodes/episode-one"
				if item.MediaURL != "https://example.com/feeds/episodes/one.mp3" || item.Kind != "audio" {
					t.Fatalf("Atom enclosure lost: %+v", item)
				}
			} else if item.MediaURL != "https://example.com/feeds/episodes/one.mp3" || item.Kind != "audio" {
				t.Fatalf("relative RSS enclosure lost: %+v", item)
			}
			if item.ID != "rss:"+Hash(canonical+"\n"+identity) || strings.Contains(item.URL+item.MediaURL+item.ThumbnailURL, "key=") {
				t.Fatalf("identity changed or feed key copied to content: %+v", item)
			}
		})
	}
}

func TestRSSEnclosureBaseOverridesAndSafety(t *testing.T) {
	for _, test := range []struct{ base, attachment, want string }{
		{`xml:base="https://example.com/base/"`, `<enclosure xml:base="../audio/" url="one.mp3" type="audio/mpeg"/>`, "https://example.com/audio/one.mp3"},
		{"", `<enclosure url="one.mp3" type="audio/mpeg"/>`, ""},
		{`xml:base="https://example.com/base/"`, `<enclosure url="javascript:alert(1)" type="audio/mpeg"/>`, ""},
		{`xml:base="https://example.com/base/"`, `<enclosure xml:base="file:///tmp/" url="one.mp3" type="audio/mpeg"/>`, ""},
		{`xml:base="https://example.com/base/"`, `<enclosure url="https://cdn.example.com/one.mp3?token=signed" type="audio/mpeg"/>`, "https://cdn.example.com/one.mp3?token=signed"},
	} {
		raw := `<rss version="2.0" ` + test.base + `><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>first</guid><title>first</title><link>https://example.com/first</link>` + test.attachment + `</item><item><guid>second</guid><title>second</title><link>https://example.com/second</link><enclosure url="two.mp3" type="audio/mpeg"/></item></channel></rss>`
		feed, err := Parse([]byte(raw), "https://example.com/private?key=secret")
		if err != nil || len(feed.Items) != 2 || feed.Items[0].MediaURL != test.want {
			t.Fatalf("base supplement mismatch: %+v, %v", feed, err)
		}
		wantSecond := ""
		if test.base != "" {
			wantSecond = "https://example.com/base/two.mp3"
		}
		if feed.Items[1].MediaURL != wantSecond {
			t.Fatalf("sibling base leaked: %+v", feed.Items[1])
		}
	}
}

func TestRSSArticleKeepsLongFeedBodyAndUTF8Bounds(t *testing.T) {
	body := strings.Repeat("完整正文与中文段落。", 8000) + "文章末尾标记"
	raw := `<rss version="2.0"><channel><title>feed</title><link>https://example.com</link><description>feed</description><item><guid>long</guid><title>long article</title><link>https://example.com/a</link><description><![CDATA[<p>` + body + `</p><script>private script</script>]]></description></item></channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Description != body || content.Validate(feed.Items[0]) != nil {
		t.Fatal("long RSS body lost", err)
	}
	bounded := textLimit(strings.Repeat("正文😀", 200000), 1<<20)
	if !utf8.ValidString(bounded) || len(bounded) > 1<<20 || len(bounded) < (1<<20)-4 {
		t.Fatal("invalid UTF-8 boundary", len(bounded))
	}
	item := feed.Items[0]
	item.Description = strings.Repeat("x", (1<<20)+1)
	if content.Validate(item) == nil {
		t.Fatal("unbounded RSS description")
	}
	item = feed.Items[0]
	item.Kind = "audio"
	if content.Validate(item) == nil {
		t.Fatal("non-article description bound changed")
	}
}

func TestFeedsKeepMediaTypesAndStripActiveHTML(t *testing.T) {
	raw := `<rss version="2.0"><channel><title>测试源</title><link>https://example.com</link><description>feed</description><item><guid>one</guid><title>文章</title><link>https://example.com/a</link><description><![CDATA[<p>正文</p><script>private script</script><style>private style</style>]]></description></item><item><guid>two</guid><title>音频</title><link>https://example.com/b</link><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/></item><item><title>unsafe</title><link>javascript:alert(1)</link></item></channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed")
	if err != nil || len(feed.Items) != 2 {
		t.Fatal(feed, err)
	}
	if feed.Items[0].Kind != "article" || feed.Items[1].Kind != "audio" || feed.Items[1].MediaURL != "https://example.com/audio.mp3" {
		t.Fatal("media mapping", feed)
	}
	if strings.Contains(feed.Items[0].Description, "private") || feed.Items[0].Description != "正文" {
		t.Fatal("active HTML")
	}
	again, _ := Parse([]byte(raw), "https://example.com/feed")
	other, _ := Parse([]byte(raw), "https://example.com/another")
	if feed.Items[0].ID != again.Items[0].ID || feed.Items[0].ID == other.Items[0].ID {
		t.Fatal("unstable identity")
	}
	if _, err := Parse([]byte("<html>not a feed</html>"), "https://example.com"); err == nil {
		t.Fatal("nonfeed accepted")
	}
}

func TestMixedFeedClassifiesEachItemWithoutCallingRSSAMediaType(t *testing.T) {
	raw := `<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>混合订阅</title><link>https://example.com</link><description>feed</description><image><url>https://example.com/feed.jpg</url><title>feed</title><link>https://example.com</link></image>
 <item><guid>podcast</guid><title>播客</title><enclosure url="https://example.com/cover.jpg" type="image/jpeg"/><enclosure url="https://example.com/episode.mp3" type="audio/mpeg; codecs=mp3"/><itunes:duration>01:02:03</itunes:duration><itunes:image href="https://example.com/episode.jpg"/></item>
 <item><guid>video</guid><title>视频</title><link>https://example.com/watch</link><enclosure url="https://example.com/video.mp4" type="video/mp4"/></item>
 <item><guid>document</guid><title>PDF</title><enclosure url="https://example.com/paper.pdf" type="application/pdf"/></item>
 <item><guid>image</guid><title>图片</title><enclosure url="https://example.com/photo.png" type="image/png"/></item>
 <item><guid>unknown</guid><title>未知附件</title><link>https://example.com/article</link><enclosure url="https://example.com/ambiguous.mp3" type="application/octet-stream"/></item>
 <item><guid>unsafe</guid><title>危险附件</title><enclosure url="javascript:alert(1)" type="audio/mpeg"/></item>
 </channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed.xml")
	if err != nil || len(feed.Items) != 5 {
		t.Fatal(feed, err)
	}
	for i, kind := range []string{"audio", "video", "pdf", "image", "article"} {
		if feed.Items[i].Kind != kind || feed.Items[i].Platform != "rss" {
			t.Fatalf("item %d: %+v", i, feed.Items[i])
		}
	}
	episode := feed.Items[0]
	if episode.MediaURL != "https://example.com/episode.mp3" || episode.URL != episode.MediaURL || episode.DurationSeconds != 3723 || episode.ThumbnailURL != "https://example.com/episode.jpg" {
		t.Fatal("podcast enclosure/duration/cover lost", episode)
	}
	if feed.Items[3].ThumbnailURL != feed.Items[3].MediaURL || feed.Items[4].MediaURL != "" {
		t.Fatal("image or unknown mapping", feed)
	}
}

func TestPodcastDurationsAreBounded(t *testing.T) {
	for raw, want := range map[string]int{"123": 123, "02:03": 123, "1:02:03": 3723, "": 0, "-1": 0, "99:99": 0, "1:60:00": 0, "1:2:3:4": 0, "9999999999999999": 0, "168:00:00": 604800, "168:00:01": 0} {
		if got := podcastDuration(raw); got != want {
			t.Errorf("%q: %d, want %d", raw, got, want)
		}
	}
}
