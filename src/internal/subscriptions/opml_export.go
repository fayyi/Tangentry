package subscriptions

import (
	"context"
	"encoding/xml"
	"strings"
)

func (s *Service) ExportOPML(ctx context.Context) ([]byte, error) {
	sources, err := s.repository.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	document := opmlDocument{Version: "2.0", Head: opmlHead{Title: "Tangentry subscriptions"}}
	document.Body.Outlines = make([]opmlOutline, 0, len(sources))
	folders := make(map[string]int)
	if repository, ok := s.repository.(GroupRepository); ok {
		groups, err := repository.ListSubscriptionGroups(ctx)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			folders[group.ID] = len(document.Body.Outlines)
			document.Body.Outlines = append(document.Body.Outlines, opmlOutline{Text: group.Name, Title: group.Name})
		}
	}
	for _, source := range sources {
		outline := opmlOutline{Text: source.Name, Title: source.Name, HTMLURL: source.URL}
		switch {
		case strings.HasPrefix(source.ID, "rss:feed:"):
			outline.Type = "rss"
			outline.XMLURL = source.CreatorID
		case strings.HasPrefix(source.ID, "youtube:channel:"):
			outline.Type = "rss"
			outline.TangentryType = "youtube"
			outline.XMLURL = "https://www.youtube.com/feeds/videos.xml?channel_id=" + source.CreatorID
		case strings.HasPrefix(source.ID, "bilibili:creator:"):
			// Standard OPML has no field for a Bilibili creator. Keep a
			// Tangentry extension so our own export round-trips it.
			outline.Type = "bilibili"
			outline.TangentryType = "bilibili"
		default:
			continue
		}
		if index, exists := folders[source.GroupID]; exists {
			document.Body.Outlines[index].Children = append(document.Body.Outlines[index].Children, outline)
		} else {
			document.Body.Outlines = append(document.Body.Outlines, outline)
		}
	}
	filtered := document.Body.Outlines[:0]
	for _, outline := range document.Body.Outlines {
		if len(outline.Children) > 0 || outline.XMLURL != "" || outline.HTMLURL != "" {
			filtered = append(filtered, outline)
		}
	}
	document.Body.Outlines = filtered
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
