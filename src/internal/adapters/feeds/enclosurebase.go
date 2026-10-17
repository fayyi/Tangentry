package feeds

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/url"
	"strings"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html/charset"
)

// Only RSS enclosures need this supplement; gofeed resolves Atom links itself.
// Keep explicit XML base scope, never infer it from a private subscription URL.
func resolveRSSEnclosures(data []byte, feed *gofeed.Feed) {
	if feed.FeedType != "rss" {
		return
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	type scope struct {
		name xml.Name
		base *url.URL
	}
	var stack []scope
	var resolved [][]string
	item := -1
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return // Do not apply a partially parsed supplement.
		}
		switch value := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if rootSeen || value.Name.Space != "" || value.Name.Local != "rss" {
					return
				}
				rootSeen = true
			}
			if len(stack) >= 256 {
				return
			}
			var base *url.URL
			if len(stack) > 0 {
				base = stack[len(stack)-1].base
			}
			for _, attr := range value.Attr {
				if attr.Name.Space == "http://www.w3.org/XML/1998/namespace" && attr.Name.Local == "base" {
					parsed, err := url.Parse(attr.Value)
					if err != nil {
						base = nil
					} else if base != nil {
						base = base.ResolveReference(parsed)
					} else {
						base = parsed
					}
				}
			}
			if value.Name.Space == "" && value.Name.Local == "item" && len(stack) == 2 && stack[1].name.Local == "channel" {
				item = len(resolved)
				resolved = append(resolved, nil)
			}
			if item >= 0 && value.Name.Space == "" && value.Name.Local == "enclosure" && len(stack) == 3 && stack[2].name.Local == "item" {
				result := ""
				for _, attr := range value.Attr {
					if attr.Name.Space == "" && attr.Name.Local == "url" && base != nil && base.IsAbs() {
						if reference, err := url.Parse(attr.Value); err == nil && attr.Value != "" {
							result = link(base.ResolveReference(reference).String())
						}
					}
				}
				resolved[item] = append(resolved[item], result)
			}
			stack = append(stack, scope{value.Name, base})
		case xml.EndElement:
			if len(stack) == 3 && value.Name.Space == "" && value.Name.Local == "item" {
				item = -1
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(value)) != "" {
				return
			}
		}
	}
	if len(resolved) != len(feed.Items) {
		return
	}
	for index, entry := range feed.Items {
		if len(resolved[index]) != len(entry.Enclosures) {
			continue
		}
		for column, enclosure := range entry.Enclosures {
			if enclosure != nil && resolved[index][column] != "" && link(enclosure.URL) == "" {
				enclosure.URL = resolved[index][column]
			}
		}
	}
}
