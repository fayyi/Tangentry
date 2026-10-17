package app

import (
	"context"

	"tangentry/internal/adapters/tools"
	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/subtitles"
)

func newHTTPPodcastCaptions(p *platforms) func(context.Context, string) (subtitles.Document, error) {
	return func(ctx context.Context, id string) (subtitles.Document, error) {
		return p.discovery.podcasts.Transcript(ctx, id, p.podcastToken())
	}
}

func newHTTPPublicPodcastCaptions(network platformNetwork) func(context.Context, content.Item) (subtitles.Document, error) {
	return func(ctx context.Context, item content.Item) (subtitles.Document, error) {
		return tools.PublicPodcastCaptions(ctx, network.public, item)
	}
}

func newHTTPToolStatus(downloadsService *downloads.Service, network platformNetwork) func() downloads.ToolReport {
	return func() downloads.ToolReport {
		report := network.toolkit.Status()
		report.DownloadDir = downloadsService.Directory()
		return report
	}
}
