package subscriptions

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/netpolicy"
)

var rssID = regexp.MustCompile(`^rss:feed:[a-f0-9]{64}$`)
var paperSourceID = regexp.MustCompile(`^(?:arxiv|huggingface):source:[a-f0-9]{64}$`)

// SourceURL returns the canonical human-facing address for a saved source.
func SourceURL(id, creator string) string {
	if strings.HasPrefix(id, "youtube:channel:") {
		return discovery.YouTubeChannelURL(creator)
	}
	if strings.HasPrefix(id, "rss:feed:") {
		return creator
	}
	if strings.HasPrefix(id, "arxiv:source:") {
		return "arxiv:" + creator
	}
	if strings.HasPrefix(id, "huggingface:source:") {
		return "huggingface:" + creator
	}
	return "https://space.bilibili.com/" + creator
}

func validID(id string) bool {
	if strings.HasPrefix(id, "youtube:channel:") {
		return discovery.YouTubeChannelID(strings.TrimPrefix(id, "youtube:channel:"))
	}
	if rssID.MatchString(id) {
		return true
	}
	if paperSourceID.MatchString(id) {
		return true
	}
	if !strings.HasPrefix(id, "bilibili:creator:") {
		return false
	}
	kind, uid, err := discovery.ParseQuery(strings.TrimPrefix(id, "bilibili:creator:"))
	return err == nil && kind == "creator" && id == "bilibili:creator:"+uid
}

func name(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !utf8.ValidString(raw) || raw == "" || utf8.RuneCountInString(raw) > 100 {
		return "", ErrInvalid
	}
	return raw, nil
}

func rssAddress(raw string) (string, error) {
	address, err := netpolicy.URL(raw)
	if err != nil {
		return "", ErrInvalid
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", ErrInvalid
	}
	host := strings.ToLower(parsed.Hostname())
	domain := func(value string) bool { return host == value || strings.HasSuffix(host, "."+value) }
	// RSSHub routes and access keys may mention a platform without being its webpage.
	if domain("bilibili.com") || domain("youtube.com") && parsed.Path == "/watch" || domain("youtu.be") || domain("xiaoyuzhoufm.com") && strings.HasPrefix(parsed.Path, "/episode/") {
		return "", ErrInvalid
	}
	return address, nil
}

func normalizeSnapshot(id string, snapshot Snapshot) (Snapshot, error) {
	if snapshot.Items == nil || len(snapshot.Items) > 100 || snapshot.Total < len(snapshot.Items) || (len(snapshot.Items) == 0 && snapshot.Total != 0) || (snapshot.HasMore && snapshot.Total <= len(snapshot.Items)) {
		return Snapshot{}, ErrSnapshot
	}
	seen := make(map[string]bool)
	snapshot.Items = slices.Clone(snapshot.Items)
	for i := range snapshot.Items {
		item := &snapshot.Items[i]
		kind, bv, err := discovery.ParseQuery(item.PlatformID)
		if content.Validate(*item) != nil || seen[item.ID] || (strings.HasPrefix(id, "youtube:") && (item.Platform != "youtube" || item.Kind != "video" || item.AuthorID != strings.TrimPrefix(id, "youtube:channel:") || !validYouTubeItem(*item))) || (strings.HasPrefix(id, "bilibili:") && (err != nil || kind != "video" || bv != item.PlatformID || item.Platform != "bilibili")) || (strings.HasPrefix(id, "rss:") && item.Platform != "rss") || (strings.HasPrefix(id, "arxiv:source:") && item.Platform != "arxiv") || (strings.HasPrefix(id, "huggingface:source:") && item.Paper == nil) {
			return Snapshot{}, ErrSnapshot
		}
		seen[item.ID] = true
		item.SavedAt = 0
	}
	return snapshot, nil
}

func validYouTubeItem(item content.Item) bool {
	id, _, err := discovery.ParseYouTube(item.URL)
	return err == nil && id == item.PlatformID
}

func rssIdentity(address string) string {
	sum := sha256.Sum256([]byte(address))
	return hex.EncodeToString(sum[:])
}
