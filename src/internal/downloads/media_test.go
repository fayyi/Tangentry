package downloads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/downloads"
)

func TestGenericURLsAndInvalidDownloadChoices(t *testing.T) {
	for _, raw := range []string{"https://vimeo.com/12345", "https://soundcloud.com/artist/track", "https://cdn.example.com/media.mp3?signature=abc", "https://v.douyin.com/ShareFixture/"} {
		if _, err := downloads.NormalizeURL(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, t.TempDir(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, options := range []downloads.Options{
		{Container: "exe"}, {Mode: "command"}, {AudioFormat: "invalid"},
		{VideoFormatID: "137+--exec"}, {AudioFormatID: "140/../../"},
		{SubtitleLanguages: []string{"all,-en"}}, {SubtitleLanguages: []string{"live_chat"}},
	} {
		_, err := service.Start(context.Background(), downloads.Request{URL: "https://example.com/video", Options: options})
		if !errors.Is(err, downloads.ErrInvalid) {
			t.Fatal("accepted invalid options", options, err)
		}
	}
}

func TestDistinctOptionsPersistAndArtifactsStayWithinCompletedJob(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	root := t.TempDir()
	service, err := downloads.NewService(store, runner, root, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	first, err := service.Start(ctx, downloads.Request{URL: "https://example.com/video", Options: downloads.Options{Container: "mkv", SubtitleLanguages: []string{"zh-Hans"}, Compress: true}})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	second, err := service.Start(ctx, downloads.Request{URL: first.URL, Options: downloads.Options{Mode: "audio", AudioFormat: "mp3"}})
	if err != nil || first.ID == second.ID {
		t.Fatal("distinct formats deduplicated", err)
	}
	saved, err := store.GetDownload(ctx, first.ID)
	if err != nil || saved.Container != "mkv" || !saved.Compress || len(saved.SubtitleLanguages) != 1 {
		t.Fatal(saved, err)
	}
	directory := filepath.Join(root, "artifact-fixture")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Title.mp4", "Title.en.srt", "private.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	job := downloads.Job{ID: "artifact-fixture", FileName: "Title.mp4", Files: []string{"Title.mp4", "Title.en.srt"}, State: "completed"}
	if err := store.SaveDownload(ctx, job); err != nil {
		t.Fatal(err)
	}
	file, err := service.Artifact(ctx, job.ID, "Title.en.srt")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, name := range []string{"private.txt", "../Title.mp4", "../../private.txt"} {
		if _, err := service.Artifact(ctx, job.ID, name); !errors.Is(err, downloads.ErrNotFound) {
			t.Fatal("unexpected artifact access", name, err)
		}
	}
	job.State = "running"
	store.SaveDownload(ctx, job)
	if _, err := service.Artifact(ctx, job.ID, "Title.en.srt"); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("unfinished subtitles exposed", err)
	}
}
