package feeds

import (
	"fmt"
	"strings"
	"tangentry/internal/content"
	"testing"

	"github.com/mmcdole/gofeed"
)

func TestPodcastTranscriptSupplementRejectsIncompleteOrMisalignedXML(t *testing.T) {
	prefix := `<rss xmlns:pi="https://podcastindex.org/namespace/1.0"><channel><item><pi:transcript url="https://example.com/captions.vtt" type="text/vtt"/></item>`
	for name, raw := range map[string]string{
		"truncated":            prefix,
		"invalid trailing XML": prefix + `</channel></rss><broken`,
		"second root":          prefix + `</channel></rss><rss/>`,
		"trailing text":        prefix + `</channel></rss>unexpected`,
		"excessive depth":      prefix + strings.Repeat("<nested>", 257) + strings.Repeat("</nested>", 257) + `</channel></rss>`,
		"extra item":           prefix + `<item/></channel></rss>`,
		"missing item":         `<rss><channel/></rss>`,
	} {
		t.Run(name, func(t *testing.T) {
			feed := &gofeed.Feed{FeedType: "rss", Items: []*gofeed.Item{{}}}
			if got := podcastTranscripts([]byte(raw), feed); got != nil {
				t.Fatal("partial or misaligned metadata accepted", got)
			}
		})
	}
}

func TestPodcastTranscriptRSSOneNamespace(t *testing.T) {
	raw := `<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:pi="https://podcastindex.org/namespace/1.0"><channel rdf:about="https://example.com/feed"><title>Fixture</title><link>https://example.com</link><description>Feed</description></channel><item rdf:about="https://example.com/episode"><title>Episode</title><link>https://example.com/episode</link><pi:transcript url="https://example.com/captions.vtt" type="text/vtt"/></item></rdf:RDF>`
	parsed, err := gofeed.NewParser().ParseString(raw)
	if err != nil || len(parsed.Items) != 1 {
		t.Fatal(parsed, err)
	}
	got := podcastTranscripts([]byte(raw), parsed)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].URL != "https://example.com/captions.vtt" {
		t.Fatal("RSS 1.0 namespace lost", got)
	}
}

func TestPodcastTranscriptNamespaceBindings(t *testing.T) {
	const primary = "https://podcastindex.org/namespace/1.0"
	const alternate = "https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md"
	for name, tag := range map[string]string{
		"alternate prefix":         `<pi:transcript xmlns:pi="` + primary + `" url="https://example.com/captions.vtt" type="text/vtt"/>`,
		"alternate namespace":      `<other:transcript xmlns:other="` + alternate + `" url="https://example.com/captions.vtt" type="text/vtt"/>`,
		"default namespace":        `<transcript xmlns="` + primary + `" url="https://example.com/captions.vtt" type="text/vtt"/>`,
		"two aliases deduplicated": `<a:transcript xmlns:a="` + primary + `" url="https://example.com/captions.vtt" type="text/vtt"/><b:transcript xmlns:b="` + alternate + `" url="https://example.com/captions.vtt" type="text/vtt"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := `<rss version="2.0"><channel><title>Fixture</title><language>zh-CN</language><item><guid>episode</guid><title>Episode</title><link>https://example.com/episode</link><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/>` + tag + `</item></channel></rss>`
			feed, err := Parse([]byte(raw), "https://example.com/feed.xml")
			if err != nil || len(feed.Items) != 1 || len(feed.Items[0].Transcripts) != 1 {
				t.Fatalf("valid namespace lost: %+v, %v", feed, err)
			}
			if feed.Items[0].Transcripts[0].Language != "zh-CN" {
				t.Fatal("feed language lost", feed.Items[0].Transcripts)
			}
		})
	}
}

func TestPodcastTranscriptScopeAndItemAssociation(t *testing.T) {
	raw := `<rss version="2.0" xmlns:podcast="https://podcastindex.org/namespace/1.0"><channel><title>Fixture</title>
<podcast:transcript url="https://example.com/channel.vtt" type="text/vtt"/>
<item><guid>skipped</guid><title>No link</title><podcast:transcript url="https://example.com/skipped.vtt" type="text/vtt"/></item>
<item><guid>one</guid><title>One</title><link>https://example.com/one</link><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/>
<podcast:transcript xmlns:podcast="urn:unrelated" url="https://example.com/wrong.vtt" type="text/vtt"/>
<wrapper><podcast:transcript url="https://example.com/nested.vtt" type="text/vtt"/></wrapper>
<podcast:transcript url="https://example.com/one.vtt" type="text/vtt"/>
</item><item><guid>two</guid><title>Two</title><link>https://example.com/two</link><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/>
<podcast:transcript url="https://example.com/two.vtt" type="text/vtt"/></item></channel></rss>`
	feed, err := Parse([]byte(raw), "https://example.com/feed.xml")
	if err != nil || len(feed.Items) != 2 {
		t.Fatal(feed, err)
	}
	for i, name := range []string{"one", "two"} {
		want := "https://example.com/" + name + ".vtt"
		if len(feed.Items[i].Transcripts) != 1 || feed.Items[i].Transcripts[0].URL != want {
			t.Fatal("wrong namespace or item association", feed.Items[i].Transcripts)
		}
	}
}

func TestPodcastTranscriptAtomAndAddressLimit(t *testing.T) {
	var tags strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&tags, `<pi:transcript url="https://example.com/%d.vtt" type="text/vtt"/>`, i)
	}
	raw := `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:pi="https://podcastindex.org/namespace/1.0"><title>Fixture</title><entry><id>episode</id><title>Episode</title><link href="https://example.com/episode"/><link rel="enclosure" href="https://example.com/audio.mp3" type="audio/mpeg"/>` + tags.String() + `</entry></feed>`
	feed, err := Parse([]byte(raw), "https://example.com/feed.xml")
	if err != nil || len(feed.Items) != 1 || len(feed.Items[0].Transcripts) != 8 {
		t.Fatal("Atom transcript or address limit incorrect", feed, err)
	}
	for i, transcript := range feed.Items[0].Transcripts {
		if transcript.URL != fmt.Sprintf("https://example.com/%d.vtt", i) {
			t.Fatal("publisher order changed", feed.Items[0].Transcripts)
		}
	}
}

func TestPodcastTranscriptMetadataPreservesIdentityAndLanguage(t *testing.T) {
	xml := `<rss version="2.0" xmlns:podcast="https://podcastindex.org/namespace/1.0"><channel><title>Fixture</title><language>zh-CN</language><item><guid>episode-one</guid><title>Episode</title><link>https://example.com/episode</link><enclosure url="https://example.com/audio.mp3" type="audio/mpeg"/>
<podcast:transcript url="https://example.com/captions.vtt" type="text/vtt; charset=utf-8"/>
<podcast:transcript url="https://example.com/captions.vtt" type="text/vtt"/>
<podcast:transcript url="https://example.com/captions.srt" type="application/x-subrip" language="en"/>
<podcast:transcript url="javascript:alert(1)" type="text/vtt"/>
<podcast:transcript url="https://example.com/plain.txt" type="text/plain"/>
<podcast:transcript url="https://example.com/captions.json" type="application/json; charset=utf-8" language="ja"/>
</item></channel></rss>`
	feed, err := Parse([]byte(xml), "https://example.com/feed.xml")
	if err != nil || len(feed.Items) != 1 {
		t.Fatal(feed, err)
	}
	item := feed.Items[0]
	if item.ID != "rss:"+Hash("https://example.com/feed.xml\nepisode-one") || item.Kind != "audio" || len(item.Transcripts) != 3 {
		t.Fatal("identity or transcript filtering incorrect", item)
	}
	if item.Transcripts[0].Language != "zh-CN" || item.Transcripts[1].Language != "en" || item.Transcripts[0].Type != "text/vtt" {
		t.Fatal("language inheritance or MIME normalization incorrect", item.Transcripts)
	}
	if err := content.Validate(item); err != nil {
		t.Fatal(err)
	}
	if item.Transcripts[2].Type != "application/json" || item.Transcripts[2].Language != "ja" {
		t.Fatal("JSON transcript metadata lost", item.Transcripts)
	}
}
