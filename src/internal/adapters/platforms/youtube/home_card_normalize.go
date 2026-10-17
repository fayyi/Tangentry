package youtube

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	validVideoID   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	validChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	durationLabel  = regexp.MustCompile(`^\d{1,3}:\d{2}(?::\d{2})?$`)
	thumbnailHost  = regexp.MustCompile(`^https://(?:i|img)\.ytimg\.com/`)
	avatarHost     = regexp.MustCompile(`^https://(?:yt3\.ggpht\.com|yt3\.googleusercontent\.com)/`)
	publicationRe  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(?:(?:Streamed|Premiered)\s+)?\d+\s+(?:second|minute|hour|day|week|month|year)s?\s+ago$`),
		regexp.MustCompile(`^(?:直播时间[:：]\s*|首播于\s*)?\d+\s*(?:秒|分钟|小时|天|周|个月|月|年)前$`),
		regexp.MustCompile(`^\d{4}年\d{1,2}月\d{1,2}日$`),
	}
)

func largestImage(list []image, allowed *regexp.Regexp) string {
	selected, width := "", -1.0
	for _, candidate := range list {
		if !allowed.MatchString(candidate.url) {
			continue
		}
		value := candidate.width
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			value = 0
		}
		if value > width {
			selected, width = candidate.url, value
		}
	}
	return selected
}

func (c card) duration() int {
	if c.seconds > 0 && !math.IsInf(c.seconds, 0) {
		return int(math.Min(604800, math.Floor(c.seconds)))
	}
	for _, label := range c.durationLabels {
		value := strings.TrimSpace(label)
		if !durationLabel.MatchString(value) {
			continue
		}
		total, valid := 0, true
		for i, part := range strings.Split(value, ":") {
			number, _ := strconv.Atoi(part)
			if i > 0 && number > 59 {
				valid = false
			}
			total = total*60 + number
		}
		if valid {
			return min(604800, total)
		}
	}
	return 0
}

func (c card) publication() string {
	if published := strings.TrimSpace(c.published); published != "" {
		return truncate(published, 256)
	}
	for i, row := range c.metadataRows {
		for j, part := range row {
			if i == 0 && j == 0 {
				continue
			}
			value := strings.TrimSpace(part)
			for _, pattern := range publicationRe {
				if pattern.MatchString(value) {
					return truncate(value, 256)
				}
			}
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

// normalizeCards keeps playable normal videos (no Shorts, playlists or
// stations), deduplicated, and rejects batches over the result budget.
func normalizeCards(cards []card, limit int) ([]HomeEntry, error) {
	entries := []HomeEntry{}
	seen := map[string]bool{}
	for _, c := range cards {
		if c.short || c.contentType == "SHORT" {
			continue
		}
		if c.contentType != "" && c.contentType != "VIDEO" && c.contentType != "MOVIE" {
			continue
		}
		title := truncate(strings.TrimSpace(c.title), 2000)
		if !validVideoID.MatchString(c.id) || title == "" || seen[c.id] {
			continue
		}
		if len(entries) >= limit {
			return nil, &innertubeError{code: "batch_size", hard: true}
		}
		entry := HomeEntry{ID: c.id, Title: title, Duration: c.duration(), PublishedText: c.publication()}
		for _, name := range c.authors {
			if name = strings.TrimSpace(name); name != "" && name != "N/A" {
				entry.Author = truncate(name, 500)
				break
			}
		}
		entry.Thumbnail = largestImage(c.thumbnails, thumbnailHost)
		if entry.Thumbnail == "" {
			entry.Thumbnail = "https://i.ytimg.com/vi/" + c.id + "/hqdefault.jpg"
		}
		entry.Avatar = largestImage(c.avatars, avatarHost)
		for _, id := range c.channelIDs {
			if validChannelID.MatchString(id) {
				entry.AuthorID = id
				break
			}
		}
		entries = append(entries, entry)
		seen[c.id] = true
	}
	return entries, nil
}
