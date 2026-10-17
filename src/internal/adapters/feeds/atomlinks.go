package feeds

import (
	"mime"
	"strings"

	"github.com/mmcdole/gofeed"
	"github.com/mmcdole/gofeed/atom"
)

type readingAtomTranslator struct {
	gofeed.DefaultAtomTranslator
}

func (t *readingAtomTranslator) Translate(raw interface{}) (*gofeed.Feed, error) {
	result, err := t.DefaultAtomTranslator.Translate(raw)
	if err != nil {
		return nil, err
	}
	feed := raw.(*atom.Feed)
	for i, entry := range feed.Entries {
		result.Items[i].Link = atomReadingLink(entry.Links)
	}
	return result, nil
}

// Keep link relation/type metadata until choosing the reading URL. The default
// translator selects the first alternate, which can be an API representation.
func atomReadingLink(links []*atom.Link) string {
	selected, priority := "", 0
	for _, candidate := range links {
		if candidate == nil || (candidate.Rel != "" && candidate.Rel != "alternate") {
			continue
		}
		value := link(candidate.Href)
		if value == "" {
			continue
		}
		rank := 1
		if strings.TrimSpace(candidate.Type) == "" {
			rank = 2
		} else if typ, _, err := mime.ParseMediaType(candidate.Type); err == nil && (typ == "text/html" || typ == "application/xhtml+xml") {
			rank = 3
		}
		if rank > priority {
			selected, priority = value, rank
		}
	}
	return selected
}
