package feeds

import (
	"bytes"
	"encoding/xml"
	"io"
	"mime"
	"strings"
	"tangentry/internal/content"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html/charset"
)

const podcastNamespace = "https://podcastindex.org/namespace/1.0"
const podcastAlternateNamespace = "https://github.com/Podcastindex-org/podcast-namespace/blob/main/docs/1.0.md"

// gofeed's extension keys lose the original namespace URI. Read resolved XML
// names so aliases and locally rebound prefixes cannot select the wrong tag.
func podcastTranscripts(data []byte, feed *gofeed.Feed) [][]content.Transcript {
	if (feed.FeedType != "rss" && feed.FeedType != "atom") || len(data) > maxFeedBytes {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	var stack, parents []xml.Name
	var entryName xml.Name
	var result [][]content.Transcript
	var seen map[content.Transcript]bool
	item := -1
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch value := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if rootSeen {
					return nil
				}
				rootSeen = true
				switch {
				case feed.FeedType == "rss" && value.Name == (xml.Name{Local: "rss"}):
					parents = []xml.Name{value.Name, {Local: "channel"}}
					entryName = xml.Name{Local: "item"}
				case feed.FeedType == "rss" && value.Name == (xml.Name{Space: "http://www.w3.org/1999/02/22-rdf-syntax-ns#", Local: "RDF"}):
					parents = []xml.Name{value.Name}
					entryName = xml.Name{Space: "http://purl.org/rss/1.0/", Local: "item"}
				case feed.FeedType == "atom" && value.Name.Local == "feed" && (value.Name.Space == "http://www.w3.org/2005/Atom" || value.Name.Space == "http://purl.org/atom/ns#" || value.Name.Space == ""):
					parents = []xml.Name{value.Name}
					entryName = xml.Name{Space: value.Name.Space, Local: "entry"}
				default:
					return nil
				}
			}
			if len(stack) >= 256 {
				return nil
			}
			isEntry := value.Name == entryName && len(stack) == len(parents)
			if isEntry {
				for i, parent := range parents {
					if stack[i] != parent {
						isEntry = false
						break
					}
				}
			}
			if isEntry {
				if len(result) >= len(feed.Items) {
					return nil
				}
				item = len(result)
				result = append(result, nil)
				seen = make(map[content.Transcript]bool)
			}
			if item >= 0 && len(stack) == len(parents)+1 && value.Name.Local == "transcript" &&
				(value.Name.Space == podcastNamespace || value.Name.Space == podcastAlternateNamespace) && len(result[item]) < 8 {
				if transcript, ok := podcastTranscript(value.Attr, feed.Language); ok && !seen[transcript] {
					seen[transcript] = true
					result[item] = append(result[item], transcript)
				}
			}
			stack = append(stack, value.Name)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil
			}
			if item >= 0 && len(stack) == len(parents)+1 && value.Name == entryName {
				item = -1
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(value)) != "" {
				return nil
			}
		}
	}
	// Apply only a complete supplement matching the parser's item order.
	if !rootSeen || len(stack) != 0 || len(result) != len(feed.Items) {
		return nil
	}
	return result
}

func podcastTranscript(attrs []xml.Attr, language string) (content.Transcript, bool) {
	var rawURL, rawType, lang string
	for _, attr := range attrs {
		if attr.Name.Space != "" {
			continue
		}
		switch attr.Name.Local {
		case "url":
			rawURL = attr.Value
		case "type":
			rawType = attr.Value
		case "language":
			lang = strings.TrimSpace(attr.Value)
		}
	}
	url := link(rawURL)
	kind, _, err := mime.ParseMediaType(rawType)
	if err != nil || url == "" || len(url) > 4096 || (kind != "text/vtt" && kind != "application/x-subrip" && kind != "application/json") {
		return content.Transcript{}, false
	}
	if lang == "" {
		lang = strings.TrimSpace(language)
	}
	if len(lang) > 64 || strings.ContainsAny(lang, "\r\n\x00") {
		return content.Transcript{}, false
	}
	return content.Transcript{URL: url, Type: kind, Language: lang}, true
}
