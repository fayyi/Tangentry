package youtube

import (
	"strconv"
	"strings"
)

func parseCard(kind string, node map[string]any) card {
	c := card{short: kind == "reelItemRenderer" || kind == "shortsLockupViewModel"}
	switch kind {
	case "videoRenderer", "gridVideoRenderer", "compactVideoRenderer", "playlistVideoRenderer":
		c.id = stringAt(node, "videoId")
		c.title = plainText(node["title"])
		owner := map[string]string{"videoRenderer": "ownerText", "gridVideoRenderer": "shortBylineText", "compactVideoRenderer": "longBylineText", "playlistVideoRenderer": "shortBylineText"}[kind]
		bylines := []any{node[owner]}
		if kind == "videoRenderer" {
			bylines = append(bylines, node["longBylineText"], node["shortBylineText"])
		}
		for _, byline := range bylines {
			if name := plainText(byline); name != "" {
				c.authors = append(c.authors, name)
				c.channelIDs = append(c.channelIDs, browseIDs(byline)...)
				break
			}
		}
		c.thumbnails = images(node["thumbnail"])
		c.avatars = images(firstPresent(path(node, "channelThumbnailSupportedRenderers", "channelThumbnailWithLinkRenderer", "thumbnail"), node["channelThumbnail"]))
		if seconds, err := strconv.ParseFloat(stringAt(node, "lengthSeconds"), 64); err == nil {
			c.seconds = seconds
		}
		c.durationLabels = append(c.durationLabels, plainText(node["lengthText"]))
		if overlays, ok := node["thumbnailOverlays"].([]any); ok {
			for _, overlay := range overlays {
				c.durationLabels = append(c.durationLabels, plainText(path(overlay, "thumbnailOverlayTimeStatusRenderer", "text")))
			}
		}
		c.published = plainText(node["publishedTimeText"])
	case "reelItemRenderer":
		c.id = stringAt(node, "videoId")
		c.title = plainText(node["headline"])
	case "shortsLockupViewModel":
		c.id, _ = path(node, "onTap", "innertubeCommand", "reelWatchEndpoint", "videoId").(string)
		c.title = plainText(path(node, "overlayMetadata", "primaryText"))
	case "lockupViewModel":
		c.id = stringAt(node, "contentId")
		c.contentType = strings.TrimPrefix(stringAt(node, "contentType"), "LOCKUP_CONTENT_TYPE_")
		metadata, _ := path(node, "metadata", "lockupMetadataViewModel").(map[string]any)
		c.title = plainText(metadata["title"])
		rows, _ := path(metadata, "metadata", "contentMetadataViewModel", "metadataRows").([]any)
		for i, row := range rows {
			parts, _ := path(row, "metadataParts").([]any)
			texts := []string{}
			for j, part := range parts {
				text := path(part, "text")
				texts = append(texts, plainText(text))
				if i == 0 && j == 0 {
					c.authors = append(c.authors, plainText(text))
					c.channelIDs = append(c.channelIDs, browseIDs(text)...)
				}
			}
			c.metadataRows = append(c.metadataRows, texts)
		}
		if avatar, ok := path(metadata, "image", "decoratedAvatarViewModel").(map[string]any); ok {
			c.avatars = images(path(avatar, "avatar", "avatarViewModel", "image"))
			c.channelIDs = append(c.channelIDs, browseIDs(path(avatar, "rendererContext", "commandContext", "onTap"))...)
		} else if stack, ok := path(metadata, "image", "avatarStackViewModel", "avatars").([]any); ok && len(stack) > 0 {
			c.avatars = images(path(stack[0], "avatarViewModel", "image"))
		}
		thumbnail := path(node, "contentImage", "thumbnailViewModel")
		c.thumbnails = images(path(thumbnail, "image"))
		overlays, _ := path(thumbnail, "overlays").([]any)
		for _, overlay := range overlays {
			badges, _ := firstPresent(path(overlay, "thumbnailBottomOverlayViewModel", "badges"), path(overlay, "thumbnailOverlayBadgeViewModel", "thumbnailBadges")).([]any)
			for _, badge := range badges {
				label, _ := path(badge, "thumbnailBadgeViewModel", "text").(string)
				c.durationLabels = append(c.durationLabels, label)
			}
		}
	}
	return c
}

func firstPresent(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// plainText reads classic text ({simpleText} or {runs}) and attributed text
// ({content}) alike.
func plainText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if text, ok := typed["simpleText"].(string); ok {
			return text
		}
		if text, ok := typed["content"].(string); ok {
			return text
		}
		if runs, ok := typed["runs"].([]any); ok {
			var builder strings.Builder
			for _, run := range runs {
				text, _ := path(run, "text").(string)
				builder.WriteString(text)
			}
			return builder.String()
		}
	}
	return ""
}

// browseIDs lists the channel browse endpoints a text or command links to.
func browseIDs(value any) []string {
	var ids []string
	walk(value, func(key string, node map[string]any) bool {
		if key == "browseEndpoint" {
			if id := stringAt(node, "browseId"); id != "" {
				ids = append(ids, id)
			}
			return false
		}
		return true
	})
	return ids
}

func images(value any) []image {
	list, ok := path(value, "thumbnails").([]any)
	if !ok {
		list, _ = path(value, "sources").([]any)
	}
	var result []image
	for _, item := range list {
		address, _ := path(item, "url").(string)
		width, _ := path(item, "width").(float64)
		result = append(result, image{url: address, width: width})
	}
	return result
}
