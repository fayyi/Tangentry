package notes

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"
)

const SearchPageSize = 50

type SearchQuery struct {
	PinnedOnly      bool
	Text            string
	Tag             string
	FromCreatedAt   int64
	ToCreatedAt     int64
	BeforeCreatedAt int64
	BeforeID        string
}
type SearchPage struct {
	PinnedOnly bool   `json:"pinnedOnly"`
	Notes      []Note `json:"notes"`
	NextCursor string `json:"nextCursor"`
	Query      string `json:"query"`
	Tag        string `json:"tag"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
}

type Filters struct {
	PinnedOnly bool
	Tag        string
	From       int64
	To         int64
}

// Pagination uses immutable creation time and identity. Deleting the boundary
// note does not invalidate the cursor; editing a body never changes its rank.
func (s *Service) Search(ctx context.Context, text, cursor string) (SearchPage, error) {
	return s.SearchFiltered(ctx, text, cursor, Filters{})
}

func (s *Service) SearchFiltered(ctx context.Context, text, cursor string, filters Filters) (SearchPage, error) {
	input, err := normalizeSearch(text, filters)
	if err != nil {
		return SearchPage{}, err
	}
	if cursor != "" {
		if len(cursor) > 53 {
			return SearchPage{}, ErrInvalid
		}
		stamp, id, ok := strings.Cut(cursor, ":")
		createdAt, err := strconv.ParseInt(stamp, 10, 64)
		if !ok || err != nil || createdAt <= 0 || !noteIDPattern.MatchString(id) {
			return SearchPage{}, ErrInvalid
		}
		input.BeforeCreatedAt, input.BeforeID = createdAt, id
	}
	values, err := s.repository.SearchNotes(ctx, input)
	if err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Notes: values, Query: input.Text, Tag: input.Tag, From: input.FromCreatedAt, To: input.ToCreatedAt, PinnedOnly: input.PinnedOnly}
	if len(values) > SearchPageSize {
		page.Notes = values[:SearchPageSize]
		last := page.Notes[len(page.Notes)-1]
		page.NextCursor = strconv.FormatInt(last.CreatedAt, 10) + ":" + last.ID
	}
	return page, nil
}

// Search and exports share the same literal text, tag and half-open date filters.
func normalizeSearch(text string, filters Filters) (SearchQuery, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 200 {
		return SearchQuery{}, ErrInvalid
	}
	if filters.Tag != "" {
		tags, err := normalizeTags([]string{filters.Tag})
		if err != nil {
			return SearchQuery{}, err
		}
		filters.Tag = tags[0]
	}
	if filters.From < 0 || filters.To < 0 || filters.From > 253402300799 || filters.To > 253402300799 || (filters.To != 0 && filters.To <= filters.From) {
		return SearchQuery{}, ErrInvalid
	}
	return SearchQuery{Text: text, Tag: filters.Tag, FromCreatedAt: filters.From, ToCreatedAt: filters.To, PinnedOnly: filters.PinnedOnly}, nil
}
