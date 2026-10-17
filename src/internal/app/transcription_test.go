package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/content"
	"tangentry/internal/subscriptions"
	"testing"
	"testing/fstest"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/downloads"
	"tangentry/internal/subtitles"
	"tangentry/internal/transcription"
)

type sourceRecordingEngine struct{ sources chan transcription.Audio }

func (e sourceRecordingEngine) Status(c transcription.Config) transcription.Status {
	return transcription.Status{Config: c, Ready: true}
}
func (e sourceRecordingEngine) Transcribe(ctx context.Context, _ transcription.Config, audio transcription.Audio, _ int, stage func(string)) (subtitles.Track, error) {
	select {
	case e.sources <- audio:
	case <-ctx.Done():
		return subtitles.Track{}, ctx.Err()
	}
	stage("recognizing")
	return subtitles.Track{Label: "明确标识的转录测试", Cues: []subtitles.Cue{{Start: 0, End: 1, Text: "本地媒体"}}}, nil
}

func TestTranscriptionPrefersCompletedDownloadsBeforeAnyPlatformResolution(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{
		"TANGENTRY_DOWNLOAD_DIR":         filepath.Join(root, "downloads"),
		"TANGENTRY_YTDLP_PATH":           filepath.Join(root, "absent-downloader"),
		"TANGENTRY_FFMPEG_PATH":          filepath.Join(root, "absent-converter"),
		"TANGENTRY_BILIBILI_COOKIE_FILE": "", "TANGENTRY_BILIBILI_COOKIE": "",
	} {
		t.Setenv(name, value)
	}
	engine := sourceRecordingEngine{sources: make(chan transcription.Audio, 1)}
	a, err := New(root, fstest.MapFS{}, Options{TranscriptionEngine: engine, BilibiliAudio: func(context.Context, string) (transcription.Audio, error) {
		return transcription.Audio{URL: "https://fixture.bilivideo.com/fresh-audio", Platform: "bilibili"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	store, err := sqlite.Open(filepath.Join(root, "tangentry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, value := range []struct{ id, url string }{
		{"bilibili:BV17x411w7KC", "https://www.bilibili.com/video/BV17x411w7KC/"},
		{"youtube:jNQXAC9IVRw", "https://www.youtube.com/watch?v=jNQXAC9IVRw"},
	} {
		downloadID := "completed-" + value.id[:3]
		directory := filepath.Join(root, "downloads", downloadID)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "video.mp4")
		if err := os.WriteFile(path, []byte("明确标识的媒体夹具"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDownload(context.Background(), downloads.Job{ID: downloadID, URL: value.url, State: "completed", FileName: "video.mp4"}); err != nil {
			t.Fatal(err)
		}
		job := startTranscription(t, a, value.id)
		select {
		case audio := <-engine.sources:
			if audio.LocalPath != path || audio.URL != "" {
				t.Fatal("platform queried instead of reusing local download", audio)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("transcription never reached the engine")
		}
		awaitTranscription(t, a, job.ID)
		if _, err := os.Stat(path); err != nil {
			t.Fatal("persistent download removed", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	job := startTranscription(t, a, "bilibili:BV17x411w7KC")
	select {
	case audio := <-engine.sources:
		if audio.LocalPath != "" || audio.URL != "https://fixture.bilivideo.com/fresh-audio" {
			t.Fatal("missing local file did not fall back to fresh source", audio)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fallback never reached the engine")
	}
	awaitTranscription(t, a, job.ID)
}

func awaitTranscription(t *testing.T, a *Application, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := a.runtime.transcription.Get(id)
		if err != nil || job.State == "failed" {
			t.Fatal(job, err)
		}
		if job.State == "completed" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("transcription did not finish")
}

func startTranscription(t *testing.T, a *Application, contentID string) transcription.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := a.runtime.transcription.Start(context.Background(), contentID, true)
		if err == nil {
			return job
		}
		if err != transcription.ErrBusy {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("previous transcription did not release the active slot")
	return transcription.Job{}
}

func TestTranscriptionResolvesArchivedRSSMediaAfterSourceRefreshAndRemoval(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TANGENTRY_DOWNLOAD_DIR", filepath.Join(root, "downloads"))
	engine := sourceRecordingEngine{sources: make(chan transcription.Audio, 1)}
	a, err := New(root, fstest.MapFS{}, Options{TranscriptionEngine: engine})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	store, err := sqlite.Open(filepath.Join(root, "tangentry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	source, _, err := a.subscriptions.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/archived"})
	if err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"audio", "video"} {
		hash := strings.Repeat(string(rune('a'+i)), 64)
		item := content.Item{ID: "rss:" + hash, Platform: "rss", PlatformID: hash, Kind: kind, Title: "Archived media", URL: "https://example.com/item", MediaURL: "https://example.com/archived.mp4", DurationSeconds: 12}
		if _, err = store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}, Total: 1}, 10); err != nil {
			t.Fatal(err)
		}
		if _, err = store.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{}, Total: 0}, 20); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.subscriptions.Delete(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		job := startTranscription(t, a, "rss:"+hash)
		select {
		case audio := <-engine.sources:
			if audio.URL != "https://example.com/archived.mp4" || audio.Duration != 12 {
				t.Fatal(audio)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("archived media not resolved")
		}
		awaitTranscription(t, a, job.ID)
	}
}
