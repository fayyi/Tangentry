package app

import (
	"context"
	"testing"

	"tangentry/internal/subtitles"
)

type providerFixture struct{ name string }

func (p providerFixture) ReadSubtitles(context.Context, string) (subtitles.Document, error) {
	return subtitles.Document{VideoID: p.name}, nil
}

func (p providerFixture) ReadYouTubeSubtitles(context.Context, string, string) (subtitles.Document, error) {
	return subtitles.Document{VideoID: p.name}, nil
}

func TestSubtitleProviderRoutesPlatformAndYouTubeToTheirOwnReaders(t *testing.T) {
	provider := subtitleProvider{bilibili: providerFixture{name: "native-bilibili"}, youtube: providerFixture{name: "ytdlp-youtube"}}
	bilibili, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || bilibili.VideoID != "native-bilibili" {
		t.Fatalf("B站 was not routed to native reader: %+v, %v", bilibili, err)
	}
	youtube, err := provider.ReadYouTubeSubtitles(context.Background(), "jNQXAC9IVRw", "en")
	if err != nil || youtube.VideoID != "ytdlp-youtube" {
		t.Fatalf("YouTube was not routed to yt-dlp reader: %+v, %v", youtube, err)
	}
}
