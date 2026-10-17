// Package articles owns explicitly fetched original text, independent of feeds and notes.
package articles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"tangentry/internal/content"
)

var (
	ErrInvalid     = errors.New("invalid article request")
	ErrNotFound    = errors.New("article not found")
	ErrConflict    = errors.New("article revision conflict")
	ErrBusy        = errors.New("article fetch busy")
	ErrUnavailable = errors.New("article unavailable")
)

type FetchError struct{ Message string }

func (e *FetchError) Error() string { return e.Message }
func (e *FetchError) Unwrap() error { return ErrUnavailable }

// Block is shared with feed snapshots so both subscribed and fetched articles
// use the same safe reader representation.
type Block = content.ArticleBlock
type Document struct {
	ContentID  string  `json:"contentId"`
	SourceURL  string  `json:"sourceUrl"`
	Title      string  `json:"title"`
	Byline     string  `json:"byline"`
	Blocks     []Block `json:"blocks"`
	Text       string  `json:"text"`
	SourceHash string  `json:"sourceHash"`
	Revision   int64   `json:"revision"`
	FetchedAt  int64   `json:"fetchedAt"`
}
type Repository interface {
	ArticleSource(context.Context, string) (content.Item, error)
	GetArticle(context.Context, string) (Document, error)
	SaveArticle(context.Context, Document, int64) (Document, error)
}
type Extractor interface {
	Fetch(context.Context, string) (Document, error)
}
type Service struct {
	repository Repository
	extractor  Extractor
	fetching   chan struct{}
}

func New(repository Repository, extractor Extractor) *Service {
	return &Service{repository: repository, extractor: extractor, fetching: make(chan struct{}, 2)}
}
func validID(id string) bool { return strings.HasPrefix(id, "rss:") && content.ValidID(id) }
func (s *Service) Get(ctx context.Context, id string) (Document, error) {
	if !validID(id) {
		return Document{}, ErrInvalid
	}
	return s.repository.GetArticle(ctx, id)
}
func (s *Service) Fetch(ctx context.Context, id string, expected int64) (Document, error) {
	if !validID(id) || expected < 0 || expected > 1_000_000_000 {
		return Document{}, ErrInvalid
	}
	select {
	case s.fetching <- struct{}{}:
		defer func() { <-s.fetching }()
	default:
		return Document{}, ErrBusy
	}
	old, err := s.repository.GetArticle(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Document{}, err
	}
	if old.Revision != expected {
		return Document{}, ErrConflict
	}
	item, err := s.repository.ArticleSource(ctx, id)
	if err != nil {
		return Document{}, err
	}
	if item.Platform != "rss" || item.Kind != "article" || item.ID != id {
		return Document{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	doc, err := s.extractor.Fetch(ctx, item.URL)
	if err != nil {
		return Document{}, err
	}
	if err = ctx.Err(); err != nil {
		return Document{}, err
	}
	doc.ContentID = id
	doc.Revision = expected + 1
	doc.FetchedAt = time.Now().Unix()
	if doc.Title == "" {
		doc.Title = item.Title
	}
	if err = Prepare(&doc); err != nil {
		return Document{}, err
	}
	return s.repository.SaveArticle(ctx, doc, expected)
}

// Canonical text is also the DOM Range text; offsets keep the existing UTF-16 contract.
func Prepare(doc *Document) error {
	if len(doc.Blocks) == 0 || len(doc.Blocks) > 4000 || len(doc.Title) > 2000 || len(doc.Byline) > 2000 {
		return ErrUnavailable
	}
	parts := make([]string, 0, len(doc.Blocks))
	for _, block := range doc.Blocks {
		switch block.Kind {
		case "paragraph", "quote", "code", "list":
			if block.Level != 0 {
				return ErrUnavailable
			}
		case "heading":
			if block.Level < 2 || block.Level > 4 {
				return ErrUnavailable
			}
		case "image":
			u, err := url.Parse(block.URL)
			if err != nil || len(block.URL) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || block.Width < 0 || block.Height < 0 || block.Width > 30000 || block.Height > 30000 || len(block.Alt) > 1000 {
				return ErrUnavailable
			}
			continue
		default:
			return ErrUnavailable
		}
		if block.Kind != "image" && (!utf8.ValidString(block.Text) || strings.TrimSpace(block.Text) == "") {
			return ErrUnavailable
		}
		parts = append(parts, block.Text)
	}
	doc.Text = strings.Join(parts, "\n\n")
	if len(doc.Text) > 1<<20 {
		return ErrUnavailable
	}
	digest := sha256.Sum256([]byte(doc.Text))
	doc.SourceHash = hex.EncodeToString(digest[:])
	return nil
}
