package app

import (
	"context"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/subtitles"
	httptransport "tangentry/internal/transport/http"
)

// httpAdapterBundle contains only the concrete translations needed by the
// transport. Keeping adapters separate from dependency mapping makes the
// composition root readable: one file translates an external client, another
// file selects the domain service that should receive it.
type httpAdapterBundle struct {
	accounts              httptransport.AccountDependencies
	bilibiliBrowser       httptransport.BilibiliBrowser
	imageCache            httptransport.ImageCache
	imageVersion          func(string) string
	podcastCaptions       func(context.Context, string) (subtitles.Document, error)
	publicPodcastCaptions func(context.Context, content.Item) (subtitles.Document, error)
	toolStatus            func() downloads.ToolReport
	updateYTDLP           func() error
}

func newHTTPAdapterBundle(p *platforms, downloadsService *downloads.Service) httpAdapterBundle {
	network := p.network
	return httpAdapterBundle{
		accounts:              newHTTPAccountAdapters(p, network),
		bilibiliBrowser:       newHTTPBilibiliBrowser(p),
		imageCache:            newHTTPImageCache(p),
		imageVersion:          newHTTPImageVersion(),
		podcastCaptions:       newHTTPPodcastCaptions(p),
		publicPodcastCaptions: newHTTPPublicPodcastCaptions(network),
		toolStatus:            newHTTPToolStatus(downloadsService, network),
		updateYTDLP:           network.toolkit.UpdateYTDLP,
	}
}
