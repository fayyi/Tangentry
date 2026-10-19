package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
	"tangentry/internal/playback"
	"tangentry/internal/subscriptions"
	"tangentry/internal/transcription"
)

func TestResolveDouyinPlaybackPrefersWebViewAndFallsBackOnce(t *testing.T) {
	var calls []string
	page := func(context.Context, string) (playback.Source, error) {
		calls = append(calls, "page")
		return playback.Source{URL: "https://v1.douyinvod.com/page.mp4"}, nil
	}
	ytdlp := func(context.Context, string) (playback.Source, error) {
		calls = append(calls, "yt-dlp")
		return playback.Source{URL: "https://v2.douyinvod.com/fallback.mp4"}, nil
	}
	if source, err := resolveDouyinPlayback(context.Background(), "6961737553342991651", page, ytdlp); err != nil || source.Platform != "douyin" || len(calls) != 1 || calls[0] != "page" {
		t.Fatalf("page result=%+v err=%v calls=%v", source, err, calls)
	}
	calls = nil
	page = func(context.Context, string) (playback.Source, error) {
		calls = append(calls, "page")
		return playback.Source{}, errors.New("page unavailable")
	}
	if source, err := resolveDouyinPlayback(context.Background(), "6961737553342991651", page, ytdlp); err != nil || source.Platform != "douyin" || len(calls) != 2 || calls[1] != "yt-dlp" {
		t.Fatalf("fallback result=%+v err=%v calls=%v", source, err, calls)
	}
}

func TestResolveDouyinPlaybackRecordsSuccessfulWebViewAndBlockedFallback(t *testing.T) {
	trace := playback.NewDiagnosticTrace()
	ctx := playback.ContextWithDiagnostics(context.Background(), trace)
	if _, err := resolveDouyinPlayback(ctx, "6961737553342991651", func(context.Context, string) (playback.Source, error) {
		trace.Add(playback.DiagnosticStage{Stage: "webview_session", Outcome: playback.OutcomeOK, Reason: playback.Reason("session_reused")})
		trace.Add(playback.DiagnosticStage{Stage: "webview_page_load", Outcome: playback.OutcomeOK, Reason: playback.Reason("resolver_success")})
		trace.Add(playback.DiagnosticStage{Stage: "webview_detail_fetch", Outcome: playback.OutcomeOK, Reason: playback.Reason("detail_success")})
		trace.Add(playback.DiagnosticStage{Stage: "webview_media_extract", Outcome: playback.OutcomeOK, Reason: playback.Reason("media_found")})
		return playback.Source{URL: "https://v1.douyinvod.com/page.mp4"}, nil
	}, func(context.Context, string) (playback.Source, error) {
		t.Fatal("yt-dlp should not run after WebView success")
		return playback.Source{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	stages := trace.Snapshot().Stages
	if len(stages) == 0 || stages[len(stages)-1].Stage != "resolver_fallback" || stages[len(stages)-1].Reason == nil || *stages[len(stages)-1].Reason != "webview_success" {
		t.Fatalf("WebView success stage missing: %+v", stages)
	}

	restrictedTrace := playback.NewDiagnosticTrace()
	restrictedCtx := playback.ContextWithDiagnostics(context.Background(), restrictedTrace)
	ytdlpCalls := 0
	_, err := resolveDouyinPlayback(restrictedCtx, "6961737553342991651", func(context.Context, string) (playback.Source, error) {
		restrictedTrace.Add(playback.DiagnosticStage{Stage: "webview_page_load", Outcome: playback.OutcomeRestricted, Reason: playback.Reason("navigation_blocked")})
		return playback.Source{}, &discovery.PlatformError{Restricted: true, Message: "needs verification"}
	}, func(context.Context, string) (playback.Source, error) {
		ytdlpCalls++
		return playback.Source{}, nil
	})
	if err == nil || ytdlpCalls != 0 || !errors.Is(err, discovery.ErrUnavailable) {
		t.Fatalf("restricted fallback behavior changed: err=%v ytdlpCalls=%d", err, ytdlpCalls)
	}
	if !strings.Contains(playback.DiagnosticSummary(restrictedTrace.Snapshot().Stages), "安全策略阻止") {
		t.Fatalf("blocked navigation summary missing: %+v", restrictedTrace.Snapshot().Stages)
	}
}

func TestResolveDouyinPlaybackKeepsVerificationAndTimeoutErrors(t *testing.T) {
	ytdlpCalls := 0
	ytdlp := func(context.Context, string) (playback.Source, error) {
		ytdlpCalls++
		return playback.Source{}, errors.New("yt-dlp should not run")
	}
	verification := &discovery.PlatformError{Restricted: true, Message: "应用内抖音页面需要验证"}
	if _, err := resolveDouyinPlayback(context.Background(), "6961737553342991651", func(context.Context, string) (playback.Source, error) {
		return playback.Source{}, verification
	}, ytdlp); !errors.Is(err, verification) || ytdlpCalls != 0 {
		t.Fatalf("verification err=%v ytdlpCalls=%d", err, ytdlpCalls)
	}
	if _, err := resolveDouyinPlayback(context.Background(), "6961737553342991651", func(context.Context, string) (playback.Source, error) {
		return playback.Source{}, context.DeadlineExceeded
	}, ytdlp); !errors.Is(err, context.DeadlineExceeded) || ytdlpCalls != 0 {
		t.Fatalf("timeout err=%v ytdlpCalls=%d", err, ytdlpCalls)
	}
}

func TestResolveDouyinPlaybackKeepsWebViewAndYTDLPStagesOnFailure(t *testing.T) {
	trace := playback.NewDiagnosticTrace()
	ctx := playback.ContextWithDiagnostics(context.Background(), trace)
	webStatus := 403
	webErr := playback.WrapDiagnostic(errors.New("webview request failed"), trace, "detail fetch failed")
	_, err := resolveDouyinPlayback(ctx, "6961737553342991651", func(context.Context, string) (playback.Source, error) {
		trace.Add(playback.DiagnosticStage{Stage: "webview_detail_fetch", Outcome: playback.OutcomeHTTPError, HTTPStatus: &webStatus})
		return playback.Source{}, webErr
	}, func(context.Context, string) (playback.Source, error) {
		exitCode := 1
		stderr := "ERROR: extractor failed"
		trace.Add(playback.DiagnosticStage{Stage: "ytdlp", Outcome: playback.OutcomeException, YTDLPExitCode: &exitCode, YTDLPStderrFirstErrorRedacted: &stderr})
		return playback.Source{}, errors.New("yt-dlp failed")
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "yt-dlp 退出码 1") {
		t.Fatalf("combined resolver message = %v", err)
	}
	var diagnostic *playback.DiagnosticError
	if !errors.As(err, &diagnostic) || len(diagnostic.Diagnostics().Stages) < 3 {
		t.Fatalf("diagnostic stages lost: %v", err)
	}
}

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
