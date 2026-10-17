package app

import (
	"context"
	"errors"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/subscriptions"
	"tangentry/internal/transcription"
)

type sourceContentFunc func(context.Context, string) (content.Item, error)

func (f sourceContentFunc) Get(ctx context.Context, id string) (content.Item, error) {
	return f(ctx, id)
}

type sourceInboxFunc func(context.Context, string) (content.Item, error)

func (f sourceInboxFunc) Item(ctx context.Context, id string) (content.Item, error) {
	return f(ctx, id)
}

type sourceDownloadFunc func(context.Context, string) (string, error)

func (f sourceDownloadFunc) CompletedPath(ctx context.Context, raw string) (string, error) {
	return f(ctx, raw)
}

func TestSourceResolverUsesSavedURLAndArchivedInbox(t *testing.T) {
	ctx := context.Background()
	const signed = "https://www.xiaohongshu.com/explore/note123?xsec_token=fixture"
	resolver := sourceResolver{
		content: sourceContentFunc(func(context.Context, string) (content.Item, error) {
			return content.Item{ID: "xiaohongshu:note123", Kind: "video", URL: signed}, nil
		}),
		inbox: sourceInboxFunc(func(context.Context, string) (content.Item, error) {
			t.Fatal("unexpected inbox lookup")
			return content.Item{}, nil
		}),
		downloads: sourceDownloadFunc(func(_ context.Context, raw string) (string, error) {
			if raw != "https://www.xiaohongshu.com/explore/note123" {
				t.Fatalf("download lookup URL = %q", raw)
			}
			return "", downloads.ErrNotFound
		}),
		locator: func(string) string { return "https://www.xiaohongshu.com/explore/note123" },
		xiaohongshu: func(_ context.Context, id, raw string) (transcription.Audio, error) {
			if id != "note123" || raw != signed {
				t.Fatalf("platform lookup = %q %q", id, raw)
			}
			return transcription.Audio{URL: "https://sns-video-qc.xhscdn.com/audio.mp4"}, nil
		},
	}
	audio, err := resolver.Resolve(ctx, "xiaohongshu:note123")
	if err != nil || audio.URL != "https://sns-video-qc.xhscdn.com/audio.mp4" {
		t.Fatalf("audio=%+v err=%v", audio, err)
	}

	const id = "rss:feeditem"
	resolver = sourceResolver{
		content: sourceContentFunc(func(context.Context, string) (content.Item, error) { return content.Item{}, content.ErrNotFound }),
		inbox: sourceInboxFunc(func(_ context.Context, got string) (content.Item, error) {
			if got != id {
				t.Fatalf("inbox id = %q", got)
			}
			return content.Item{ID: id, Kind: "audio", MediaURL: "https://media.example/episode.mp3", DurationSeconds: 42}, nil
		}),
		downloads: sourceDownloadFunc(func(context.Context, string) (string, error) { return "", downloads.ErrNotFound }),
	}
	audio, err = resolver.Resolve(ctx, id)
	if err != nil || audio.URL != "https://media.example/episode.mp3" || audio.Duration != 42 {
		t.Fatalf("archived audio=%+v err=%v", audio, err)
	}
}

func TestSourceResolverSurfacesInboxFailures(t *testing.T) {
	want := errors.New("inbox unavailable")
	resolver := sourceResolver{
		content:   sourceContentFunc(func(context.Context, string) (content.Item, error) { return content.Item{}, content.ErrNotFound }),
		inbox:     sourceInboxFunc(func(context.Context, string) (content.Item, error) { return content.Item{}, want }),
		downloads: sourceDownloadFunc(func(context.Context, string) (string, error) { return "", downloads.ErrNotFound }),
	}
	if _, err := resolver.Resolve(context.Background(), "rss:feeditem"); !errors.Is(err, want) || errors.Is(err, subscriptions.ErrInboxNotFound) {
		t.Fatalf("error = %v", err)
	}
}
