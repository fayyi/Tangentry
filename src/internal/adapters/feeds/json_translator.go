package feeds

import (
	"html"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
	jsonfeed "github.com/mmcdole/gofeed/json"
)

// Preserve format-specific reading metadata while delegating parsing to gofeed.
type readingJSONTranslator struct{}

func (*readingJSONTranslator) Translate(source interface{}) (*gofeed.Feed, error) {
	feed, err := (&gofeed.DefaultJSONTranslator{}).Translate(source)
	if err != nil {
		return nil, err
	}
	original := source.(*jsonfeed.Feed)
	for index, entry := range original.Items {
		item := feed.Items[index]
		if strings.TrimSpace(entry.ID) == "" {
			item.Link = ""
			item.Enclosures = nil
			continue
		}
		// Only content_html is markup; literal angle brackets must survive.
		if entry.ContentHTML == "" {
			item.Content = html.EscapeString(entry.ContentText)
		}
		item.Title = html.EscapeString(entry.Title)
		item.Description = html.EscapeString(entry.Summary)
		authors := entry.Authors
		if authors == nil && entry.Author != nil {
			authors = []*jsonfeed.Author{entry.Author}
		}
		if authors == nil {
			authors = original.Authors
			if authors == nil && original.Author != nil {
				authors = []*jsonfeed.Author{original.Author}
			}
		}
		item.Custom = make(map[string]string)
		if len(authors) > 0 && authors[0] != nil {
			item.Author = &gofeed.Person{Name: html.EscapeString(authors[0].Name)}
			item.Custom["readingAvatar"] = link(authors[0].Avatar)
		}
		_, media := enclosureMedia(item.Enclosures)
		if entry.Attachments != nil {
			for _, attachment := range *entry.Attachments {
				if media != "" && link(attachment.URL) == media && attachment.DurationInSeconds > 0 && attachment.DurationInSeconds <= 604800 {
					item.Custom["readingDuration"] = strconv.FormatInt(attachment.DurationInSeconds, 10)
					break
				}
			}
		}
	}
	feed.Title = html.EscapeString(original.Title)
	return feed, nil
}
