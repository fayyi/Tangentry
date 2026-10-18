package content

import "strings"

// CardDescriptionRunes bounds descriptions in card listings.
const CardDescriptionRunes = 1200

// Card returns the fields a content card shows, without article bodies.
func Card(item Item) Item {
	description := []rune(item.Description)
	if len(item.ArticleBlocks) == 0 && len(item.Transcripts) == 0 && len(description) <= CardDescriptionRunes {
		return item
	}
	item.ArticleBlocks, item.Transcripts, item.Discussion, item.Partial = nil, nil, nil, true
	if len(description) > CardDescriptionRunes {
		item.Description = string(description[:CardDescriptionRunes])
	}
	return item
}

// Navigation-only feed labels are not article summaries.
func UsefulDescription(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "comments", "comment", "discussion", "read more", "阅读全文", "阅读原文", "查看评论", "评论":
		return false
	}
	return true
}
