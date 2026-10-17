package content

import "strings"

// MaxPageSize bounds one library page.
const MaxPageSize = 200

var kinds = map[string]bool{"video": true, "article": true, "audio": true, "pdf": true, "image": true}

// Query filters the library. Q matches title, author and description.
type Query struct {
	Kind, Platform, Q string
	// Limit 0 reads every matching item.
	Limit, Offset int
}

// Page is one slice of matching cards; Counts are per kind over the
// platform and text filters, so kind tabs can show totals.
type Page struct {
	Items  []Item         `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
	Next   int            `json:"next,omitempty"`
}

func ValidQuery(q Query) bool {
	return (q.Kind == "" || kinds[q.Kind]) && len(q.Platform) <= 40 && !strings.ContainsAny(q.Platform, ":\x00") &&
		len(q.Q) <= 200 && q.Limit >= 0 && q.Limit <= MaxPageSize && q.Offset >= 0
}
