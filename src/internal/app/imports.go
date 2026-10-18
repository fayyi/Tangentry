package app

import (
	"context"
	"log/slog"
	"strings"

	paperadapter "tangentry/internal/adapters/papers"
	"tangentry/internal/adapters/platforms/discourse"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	papersdomain "tangentry/internal/papers"
)

// contentImporter resolves pasted addresses using platform clients and the
// small persistence surface needed to reuse signed Xiaohongshu URLs.
type contentImporter struct {
	platforms platformImports
	records   xiaohongshuRecords
	documents documentFetcher
	papers    *paperadapter.Client
}

// importSource resolves one pasted address into a feed. RSS returns the whole
// channel; every other source resolves to a single item.
func (i contentImporter) importSource(ctx context.Context, source, raw string) (discovery.Feed, error) {
	if source == "rss" {
		feed, err := i.platforms.feeds.Fetch(ctx, raw)
		if err == nil {
			i.platforms.feeds.EnrichSummaries(ctx, feed.Items)
		}
		return feed, err
	}
	item, err := i.importItem(ctx, source, raw)
	if err != nil {
		return discovery.Feed{}, err
	}
	return discovery.Feed{Items: []content.Item{item}, Page: 1, Total: 1, SourceTitle: item.Author, Query: raw}, nil
}

func (i contentImporter) importItem(ctx context.Context, source, raw string) (content.Item, error) {
	p := i.platforms
	switch source {
	case "linuxdo":
		id, err := discourse.ParseTopicURL(raw)
		if err != nil {
			return content.Item{}, err
		}
		return p.forum.Topic(ctx, id)
	case "xiaohongshu":
		return i.importXiaohongshu(ctx, raw)
	case "douyin":
		ref, err := p.douyin.Resolve(ctx, raw)
		if err != nil {
			return content.Item{}, err
		}
		return p.toolkit.Douyin(ctx, ref.URL)
	case "youtube":
		return p.toolkit.YouTube(ctx, raw)
	case "xiaoyuzhou":
		return p.podcasts.Episode(ctx, raw)
	case "pdf":
		return i.documents.Fetch(ctx, raw)
	case "paper":
		return i.importPaper(ctx, raw)
	default:
		return content.Item{}, discovery.ErrInvalidQuery
	}
}

func (i contentImporter) importPaper(ctx context.Context, raw string) (content.Item, error) {
	if i.papers == nil || i.documents == nil {
		return content.Item{}, discovery.ErrUnavailable
	}
	item, err := i.papers.Fetch(ctx, raw)
	if err != nil {
		return content.Item{}, err
	}
	ref, err := papersdomain.ParseReference(raw)
	if err != nil {
		return content.Item{}, discovery.ErrInvalidQuery
	}
	document, err := i.documents.Fetch(ctx, ref.PDFURL)
	if err != nil {
		return content.Item{}, err
	}
	item.Paper.DocumentID = document.PlatformID
	item.URL = document.URL
	item.MediaURL = document.URL
	return item, nil
}

// importXiaohongshu reuses a previously stored signed address when the pasted
// one lacks the xsec_token the platform requires for detail reads.
func (i contentImporter) importXiaohongshu(ctx context.Context, raw string) (content.Item, error) {
	p := i.platforms
	if ref, err := discovery.ParseXiaohongshu(raw); err == nil && ref.ID != "" && !strings.Contains(ref.URL, "xsec_token=") {
		if previous, _, readErr := i.records.XiaohongshuDetail(ctx, "xiaohongshu:"+ref.ID); readErr == nil && previous.ID != "" && strings.Contains(previous.URL, "xsec_token=") {
			raw = previous.URL
		}
	}
	item, err := p.notes.Note(ctx, raw)
	if err != nil {
		return content.Item{}, err
	}
	if saveErr := i.records.SaveXiaohongshuDetail(ctx, item); saveErr != nil {
		slog.Warn("Xiaohongshu detail could not be saved")
	}
	return item, nil
}

// refreshXiaohongshu re-reads a note's details and stores them for later views.
func (i contentImporter) refreshXiaohongshu(ctx context.Context, id, raw string) (content.Item, error) {
	p := i.platforms
	item, err := p.notes.NoteFor(ctx, strings.TrimPrefix(id, "xiaohongshu:"), raw)
	if err != nil {
		return content.Item{}, err
	}
	if err := i.records.SaveXiaohongshuDetail(ctx, item); err != nil {
		return content.Item{}, err
	}
	return item, nil
}
