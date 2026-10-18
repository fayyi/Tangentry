// Package papers contains paper identities and provider payload parsers.
package papers

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tangentry/internal/content"
)

var ErrInvalidReference = errors.New("invalid paper reference")
var ErrInvalidPayload = errors.New("invalid paper payload")

var modernID = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4,5}$`)
var legacyID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.-]*(?:\.[A-Za-z]{2})?/[0-9]{7}$`)
var versionSuffix = regexp.MustCompile(`(?i)v([1-9][0-9]{0,3})$`)
var categoryPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*(?:\+[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*)*$`)
var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func NormalizeCategory(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 256 || !categoryPattern.MatchString(raw) {
		return "", ErrInvalidReference
	}
	return strings.ToLower(raw), nil
}

type Reference struct {
	ID          string
	Version     int
	PDFURL      string
	ArxivURL    string
	HFURL       string
	AlphaXivURL string
}

type Paper struct {
	ID          string
	Version     int
	Title       string
	Authors     []string
	Categories  []string
	Abstract    string
	PublishedAt int64
	UpdatedAt   int64
	PDFURL      string
	ArxivURL    string
	HFURL       string
	AlphaXivURL string
	GitHubURL   string
	Upvotes     int
	GitHubStars int
	AIKeywords  []string
	AISummary   string
}

func ParseReference(raw string) (Reference, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return Reference{}, ErrInvalidReference
	}
	value := raw
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "arxiv:") {
		value = strings.TrimSpace(value[len("arxiv:"):])
	}
	// Pasted references often omit the scheme (for example
	// "huggingface.co/papers/2610.22322"). Treat only the known paper hosts as
	// URL-like; arbitrary hostnames must still be rejected below.
	if !strings.Contains(value, "://") {
		for _, host := range []string{"arxiv.org/", "www.arxiv.org/", "huggingface.co/", "www.huggingface.co/", "alphaxiv.org/", "www.alphaxiv.org/", "alpha-xiv.org/", "www.alpha-xiv.org/"} {
			if strings.HasPrefix(strings.ToLower(value), host) {
				value = "https://" + value
				break
			}
		}
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return Reference{}, ErrInvalidReference
		}
		host := strings.ToLower(u.Hostname())
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch host {
		case "arxiv.org", "www.arxiv.org":
			if len(parts) < 2 || (parts[0] != "abs" && parts[0] != "pdf") {
				return Reference{}, ErrInvalidReference
			}
			value = strings.Join(parts[1:], "/")
		case "huggingface.co", "www.huggingface.co":
			if len(parts) != 2 || parts[0] != "papers" {
				return Reference{}, ErrInvalidReference
			}
			value = parts[1]
		case "alphaxiv.org", "www.alphaxiv.org", "alpha-xiv.org", "www.alpha-xiv.org":
			if len(parts) != 2 || (parts[0] != "abs" && parts[0] != "papers") {
				return Reference{}, ErrInvalidReference
			}
			value = parts[1]
		default:
			return Reference{}, ErrInvalidReference
		}
	}
	if strings.HasSuffix(strings.ToLower(value), ".pdf") {
		value = value[:len(value)-len(".pdf")]
	}
	version := 0
	if match := versionSuffix.FindStringSubmatch(value); len(match) == 2 {
		version, _ = strconv.Atoi(match[1])
		value = value[:len(value)-len(match[0])]
	}
	if !validID(value) {
		return Reference{}, ErrInvalidReference
	}
	return Reference{
		ID: value, Version: version,
		PDFURL:      "https://arxiv.org/pdf/" + value + versionString(version) + ".pdf",
		ArxivURL:    "https://arxiv.org/abs/" + value + versionString(version),
		HFURL:       "https://huggingface.co/papers/" + value,
		AlphaXivURL: "https://alphaxiv.org/abs/" + value + versionString(version),
	}, nil
}

func validID(id string) bool { return modernID.MatchString(id) || legacyID.MatchString(id) }

func versionString(version int) string {
	if version < 1 {
		return ""
	}
	return "v" + strconv.Itoa(version)
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Summary    string         `xml:"summary"`
	Published  string         `xml:"published"`
	Updated    string         `xml:"updated"`
	Authors    []atomAuthor   `xml:"author"`
	Categories []atomCategory `xml:"category"`
	Links      []atomLink     `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}
type atomCategory struct {
	Term string `xml:"term,attr"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

func ParseArxivAtom(data []byte) ([]Paper, error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return nil, ErrInvalidPayload
	}
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil || len(feed.Entries) == 0 {
		return nil, ErrInvalidPayload
	}
	result := make([]Paper, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		ref, err := ParseReference(entry.ID)
		if err != nil {
			continue
		}
		paper := Paper{ID: ref.ID, Version: ref.Version, Title: clean(entry.Title, 2000), Abstract: clean(entry.Summary, 20000), PDFURL: ref.PDFURL, ArxivURL: ref.ArxivURL, HFURL: ref.HFURL, AlphaXivURL: ref.AlphaXivURL}
		for _, author := range entry.Authors {
			if name := clean(author.Name, 300); name != "" && len(paper.Authors) < 200 {
				paper.Authors = append(paper.Authors, name)
			}
		}
		for _, category := range entry.Categories {
			if value := clean(category.Term, 100); value != "" && len(paper.Categories) < 50 {
				paper.Categories = append(paper.Categories, value)
			}
		}
		paper.PublishedAt = parseTime(entry.Published)
		paper.UpdatedAt = parseTime(entry.Updated)
		for _, link := range entry.Links {
			if strings.EqualFold(link.Rel, "related") && strings.Contains(strings.ToLower(link.Type), "pdf") && fixedArxivPDF(link.Href, ref.ID) {
				paper.PDFURL = link.Href
			}
		}
		if paper.Title != "" {
			result = append(result, paper)
		}
	}
	if len(result) == 0 {
		return nil, ErrInvalidPayload
	}
	return result, nil
}

func fixedArxivPDF(raw, id string) bool {
	ref, err := ParseReference(raw)
	return err == nil && ref.ID == id && strings.HasPrefix(strings.ToLower(raw), "https://arxiv.org/pdf/")
}

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Items []rssItem `xml:"item"`
}
type rssItem struct {
	GUID        string   `xml:"guid"`
	Link        string   `xml:"link"`
	Title       string   `xml:"title"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
	Authors     []string `xml:"author"`
	Creator     string   `xml:"creator"`
	Categories  []string `xml:"category"`
}

func ParseArxivRSS(data []byte) ([]Paper, error) {
	if len(data) == 0 || len(data) > 16<<20 {
		return nil, ErrInvalidPayload
	}
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil || len(feed.Channel.Items) == 0 {
		return nil, ErrInvalidPayload
	}
	result := make([]Paper, 0, len(feed.Channel.Items))
	for _, entry := range feed.Channel.Items {
		ref, err := ParseReference(entry.GUID)
		if err != nil {
			ref, err = ParseReference(entry.Link)
		}
		if err != nil {
			continue
		}
		paper := Paper{ID: ref.ID, Version: ref.Version, Title: clean(entry.Title, 2000), Abstract: clean(entry.Description, 20000), PublishedAt: parseTime(entry.PubDate), PDFURL: ref.PDFURL, ArxivURL: ref.ArxivURL, HFURL: ref.HFURL, AlphaXivURL: ref.AlphaXivURL}
		if entry.Creator != "" {
			paper.Authors = append(paper.Authors, clean(entry.Creator, 300))
		}
		for _, author := range entry.Authors {
			if name := clean(author, 300); name != "" && len(paper.Authors) < 200 {
				paper.Authors = append(paper.Authors, name)
			}
		}
		for _, category := range entry.Categories {
			if value := clean(category, 100); value != "" && len(paper.Categories) < 50 {
				paper.Categories = append(paper.Categories, value)
			}
		}
		if paper.Title != "" {
			result = append(result, paper)
		}
	}
	if len(result) == 0 {
		return nil, ErrInvalidPayload
	}
	return result, nil
}

type hfPaper struct {
	ID          string          `json:"id"`
	Paper       *hfPaperPayload `json:"paper"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Authors     []hfAuthor      `json:"authors"`
	Upvotes     int             `json:"upvotes"`
	GitHubRepo  json.RawMessage `json:"githubRepo"`
	GitHubStars int             `json:"githubStars"`
	AISummary   string          `json:"ai_summary"`
	AIKeywords  []string        `json:"ai_keywords"`
}

type hfPaperPayload struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Authors     []hfAuthor      `json:"authors"`
	Upvotes     int             `json:"upvotes"`
	GitHubRepo  json.RawMessage `json:"githubRepo"`
	GitHubStars int             `json:"githubStars"`
	AISummary   string          `json:"ai_summary"`
	AIKeywords  []string        `json:"ai_keywords"`
}

type hfAuthor struct {
	Name string `json:"name"`
}

func ParseHFDaily(data []byte) ([]Paper, error) {
	if len(data) == 0 || len(data) > 16<<20 {
		return nil, ErrInvalidPayload
	}
	var entries []hfPaper
	if err := json.Unmarshal(data, &entries); err != nil || len(entries) == 0 {
		return nil, ErrInvalidPayload
	}
	result := make([]Paper, 0, len(entries))
	for _, entry := range entries {
		id := entry.ID
		if id == "" && entry.Paper != nil {
			id = entry.Paper.ID
		}
		if entry.Paper != nil {
			if entry.Title == "" {
				entry.Title = entry.Paper.Title
			}
			if entry.Summary == "" {
				entry.Summary = entry.Paper.Summary
			}
			if len(entry.Authors) == 0 {
				entry.Authors = entry.Paper.Authors
			}
			if entry.Upvotes == 0 {
				entry.Upvotes = entry.Paper.Upvotes
			}
			if len(entry.GitHubRepo) == 0 && len(entry.Paper.GitHubRepo) > 0 {
				entry.GitHubRepo = entry.Paper.GitHubRepo
			}
			if entry.GitHubStars == 0 {
				entry.GitHubStars = entry.Paper.GitHubStars
			}
			if entry.AISummary == "" {
				entry.AISummary = entry.Paper.AISummary
			}
			if len(entry.AIKeywords) == 0 {
				entry.AIKeywords = entry.Paper.AIKeywords
			}
		}
		ref, err := ParseReference(id)
		if err != nil {
			continue
		}
		paper := Paper{
			ID: ref.ID, Version: ref.Version, Title: clean(entry.Title, 2000), Abstract: clean(entry.Summary, 20000),
			PDFURL: ref.PDFURL, ArxivURL: ref.ArxivURL, HFURL: ref.HFURL, AlphaXivURL: ref.AlphaXivURL,
			Upvotes: max(0, entry.Upvotes), GitHubStars: max(0, entry.GitHubStars), AISummary: clean(entry.AISummary, 20000),
		}
		for _, author := range entry.Authors {
			if name := clean(author.Name, 300); name != "" && len(paper.Authors) < 200 {
				paper.Authors = append(paper.Authors, name)
			}
		}
		for _, keyword := range entry.AIKeywords {
			if value := clean(keyword, 100); value != "" && len(paper.AIKeywords) < 50 {
				paper.AIKeywords = append(paper.AIKeywords, value)
			}
		}
		paper.GitHubURL = githubURL(entry.GitHubRepo)
		if paper.Title != "" {
			result = append(result, paper)
		}
	}
	if len(result) == 0 {
		return nil, ErrInvalidPayload
	}
	return result, nil
}

func githubURL(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return safeGitHubURL(value)
	}
	var object struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &object) == nil {
		if object.URL != "" {
			return safeGitHubURL(object.URL)
		}
		if object.Name != "" {
			return safeGitHubURL("https://github.com/" + object.Name)
		}
	}
	return ""
}

func safeGitHubURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if githubRepoPattern.MatchString(raw) {
		raw = "https://github.com/" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.Port() != "" {
		return ""
	}
	return u.String()
}

func ToItem(paper Paper) content.Item {
	authors := append([]string(nil), paper.Authors...)
	category := strings.Join(paper.Categories, ", ")
	ref := Reference{ID: paper.ID, Version: paper.Version}
	if paper.PDFURL == "" {
		paper.PDFURL = "https://arxiv.org/pdf/" + paper.ID + versionString(paper.Version) + ".pdf"
	}
	if paper.ArxivURL == "" {
		paper.ArxivURL = "https://arxiv.org/abs/" + paper.ID + versionString(paper.Version)
	}
	if paper.HFURL == "" {
		paper.HFURL = "https://huggingface.co/papers/" + paper.ID
	}
	_ = ref
	return content.Item{
		ID: "arxiv:" + paper.ID, Platform: "arxiv", PlatformID: paper.ID, Kind: "pdf", Title: paper.Title,
		Author: strings.Join(authors, ", "), Category: category, URL: paper.PDFURL, MediaURL: paper.PDFURL,
		Description: paper.Abstract, PublishedAt: paper.PublishedAt,
		Paper: &content.PaperMetadata{
			ArxivID: paper.ID, Title: paper.Title, Version: paper.Version, Authors: authors, Categories: append([]string(nil), paper.Categories...),
			Abstract: paper.Abstract, PublishedAt: paper.PublishedAt, UpdatedAt: paper.UpdatedAt, PDFURL: paper.PDFURL,
			ArxivURL: paper.ArxivURL, HFURL: paper.HFURL, GitHubURL: paper.GitHubURL, AlphaXivURL: paper.AlphaXivURL,
			Upvotes: paper.Upvotes, GitHubStars: paper.GitHubStars, AIKeywords: append([]string(nil), paper.AIKeywords...), AISummary: paper.AISummary,
		},
	}
}

func clean(value string, limit int) string {
	value = strings.Join(strings.Fields(html.UnescapeString(value)), " ")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}

func parseTime(raw string) int64 {
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if value, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return value.Unix()
		}
	}
	return 0
}
