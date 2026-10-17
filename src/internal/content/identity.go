package content

import (
	"regexp"
	"strings"
)

// Content identities are shared by notes, read state and saved transcripts.
// Source IDs and arbitrary locator URLs never identify an individual item.
var identities = map[string]*regexp.Regexp{
	"bilibili":    regexp.MustCompile(`^(?:BV[0-9A-Za-z]{10}|dynamic:[0-9A-Za-z_-]{1,64})$`),
	"youtube":     regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`),
	"douyin":      regexp.MustCompile(`^[1-9][0-9]{15,19}$`),
	"xiaohongshu": regexp.MustCompile(`^[a-f0-9]{24}$`),
	"rss":         regexp.MustCompile(`^[a-f0-9]{64}$`),
	"pdf":         regexp.MustCompile(`^[a-f0-9]{64}$`),
	"xiaoyuzhou":  regexp.MustCompile(`^[a-f0-9]{24}$`),
	"linuxdo":     regexp.MustCompile(`^[1-9][0-9]{0,18}$`),
}

func ValidID(id string) bool {
	platform, value, ok := strings.Cut(id, ":")
	pattern := identities[platform]
	return ok && pattern != nil && pattern.MatchString(value)
}

func SpeechID(id string) bool { return !strings.HasPrefix(id, "pdf:") && ValidID(id) }
