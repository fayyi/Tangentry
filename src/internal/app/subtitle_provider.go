package app

import (
	"context"
	"strings"

	"tangentry/internal/subtitles"
)

type subtitleProvider struct {
	bilibili subtitles.Provider
	ytdlp    subtitles.Provider
	youtube  subtitles.YouTubeProvider
}

func (p subtitleProvider) ReadSubtitles(ctx context.Context, id string) (subtitles.Document, error) {
	doc, err := p.bilibili.ReadSubtitles(ctx, id)
	if p.ytdlp == nil || err == nil && len(doc.Tracks) > 0 {
		return doc, err
	}
	if err == nil && (doc.Status == "login_required" || doc.Status == "rate_limited" || doc.Status == "error" || doc.Status == "api_empty") {
		fallback, fallbackErr := p.ytdlp.ReadSubtitles(ctx, id)
		if fallbackErr != nil || len(fallback.Tracks) == 0 {
			return doc, nil
		}
		return p.replaceWithYTDLP(doc, fallback, id), nil
	}
	fallback, fallbackErr := p.ytdlp.ReadSubtitles(ctx, id)
	if fallbackErr != nil || len(fallback.Tracks) == 0 {
		return doc, err
	}
	return p.replaceWithYTDLP(doc, fallback, id), nil
}

func (p subtitleProvider) replaceWithYTDLP(doc, fallback subtitles.Document, id string) subtitles.Document {
	for i := range fallback.Tracks {
		fallback.Tracks[i].Source = "yt-dlp"
		fallback.Tracks[i].Mismatch = false
	}
	if len(fallback.Languages) == 0 {
		fallback.Languages = make([]subtitles.Language, 0, len(fallback.Tracks))
		for _, track := range fallback.Tracks {
			fallback.Languages = append(fallback.Languages, subtitles.Language{Language: track.Language, Label: track.Label, Automatic: track.Automatic, Source: "yt-dlp", HasURL: true})
		}
	} else {
		for i := range fallback.Languages {
			fallback.Languages[i].Source = "yt-dlp"
			fallback.Languages[i].HasURL = true
		}
	}
	fallback.VideoID, fallback.Part = doc.VideoID, doc.Part
	if doc.VideoID == "" {
		fallback.VideoID = id
	}
	if doc.Part < 1 {
		fallback.Part = 1
	}
	fallback.Status = fallbackPartStatus(fallback.Tracks[0])
	fallback.ErrorCode, fallback.ErrorMessage = 0, ""
	if len(doc.Parts) > 0 {
		first := doc.Parts[0]
		first.Status = fallback.Status
		first.ErrorCode, first.ErrorMessage = 0, ""
		first.Languages, first.Tracks = fallback.Languages, fallback.Tracks
		fallback.Parts = append([]subtitles.Part(nil), doc.Parts...)
		fallback.Parts[0] = first
	} else {
		fallback.Parts = []subtitles.Part{{Page: fallback.Part, Status: fallback.Status, Languages: fallback.Languages, Tracks: fallback.Tracks}}
	}
	fallback.Languages = fallback.Parts[0].Languages
	return fallback
}

func fallbackPartStatus(track subtitles.Track) string {
	if track.Automatic || strings.HasPrefix(strings.ToLower(track.Language), "ai-") {
		return "ai"
	}
	return "cc"
}

func (p subtitleProvider) ReadYouTubeSubtitles(ctx context.Context, id, language string) (subtitles.Document, error) {
	return p.youtube.ReadYouTubeSubtitles(ctx, id, language)
}
