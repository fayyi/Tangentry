// Package feeds translates RSS/Atom documents into Tangentry's own content model.
package feeds

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"tangentry/internal/content"
	"tangentry/internal/discovery"

	"github.com/mmcdole/gofeed"
)

type Client struct {
	HTTP *http.Client
	// Optional protocol-specific mapping; bounded I/O and cache validation stay here.
	Parser         func([]byte, string) (discovery.Feed, error)
	previewMu      sync.Mutex
	previews       map[string]preview
	previewPending map[string]chan struct{}
}

const maxFeedBytes = 16 << 20

func (c *Client) Fetch(ctx context.Context, raw string) (discovery.Feed, error) {
	result, err := c.FetchConditional(ctx, raw, Validators{})
	return result.Feed, err
}
func (c *Client) parseResponse(ctx context.Context, response *http.Response, canonical string) (discovery.Feed, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, maxFeedBytes+1))
	if ctx.Err() != nil {
		return discovery.Feed{}, ctx.Err()
	}
	if err != nil {
		return discovery.Feed{}, &FetchError{Message: "订阅源响应读取中断，已有内容保留，请稍后更新。"}
	}
	if len(data) > maxFeedBytes {
		return discovery.Feed{}, &FetchError{Message: "订阅源响应超过 16 MB，请减少源返回的条目或正文数量。"}
	}
	if c.Parser != nil {
		return c.Parser(data, canonical)
	}
	return Parse(data, canonical)
}
func Parse(data []byte, canonical string) (discovery.Feed, error) {
	parser := gofeed.NewParser()
	parser.AtomTranslator = &readingAtomTranslator{}
	parser.JSONTranslator = &readingJSONTranslator{}
	f, err := parser.ParseString(string(data))
	if err != nil {
		if htmlResponse(data) {
			return discovery.Feed{}, &FetchError{Message: "订阅源返回了网页而不是 RSS / Atom，可能是登录页、错误页或反向代理页面。请检查订阅地址、RSSHub 路由与鉴权配置。"}
		}
		return discovery.Feed{}, &discovery.PlatformError{Message: "这不是可读取的 RSS / Atom 订阅地址，请使用订阅源链接。"}
	}
	items := []content.Item{}
	resolveRSSEnclosures(data, f)
	transcripts := podcastTranscripts(data, f)
	seen := map[string]bool{}
	for entryIndex, entry := range f.Items {
		if len(items) >= 100 {
			break
		}
		kind, media := enclosureMedia(entry.Enclosures)
		url := link(entry.Link)
		if url == "" {
			url = media
		}
		if url == "" {
			continue
		}
		identity := entry.GUID
		if identity == "" {
			identity = url
		}
		id := Hash(canonical + "\n" + identity)
		if seen[id] {
			continue
		}
		seen[id] = true
		title := Text(entry.Title)
		if title == "" {
			title = url
		}
		author := f.Title
		if entry.Author != nil && entry.Author.Name != "" {
			author = entry.Author.Name
		}
		body := entry.Content
		if summaryText(body) == "" {
			body = entry.Description
		}
		thumbnail := ""
		if entry.Image != nil {
			thumbnail = link(entry.Image.URL)
		}
		if kind == "image" && media != "" {
			thumbnail = media
		}
		duration, _ := strconv.Atoi(entry.Custom["readingDuration"])
		if entry.ITunesExt != nil {
			duration = podcastDuration(entry.ITunesExt.Duration)
			if thumbnail == "" {
				thumbnail = link(entry.ITunesExt.Image)
			}
		}
		if thumbnail == "" && f.Image != nil {
			thumbnail = link(f.Image.URL)
		}
		if thumbnail == "" && f.ITunesExt != nil {
			thumbnail = link(f.ITunesExt.Image)
		}
		var published int64
		if entry.PublishedParsed != nil {
			published = entry.PublishedParsed.Unix()
		} else if entry.UpdatedParsed != nil {
			published = entry.UpdatedParsed.Unix()
		}
		description := summaryText(body)
		if kind == "article" && description != "" {
			description = textLimit(body, 1<<20)
		}
		var articleBlocks []content.ArticleBlock
		if kind == "article" && body != "" {
			articleBlocks = parseArticleBlocks(body, url)
			if thumbnail == "" {
				for _, block := range articleBlocks {
					if block.Kind == "image" {
						thumbnail = block.URL
						break
					}
				}
			}
		}
		item := content.Item{ID: "rss:" + id, Platform: "rss", PlatformID: id, Kind: kind, Title: title, Author: Text(author), URL: url, ThumbnailURL: thumbnail, Description: description, MediaURL: media, DurationSeconds: duration, PublishedAt: published, ArticleBlocks: articleBlocks}
		if kind == "audio" && len(transcripts) == len(f.Items) {
			item.Transcripts = transcripts[entryIndex]
		}
		item.AuthorAvatarURL = entry.Custom["readingAvatar"]
		if content.Validate(item) != nil {
			continue
		}
		items = append(items, item)
	}
	return discovery.Feed{Items: items, Page: 1, Total: len(items), SourceTitle: Text(f.Title), Query: canonical}, nil
}
