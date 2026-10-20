package app

import (
	"context"
	"errors"
	"testing"

	"tangentry/internal/subtitles"
)

type providerFixture struct {
	name    string
	doc     subtitles.Document
	err     error
	readIDs *[]string
}

func (p providerFixture) ReadSubtitles(_ context.Context, id string) (subtitles.Document, error) {
	if p.readIDs != nil {
		*p.readIDs = append(*p.readIDs, id)
	}
	if p.err != nil {
		return p.doc, p.err
	}
	if p.doc.VideoID != "" {
		return p.doc, nil
	}
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

func TestSubtitleProviderFallsBackToYTDLPWhenNativeHasNoTracks(t *testing.T) {
	for _, test := range []struct {
		name   string
		native providerFixture
		tool   providerFixture
	}{
		{name: "native none", native: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "none", Tracks: []subtitles.Track{}}}, tool: providerFixture{doc: providerFallbackDocument()}},
		{name: "native api empty", native: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "api_empty", Tracks: []subtitles.Track{}}}, tool: providerFixture{doc: providerFallbackDocument()}},
		{name: "native error", native: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "error", ErrorCode: -101, Tracks: []subtitles.Track{}}}, tool: providerFixture{doc: providerFallbackDocument()}},
		{name: "native rate limited", native: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "rate_limited", ErrorCode: 412, Tracks: []subtitles.Track{}}}, tool: providerFixture{doc: providerFallbackDocument()}},
		{name: "native reader error", native: providerFixture{err: errors.New("fixture native failure")}, tool: providerFixture{doc: providerFallbackDocument()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := subtitleProvider{bilibili: test.native, ytdlp: test.tool}
			doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
			if err != nil || doc.Status != "ai" || len(doc.Tracks) != 1 || doc.Tracks[0].Source != "yt-dlp" || !doc.Tracks[0].Automatic {
				t.Fatalf("yt-dlp track did not replace empty native result: %+v %v", doc, err)
			}
			if len(doc.Parts) != 1 || doc.Parts[0].Tracks[0].Source != "yt-dlp" {
				t.Fatalf("fallback part was not updated: %+v", doc.Parts)
			}
		})
	}
}

func TestSubtitleProviderDoesNotDowngradeNativeStateWhenYTDLPConfirmsNoTracks(t *testing.T) {
	for _, status := range []string{"api_empty", "error", "rate_limited"} {
		t.Run(status, func(t *testing.T) {
			native := subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: status, ErrorCode: 412, ErrorMessage: "fixture status", Tracks: []subtitles.Track{}}
			provider := subtitleProvider{
				bilibili: providerFixture{doc: native},
				ytdlp:    providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "none", Tracks: []subtitles.Track{}}},
			}
			doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
			if err != nil || doc.Status != status || doc.ErrorCode != 412 || doc.ErrorMessage != "fixture status" {
				t.Fatalf("native state was replaced without dual confirmation: %+v %v", doc, err)
			}
		})
	}
}

func TestSubtitleProviderDoesNotReplaceNativeErrorWithNoPartInformation(t *testing.T) {
	nativeErr := errors.New("fixture native error")
	provider := subtitleProvider{bilibili: providerFixture{err: nativeErr}, ytdlp: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Status: "none", Tracks: []subtitles.Track{}}}}
	doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if !errors.Is(err, nativeErr) || doc.Status != "" {
		t.Fatalf("native failure was hidden by inconclusive fallback: %+v %v", doc, err)
	}
}

func TestSubtitleProviderKeepsNativeFailureWhenFallbackDoesNotConfirmCaptions(t *testing.T) {
	native := subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "rate_limited", ErrorCode: 412, ErrorMessage: "fixture restriction", Tracks: []subtitles.Track{}}
	provider := subtitleProvider{
		bilibili: providerFixture{doc: native},
		ytdlp:    providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Status: "none", Tracks: []subtitles.Track{}}},
	}
	doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || doc.Status != "rate_limited" || doc.ErrorCode != 412 || doc.ErrorMessage != "fixture restriction" {
		t.Fatalf("native status was not preserved: %+v %v", doc, err)
	}
	provider.ytdlp = providerFixture{err: errors.New("fixture tool failure")}
	doc, err = provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || doc.Status != "rate_limited" || doc.ErrorMessage != "fixture restriction" {
		t.Fatalf("native status was not preserved after tool failure: %+v %v", doc, err)
	}
}

func TestSubtitleProviderReturnsNoneOnlyWhenBothPathsHaveNoTracks(t *testing.T) {
	provider := subtitleProvider{
		bilibili: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "none", Tracks: []subtitles.Track{}}},
		ytdlp:    providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "none", Tracks: []subtitles.Track{}}},
	}
	doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatalf("confirmed absence was not preserved: %+v %v", doc, err)
	}
}

func TestSubtitleProviderPreservesNativeNoneWhenYTDLPFails(t *testing.T) {
	provider := subtitleProvider{
		bilibili: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "none", Tracks: []subtitles.Track{}}},
		ytdlp:    providerFixture{err: errors.New("fixture tool unavailable")},
	}
	doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || doc.Status != "none" || len(doc.Tracks) != 0 {
		t.Fatalf("native state was not preserved after tool failure: %+v %v", doc, err)
	}
}

func TestSubtitleProviderDoesNotUseYTDLPWhenNativeHasTracks(t *testing.T) {
	var fallbackCalls []string
	provider := subtitleProvider{
		bilibili: providerFixture{doc: subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "ai", Tracks: []subtitles.Track{{Language: "ai-zh", Source: "ai", Automatic: true, Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "native"}}}}}},
		ytdlp:    providerFixture{readIDs: &fallbackCalls},
	}
	doc, err := provider.ReadSubtitles(context.Background(), "BV17x411w7KC")
	if err != nil || len(doc.Tracks) != 1 || doc.Tracks[0].Source != "ai" || len(fallbackCalls) != 0 {
		t.Fatalf("yt-dlp ran despite native tracks: %+v %v calls=%v", doc, err, fallbackCalls)
	}
}

func providerFallbackDocument() subtitles.Document {
	return subtitles.Document{VideoID: "BV17x411w7KC", Part: 1, Status: "available", Tracks: []subtitles.Track{{Language: "zh-CN", Label: "简体中文", Automatic: true, Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "夹具字幕"}}}}}
}
