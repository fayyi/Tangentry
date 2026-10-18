package discovery

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"tangentry/internal/content"
)

// SearchPage is the small, platform-neutral page returned by a keyword
// adapter. Cursor is opaque to callers and belongs to the adapter.
type SearchPage struct {
	Items    []content.Item `json:"items"`
	Cursor   string         `json:"cursor,omitempty"`
	HasMore  bool           `json:"hasMore"`
	Platform string         `json:"platform"`
}

type SearchProvider interface {
	Search(context.Context, string, string) (SearchPage, error)
}

type CreatorSearchProvider interface {
	Creator(context.Context, string, string) (SearchPage, error)
}

type SearchSource struct {
	Page  SearchPage `json:"page"`
	Error string     `json:"error,omitempty"`
}

type SearchResponse struct {
	Query   string                  `json:"query"`
	Local   content.Page            `json:"local"`
	Sources map[string]SearchSource `json:"sources"`
	Link    *Feed                   `json:"link,omitempty"`
}

type SearchOptions struct {
	Platform string
	Kind     string
	From     int64
	To       int64
	Cursor   string
}

// SearchService fans out one request per platform. A failed platform is
// isolated in Sources so local results and other platforms remain useful.
type SearchService struct {
	Local      func(context.Context, content.Query) (content.Page, error)
	Subscribed func(context.Context) (Feed, error)
	Providers  map[string]SearchProvider
	Creators   map[string]CreatorSearchProvider
	Link       func(context.Context, string) (Feed, error)
	cacheMu    sync.Mutex
	cache      map[string]searchCacheEntry
	ttl        time.Duration
}

func (s *SearchService) Creator(ctx context.Context, platform, id, cursor string) (SearchPage, error) {
	provider := s.Creators[platform]
	if provider == nil {
		return SearchPage{Platform: platform}, &PlatformError{Message: "该平台暂不支持创作者临时视图。"}
	}
	return provider.Creator(ctx, id, cursor)
}

type searchCacheEntry struct {
	at     time.Time
	result SearchResponse
}

func NewSearchService(local func(context.Context, content.Query) (content.Page, error), providers map[string]SearchProvider) *SearchService {
	copyProviders := make(map[string]SearchProvider, len(providers))
	for name, provider := range providers {
		copyProviders[name] = provider
	}
	return &SearchService{Local: local, Providers: copyProviders, cache: map[string]searchCacheEntry{}, ttl: 20 * time.Second}
}

func (s *SearchService) Search(ctx context.Context, query string, options SearchOptions) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 200 {
		return SearchResponse{}, ErrInvalidQuery
	}
	if ctx.Err() != nil {
		return SearchResponse{}, ctx.Err()
	}
	key := query + "\x00" + options.Platform + "\x00" + options.Kind + "\x00" + strconv.FormatInt(options.From, 10) + "\x00" + strconv.FormatInt(options.To, 10) + "\x00" + options.Cursor
	s.cacheMu.Lock()
	if cached, ok := s.cache[key]; ok && time.Since(cached.at) < s.ttl {
		s.cacheMu.Unlock()
		return cached.result, nil
	}
	s.cacheMu.Unlock()
	result := SearchResponse{Query: query, Sources: map[string]SearchSource{}}
	if s.Local != nil {
		page, err := s.Local(ctx, content.Query{Q: query, Platform: options.Platform, Kind: options.Kind, Limit: 50})
		if err != nil {
			return SearchResponse{}, err
		}
		// The repository already applies the full-text query, including body and
		// subtitle columns that card projections intentionally omit.
		page.Items = filterItems(page.Items, "", options)
		page.Total = len(page.Items)
		result.Local = page
	}
	if s.Subscribed != nil && options.Platform == "" {
		if feed, err := s.Subscribed(ctx); err == nil {
			result.Local.Items = append(result.Local.Items, filterItems(feed.Items, query, options)...)
			result.Local.Items = filterItems(result.Local.Items, query, options)
			result.Local.Total = len(result.Local.Items)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for platform, provider := range s.Providers {
		if options.Platform != "" && options.Platform != platform {
			continue
		}
		platform, provider := platform, provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			platformCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			cursor := ""
			if options.Platform == platform {
				cursor = options.Cursor
			}
			page, err := provider.Search(platformCtx, query, cursor)
			if err == nil {
				page = filterPage(page, options)
			}
			source := SearchSource{Page: page}
			if err != nil {
				source.Error = err.Error()
			}
			mu.Lock()
			result.Sources[platform] = source
			mu.Unlock()
		}()
	}
	wg.Wait()
	if kind, _, err := ParseQuery(query); err == nil && kind != "" && s.Link != nil {
		if feed, linkErr := s.Link(ctx, query); linkErr == nil {
			result.Link = &feed
		} else if !errors.Is(linkErr, ErrInvalidQuery) {
			result.Sources["link"] = SearchSource{Error: linkErr.Error()}
		}
	}
	s.cacheMu.Lock()
	s.cache[key] = searchCacheEntry{at: time.Now(), result: result}
	s.cacheMu.Unlock()
	return result, nil
}

func filterItems(items []content.Item, query string, options SearchOptions) []content.Item {
	terms := strings.Fields(strings.ToLower(query))
	result := make([]content.Item, 0)
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.ID] || options.Kind != "" && item.Kind != options.Kind || options.From > 0 && item.PublishedAt < options.From || options.To > 0 && item.PublishedAt > options.To {
			continue
		}
		text := strings.ToLower(strings.Join([]string{item.Title, item.Author, item.Description, item.PreviewText}, "\n"))
		ok := true
		for _, term := range terms {
			if !strings.Contains(text, term) {
				ok = false
				break
			}
		}
		if ok {
			seen[item.ID] = true
			result = append(result, item)
		}
	}
	return result
}

func filterPage(page SearchPage, options SearchOptions) SearchPage {
	page.Items = filterItems(page.Items, "", options)
	if len(page.Items) == 0 && page.HasMore {
		// Keep the adapter cursor intact. A later page may contain matching items.
		return page
	}
	return page
}

// CreatorView reads a temporary creator view. It does not mutate subscriptions
// or recommendation preferences.
type CreatorProvider interface {
	Creator(context.Context, string) (Feed, error)
}

func Creator(provider CreatorProvider) func(context.Context, string) (Feed, error) {
	return provider.Creator
}
