package playback

import (
	"net/url"
	"strconv"
	"time"
)

// Only API-supplied, allowlisted addresses for this one representation are
// considered. Never invent CDN hosts or mix bytes from different responses.
func normalizeSource(source Source) (Source, error) {
	if source.Platform == "" {
		source.Platform = "bilibili"
	}
	if !allowedFor(source.Platform, source.URL) {
		return Source{}, ErrUnavailable
	}
	seen := map[string]bool{source.URL: true}
	backups := make([]string, 0, 3)
	for _, raw := range source.Backups {
		if allowedFor(source.Platform, raw) && !seen[raw] {
			seen[raw] = true
			backups = append(backups, raw)
			if len(backups) == 3 {
				break
			}
		}
	}
	source.Backups = backups
	return source, nil
}

func sourceDeadline(raw string) time.Time {
	u, err := url.Parse(raw)
	if err != nil {
		return time.Time{}
	}
	value, err := strconv.ParseInt(u.Query().Get("deadline"), 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
