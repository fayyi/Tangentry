package feeds

import (
	"mime"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
)

// RSS is a transport, not a media type. Explicit MIME wins; image attachments
// do not hide a playable episode. Missing or unknown types remain articles.
func enclosureMedia(entries []*gofeed.Enclosure) (string, string) {
	kind, media, priority := "article", "", 0
	for _, e := range entries {
		if e == nil {
			continue
		}
		value := link(e.URL)
		if value == "" {
			continue
		}
		typ, _, err := mime.ParseMediaType(e.Type)
		if err != nil {
			continue
		}
		selected, rank := "", 0
		switch {
		case strings.HasPrefix(typ, "audio/"):
			selected, rank = "audio", 3
		case strings.HasPrefix(typ, "video/"):
			selected, rank = "video", 3
		case typ == "application/pdf":
			selected, rank = "pdf", 2
		case strings.HasPrefix(typ, "image/"):
			selected, rank = "image", 1
		}
		if rank > priority {
			kind, media, priority = selected, value, rank
		}
	}
	return kind, media
}

func podcastDuration(raw string) int {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) > 3 {
		return 0
	}
	total := 0
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 604800 || (i > 0 && n > 59) {
			return 0
		}
		total = total*60 + n
		if total > 604800 {
			return 0
		}
	}
	return total
}
