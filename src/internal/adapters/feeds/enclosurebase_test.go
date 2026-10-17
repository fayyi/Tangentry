package feeds

import (
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
)

func TestEnclosureSupplementIsAtomicAndBounded(t *testing.T) {
	prefix := `<rss xml:base="https://example.com/audio/"><channel><item><enclosure url="one.mp3" type="audio/mpeg"/></item>`
	for name, raw := range map[string]string{
		"truncated": prefix,
		"invalid trailing XML": prefix + `</channel></rss><broken`,
		"multiple roots": prefix + `</channel></rss><rss/>`,
		"trailing text": prefix + `</channel></rss>unexpected`,
		"excessive depth": prefix + strings.Repeat("<nested>", 257) + strings.Repeat("</nested>", 257) + `</channel></rss>`,
		"extra item": prefix + `<item><enclosure url="two.mp3"/></item></channel></rss>`,
		"extra enclosure": `<rss xml:base="https://example.com/audio/"><channel><item><enclosure url="one.mp3"/><enclosure url="two.mp3"/></item></channel></rss>`,
	} {
		t.Run(name, func(t *testing.T) {
			attachment := &gofeed.Enclosure{URL: "one.mp3", Type: "audio/mpeg"}
			feed := &gofeed.Feed{FeedType: "rss", Items: []*gofeed.Item{{Enclosures: []*gofeed.Enclosure{attachment}}}}
			resolveRSSEnclosures([]byte(raw), feed)
			if attachment.URL != "one.mp3" {
				t.Fatalf("unconfirmed supplement changed attachment: %q", attachment.URL)
			}
		})
	}
}

func TestEnclosureSupplementDeclaredLegacyCharset(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss version="2.0" xml:base="https://example.com/audio/"><channel><title>caf`)
	raw = append(raw, 0xe9)
	raw = append(raw, []byte(`</title><link>https://example.com</link><description>feed</description><item><guid>one</guid><title>episode</title><link>https://example.com/one</link><enclosure url="one.mp3" type="audio/mpeg"/></item></channel></rss>`)...)
	feed, err := Parse(raw, "https://example.com/private?key=fixture")
	if err != nil || len(feed.Items) != 1 || feed.Items[0].MediaURL != "https://example.com/audio/one.mp3" || feed.Items[0].Kind != "audio" {
		t.Fatalf("legacy-encoded attachment lost: %+v, %v", feed, err)
	}
}
