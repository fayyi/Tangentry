package app

import (
	"context"

	"tangentry/internal/subtitles"
)

type subtitleProvider struct {
	bilibili subtitles.Provider
	youtube  subtitles.YouTubeProvider
}

func (p subtitleProvider) ReadSubtitles(ctx context.Context, id string) (subtitles.Document, error) {
	return p.bilibili.ReadSubtitles(ctx, id)
}

func (p subtitleProvider) ReadYouTubeSubtitles(ctx context.Context, id, language string) (subtitles.Document, error) {
	return p.youtube.ReadYouTubeSubtitles(ctx, id, language)
}
