package youtube

import (
	"maps"
	"slices"
)

// HomeEntry is one normalized recommendation card.
type HomeEntry struct {
	ID            string
	Title         string
	Author        string
	AuthorID      string
	Avatar        string
	Thumbnail     string
	Duration      int
	PublishedText string
}

type image struct {
	url   string
	width float64
}

// card holds the fields of one parsed InnerTube video renderer that the
// normalizer reads: classic renderers and modern lockup view models alike.
type card struct {
	short          bool
	contentType    string
	id             string
	title          string
	authors        []string
	channelIDs     []string
	thumbnails     []image
	avatars        []image
	seconds        float64
	durationLabels []string
	published      string
	metadataRows   [][]string
}

// feedPage is the part of one browse response the home flow needs.
type feedPage struct {
	cards        []card
	nudge        bool
	continuation string
	more         bool
}

// Response sections the YouTube.js feed memo covered.
var feedSections = []string{"contents", "continuationContents", "onResponseReceivedCommands", "onResponseReceivedEndpoints", "onResponseReceivedActions", "sidebar"}

func parseFeedPage(data map[string]any) feedPage {
	var page feedPage
	visit := func(value any, header bool) {
		walk(value, func(key string, node map[string]any) bool {
			switch key {
			case "videoRenderer", "gridVideoRenderer", "compactVideoRenderer", "playlistVideoRenderer", "reelItemRenderer", "shortsLockupViewModel", "lockupViewModel":
				page.cards = append(page.cards, parseCard(key, node))
				return false
			case "feedNudgeRenderer":
				page.nudge = true
			case "continuationItemRenderer", "continuationItemViewModel":
				// Header continuations (such as chip bars) are not the next page.
				if !header && !page.more {
					page.more = true
					if token := continuationToken(node); token != "" && len(token) <= 16384 {
						page.continuation = token
					}
				}
				return false
			}
			return true
		})
	}
	for _, section := range feedSections {
		visit(data[section], false)
	}
	visit(data["header"], true)
	return page
}

// walk visits every object under a key in document order. visit returns
// false to skip a recognized node's children.
func walk(value any, visit func(string, map[string]any) bool) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walk(item, visit)
		}
	case map[string]any:
		// A renderer object normally has one key, so list order carries the
		// document order; sorted keys keep the rest deterministic.
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			child := typed[key]
			if object, ok := child.(map[string]any); ok && !visit(key, object) {
				continue
			}
			walk(child, visit)
		}
	}
}

func continuationToken(node map[string]any) string {
	token := ""
	walk(node, func(key string, child map[string]any) bool {
		if token != "" {
			return false
		}
		if key == "continuationCommand" {
			token = firstNonEmpty(stringAt(child, "token"), stringAt(child, "continuation"))
		}
		return token == ""
	})
	return token
}
