package favoritesync

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"tangentry/internal/content"
)

func filterTerms(raw string) []string {
	var terms []string
	for _, term := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == '\r' }) {
		if term = strings.TrimSpace(term); term != "" {
			terms = append(terms, strings.ToLower(term))
		}
	}
	return terms
}

func validFilter(raw string) bool {
	if !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > 300 || len(filterTerms(raw)) > 20 {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func normalizedPolicy(p Policy) Policy {
	p.TitleInclude = strings.TrimSpace(p.TitleInclude)
	p.TitleExclude = strings.TrimSpace(p.TitleExclude)
	p.AuthorInclude = strings.TrimSpace(p.AuthorInclude)
	p.DirectoryName = strings.TrimSpace(p.DirectoryName)
	p.Download = normalizeDownloadOptions(p.Download)
	if p.DownloadSubtitles || len(p.Download.SubtitleLanguages) > 0 {
		p.DownloadSubtitles = true
		if len(p.Download.SubtitleLanguages) == 0 {
			p.Download.SubtitleLanguages = []string{"all"}
		}
	} else {
		p.Download.AutomaticSubtitles = false
	}
	return p
}

func containsTerm(text string, terms []string) bool {
	text = strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func (p Policy) matchesDownload(item content.Item) bool {
	include, exclude, authors := filterTerms(p.TitleInclude), filterTerms(p.TitleExclude), filterTerms(p.AuthorInclude)
	return (len(include) == 0 || containsTerm(item.Title, include)) &&
		!containsTerm(item.Title, exclude) &&
		(len(authors) == 0 || containsTerm(item.Author, authors))
}

func matchingPending(p Policy, items []content.Item, pending []string) []string {
	eligible := make(map[string]bool, len(items))
	for _, item := range items {
		eligible[item.ID] = p.matchesDownload(item)
	}
	result := make([]string, 0, len(pending))
	for _, id := range pending {
		if eligible[id] {
			result = append(result, id)
		}
	}
	return result
}
