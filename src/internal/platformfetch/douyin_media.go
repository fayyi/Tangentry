package platformfetch

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const MaxDouyinMediaURLLength = 8192

var douyinCDNSuffixes = []string{".douyinvod.com", ".douyinvod.net", ".bytecdn.cn", ".zjcdn.com", ".amemv.com"}

// IsDouyinCDNHost is the shared boundary for public Douyin media hosts.
func IsDouyinCDNHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, suffix := range douyinCDNSuffixes {
		if strings.HasSuffix(host, suffix) && host != suffix[1:] {
			return true
		}
	}
	return false
}

// NormalizeDouyinMediaURL upgrades allowlisted HTTP media URLs before they
// enter a playback path. The returned reason is a bounded diagnostic enum.
func NormalizeDouyinMediaURL(raw string, now time.Time) (string, string) {
	if len(raw) > MaxDouyinMediaURLLength {
		return "", "too_long"
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "scheme"
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "scheme"
	}
	if u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return "", "host_not_allowed"
	}
	port := u.Port()
	if port != "" && !((u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")) {
		return "", "host_not_allowed"
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !IsDouyinCDNHost(host) {
		return "", "host_not_allowed"
	}
	if !douyinMediaURLLive(u, now) {
		return "", "expired"
	}
	if u.Scheme == "http" {
		u.Scheme = "https"
		if port == "80" {
			u.Host = host
		}
	}
	return u.String(), ""
}

func DouyinMediaDeadline(raw string) time.Time {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}
	query := u.Query()
	for _, key := range []string{"deadline", "x-expires", "expires"} {
		value, err := strconv.ParseInt(query.Get(key), 10, 64)
		if err == nil && value > 0 {
			return time.Unix(value, 0)
		}
	}
	return time.Time{}
}

func douyinMediaURLLive(u *url.URL, now time.Time) bool {
	query := u.Query()
	for _, key := range []string{"deadline", "x-expires", "expires"} {
		value := query.Get(key)
		if value == "" {
			continue
		}
		unix, err := strconv.ParseInt(value, 10, 64)
		return err == nil && unix > now.Unix()
	}
	return true
}
