package playback

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"tangentry/internal/platformfetch"
)

// Only API-supplied, allowlisted addresses for this one representation are
// considered. Never invent CDN hosts or mix bytes from different responses.
func normalizeSource(source Source) (Source, error) {
	if source.Platform == "" {
		source.Platform = "bilibili"
	}
	if source.Platform == "douyin" {
		value, reason := platformfetch.NormalizeDouyinMediaURL(source.URL, time.Now())
		if reason != "" {
			return Source{}, ErrUnavailable
		}
		source.URL = value
	}
	if !allowedFor(source.Platform, source.URL) {
		return Source{}, ErrUnavailable
	}
	now := time.Now()
	if deadline := sourceDeadline(source.URL); !deadline.IsZero() && !deadline.After(now) {
		return Source{}, ErrUnavailable
	}
	seen := map[string]bool{source.URL: true}
	backups := make([]string, 0, 3)
	for _, raw := range source.Backups {
		if source.Platform == "douyin" {
			normalized, reason := platformfetch.NormalizeDouyinMediaURL(raw, now)
			if reason != "" {
				continue
			}
			raw = normalized
		}
		deadline := sourceDeadline(raw)
		if allowedFor(source.Platform, raw) && (deadline.IsZero() || deadline.After(now)) && !seen[raw] {
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
	if platformfetch.IsDouyinCDNHost(strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))) {
		return platformfetch.DouyinMediaDeadline(raw)
	}
	value, err := strconv.ParseInt(u.Query().Get("deadline"), 10, 64)
	if err != nil || value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
