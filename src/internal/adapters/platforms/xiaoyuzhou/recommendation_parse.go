package xiaoyuzhou

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/content"
)

// collectEpisodes walks the provider's editorial tree and keeps only concrete
// episode records. It deliberately contains no request or session state.
func collectEpisodes(value any, items *[]content.Item, seen map[string]bool) {
	collectEpisodeNodes(value, items, seen, 0)
}

func collectEpisodeNodes(value any, items *[]content.Item, seen map[string]bool, depth int) {
	if depth > 32 || len(*items) >= 200 {
		return
	}
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			collectEpisodeNodes(child, items, seen, depth+1)
		}
	case map[string]any:
		if item, ok := episodeItem(node); ok && !seen[item.ID] {
			seen[item.ID] = true
			*items = append(*items, item)
		}
		for _, child := range node {
			collectEpisodeNodes(child, items, seen, depth+1)
		}
	}
}

func episodeItem(v map[string]any) (content.Item, bool) {
	id, _ := v["eid"].(string)
	if !episodeID.MatchString(id) {
		return content.Item{}, false
	}
	title, _ := v["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return content.Item{}, false
	}
	author, authorID := "小宇宙", ""
	if p, ok := v["podcast"].(map[string]any); ok {
		if x, _ := p["title"].(string); x != "" {
			author = x
		}
		if x, _ := p["pid"].(string); x != "" {
			authorID = x
		}
	}
	image := episodeImage(v)
	duration := 0
	if n, ok := v["duration"].(float64); ok && n > 0 {
		duration = int(n)
	}
	item := content.Item{ID: "xiaoyuzhou:" + id, Platform: "xiaoyuzhou", PlatformID: id, Kind: "audio", Title: title, Author: author, AuthorID: authorID, URL: "https://www.xiaoyuzhoufm.com/episode/" + id, ThumbnailURL: image, DurationSeconds: duration, PublishedAt: recommendationTime(v)}
	if private, _ := v["isPrivateMedia"].(bool); !private {
		if enclosure, ok := v["enclosure"].(map[string]any); ok {
			if raw, _ := enclosure["url"].(string); validAudioURL(raw) {
				item.MediaURL = raw
			}
		}
	}
	if content.Validate(item) != nil {
		return content.Item{}, false
	}
	return item, true
}

func recommendationTime(v map[string]any) int64 {
	for _, key := range []string{"pubDate", "publishedAt", "publishTime", "pubtime", "createdAt", "createTime"} {
		if value, ok := v[key]; ok {
			if timestamp := parseRecommendationTime(value); timestamp > 0 {
				return timestamp
			}
		}
	}
	return 0
}

func parseRecommendationTime(value any) int64 {
	switch raw := value.(type) {
	case float64:
		return normalizeRecommendationUnix(int64(raw))
	case int64:
		return normalizeRecommendationUnix(raw)
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed.Unix()
		}
		if number, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return normalizeRecommendationUnix(number)
		}
	}
	return 0
}

func normalizeRecommendationUnix(value int64) int64 {
	// Millisecond timestamps are currently used by some discovery responses.
	if value > 100000000000 {
		value /= 1000
	}
	if value < 946684800 || value > time.Now().Add(24*time.Hour).Unix() {
		return 0
	}
	return value
}

// episodeImage accepts the image shapes used by both the discovery feed and
// the app episode API. Older episodes omit imageUrl at the episode level and
// only expose the podcast cover, which should still be a useful card image.
func episodeImage(v map[string]any) string {
	for _, key := range []string{"imageUrl", "coverUrl", "cover", "picUrl", "image"} {
		if image := imageValue(v[key], 0); image != "" {
			return image
		}
	}
	if podcast, ok := v["podcast"].(map[string]any); ok {
		return imageValue(podcast, 0)
	}
	return ""
}

func imageValue(value any, depth int) string {
	if depth > 4 {
		return ""
	}
	if raw, ok := value.(string); ok {
		image := normalizeImage(raw)
		u, err := url.Parse(image)
		if err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
			return image
		}
	}
	if nested, ok := value.(map[string]any); ok {
		for _, key := range []string{"picUrl", "imageUrl", "coverUrl", "image", "cover", "avatar", "avatarUrl", "url", "largePicUrl", "middlePicUrl", "thumbnailUrl", "smallPicUrl"} {
			if image := imageValue(nested[key], depth+1); image != "" {
				return image
			}
		}
	}
	return ""
}

func normalizeImage(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}
