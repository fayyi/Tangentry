package notes

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

var tagPattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+(/[\p{L}\p{N}_-]+)*$`)

func normalizeTags(input []string) ([]string, error) {
	if len(input) > 12 {
		return nil, ErrInvalid
	}
	values := make([]string, 0, len(input))
	seen := make(map[string]bool)
	for _, raw := range input {
		tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
		if utf8.RuneCountInString(tag) > 32 || !tagPattern.MatchString(tag) {
			return nil, ErrInvalid
		}
		if !seen[tag] {
			seen[tag] = true
			values = append(values, tag)
		}
	}
	return values, nil
}

func (s *Service) Tags(ctx context.Context) ([]TagCount, error) {
	return s.repository.NoteTags(ctx)
}
