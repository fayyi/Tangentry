// Package discovery owns browsing results; discovering an item never saves it.
package discovery

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"tangentry/internal/content"
)

var ErrInvalidQuery = errors.New("invalid query")
var ErrUnavailable = errors.New("platform unavailable")

type PlatformError struct {
	Message    string
	Restricted bool
}

func (e *PlatformError) Error() string { return e.Message }
func (e *PlatformError) Unwrap() error { return ErrUnavailable }

type Feed struct {
	Items       []content.Item `json:"items"`
	Page        int            `json:"page"`
	Total       int            `json:"total"`
	HasMore     bool           `json:"hasMore"`
	SourceTitle string         `json:"sourceTitle"`
	Query       string         `json:"query"`
}

type Provider interface {
	CreatorVideos(context.Context, string, int) (Feed, error)
	Video(context.Context, string) (content.Item, error)
}

type Service struct{ provider Provider }

func NewService(provider Provider) *Service { return &Service{provider: provider} }

var uidPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var bvPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

func ParseQuery(raw string) (kind string, id string, err error) {
	raw = strings.TrimSpace(raw)
	if uidPattern.MatchString(raw) {
		if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
			return "", "", ErrInvalidQuery
		}
		return "creator", raw, nil
	}
	if bvPattern.MatchString(raw) {
		return "video", raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", ErrInvalidQuery
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Hostname() == "space.bilibili.com" && len(parts) > 0 && uidPattern.MatchString(parts[0]) {
		return ParseQuery(parts[0])
	}
	if (u.Hostname() == "www.bilibili.com" || u.Hostname() == "bilibili.com" || u.Hostname() == "m.bilibili.com") && len(parts) >= 2 && parts[0] == "video" && bvPattern.MatchString(parts[1]) {
		return "video", parts[1], nil
	}
	return "", "", ErrInvalidQuery
}

func (s *Service) Search(ctx context.Context, query string, page int) (Feed, error) {
	kind, id, err := ParseQuery(query)
	if err != nil || page < 1 || page > 1000 {
		return Feed{}, ErrInvalidQuery
	}
	if kind == "creator" {
		return s.provider.CreatorVideos(ctx, id, page)
	}
	if page != 1 {
		return Feed{}, ErrInvalidQuery
	}
	item, err := s.provider.Video(ctx, id)
	if err != nil {
		return Feed{}, err
	}
	return Feed{Items: []content.Item{item}, Page: 1, Total: 1, SourceTitle: item.Author, Query: id}, nil
}
