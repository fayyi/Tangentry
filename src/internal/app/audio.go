package app

import (
	"context"

	"tangentry/internal/transcription"
)

func newAudioResolver(library contentReader, inbox inboxReader, downloads completedDownloadReader, media sourceMedia) transcription.SourceResolver {
	resolver := &sourceResolver{
		content: library, inbox: inbox, downloads: downloads,
		youtube: media.youtube, douyin: media.douyin,
		xiaohongshu: media.xiaohongshu, locator: media.locator,
		podcast: media.podcast, bilibili: media.bilibili,
	}
	return resolver.Resolve
}

func (p *platforms) transcriptionSource(bilibili func(context.Context, string) (transcription.Audio, error)) sourceMedia {
	if bilibili == nil {
		bilibili = p.media.bilibili.Audio
	}
	return sourceMedia{
		youtube: p.media.toolkit.YouTubeAudio, douyin: p.media.toolkit.DouyinAudio,
		xiaohongshu: p.media.toolkit.XiaohongshuAudio, locator: p.media.notes.VideoLocator,
		podcast: p.media.podcasts.Episode, bilibili: bilibili,
	}
}
