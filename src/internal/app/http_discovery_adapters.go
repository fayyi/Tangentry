package app

import (
	"context"

	"tangentry/internal/adapters/platforms/bilibili"
	xhs "tangentry/internal/adapters/platforms/xiaohongshu"
	httptransport "tangentry/internal/transport/http"
)

type bilibiliBrowser struct{ client *bilibili.Client }
type imageCache struct{ cache *xhs.Images }

func newHTTPBilibiliBrowser(p *platforms) httptransport.BilibiliBrowser {
	return bilibiliBrowser{p.discovery.bilibili}
}

func newHTTPImageCache(p *platforms) httptransport.ImageCache {
	return imageCache{p.imports.noteImages}
}

func newHTTPImageVersion() func(string) string { return xhs.ImageVersion }

func (b bilibiliBrowser) Browse(ctx context.Context, kind string, page int, cursor string, area int) (httptransport.BrowsePage, error) {
	value, err := b.client.Browse(ctx, kind, page, cursor, area)
	result := httptransport.BrowsePage{Items: value.Items, Next: value.Next, HasMore: value.HasMore, Page: value.Page, SourceTitle: value.SourceTitle}
	for _, room := range value.Rooms {
		result.Rooms = append(result.Rooms, httptransport.LiveRoom{ID: room.ID, Title: room.Title, Author: room.Author, Thumbnail: room.Thumbnail, URL: room.URL})
	}
	return result, err
}

func (b bilibiliBrowser) Profile(ctx context.Context) (httptransport.Profile, error) {
	value, err := b.client.Profile(ctx)
	return httptransport.Profile{LoggedIn: value.LoggedIn, ID: value.ID, Name: value.Name, Avatar: value.Avatar, Level: value.Level}, err
}

func (c imageCache) Read(ctx context.Context, raw string) (httptransport.ImageData, error) {
	value, err := c.cache.Read(ctx, raw)
	return httptransport.ImageData{Bytes: value.Bytes, Type: value.Type}, err
}

var _ httptransport.BilibiliBrowser = bilibiliBrowser{}
var _ httptransport.ImageCache = imageCache{}
