package downloads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/downloads"
)

type multiPageMediaRunner struct{}

func (multiPageMediaRunner) Ready() bool { return true }
func (multiPageMediaRunner) Download(context.Context, string, string, int) (string, error) {
	return "", errors.New("unexpected legacy download")
}
func (multiPageMediaRunner) Inspect(context.Context, string) (downloads.MediaInfo, error) {
	return downloads.MediaInfo{}, nil
}
func (multiPageMediaRunner) DownloadMedia(_ context.Context, _ string, directory string, _ int, _ downloads.Options) (downloads.Result, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return downloads.Result{}, err
	}
	for _, name := range []string{"Season 1/Video - S01E01.mp4", "Season 1/Video - S01E02.mp4", "Season 1/Video - S01E01.zh-Hans.srt", "Season 1/Video - S01E02.danmaku.xml", "tvshow.nfo", "Season 1/Video - S01E01.nfo"} {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return downloads.Result{}, err
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			return downloads.Result{}, err
		}
	}
	return downloads.Result{FileName: "Season 1/Video - S01E01.mp4", FileSize: 14, OriginalSize: 14}, nil
}

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
	subdirectory := "B站收藏夹/Fixture (12345)"
	outputDirectory := "Video title [BV1234567890]"
	directory := filepath.Join(root, filepath.FromSlash(subdirectory), outputDirectory)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Title.mp4", "Title.en.srt", "private.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	job := downloads.Job{ID: "artifact-fixture", Subdirectory: subdirectory, OutputDirectory: outputDirectory, FileName: "Title.mp4", Files: []string{"Title.mp4", "Title.en.srt"}, State: "completed"}
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

func TestCompletedMultiPageDownloadRegistersVideosAndSidecars(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, multiPageMediaRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	job, err := service.Start(context.Background(), downloads.Request{URL: "https://www.bilibili.com/video/BV17x411w7KC/"})
	if err != nil {
		t.Fatal(err)
	}
	job = waitState(t, store, job.ID, "completed")
	if job.FileName != "Season 1/Video - S01E01.mp4" || len(job.Files) != 6 {
		t.Fatalf("completed files = %q, %v", job.FileName, job.Files)
	}
	for _, name := range job.Files {
		if name != "tvshow.nfo" && (name == "Season 1" || !strings.HasPrefix(name, "Season 1/")) {
			t.Fatalf("multi-page artifact escaped Season 1: %v", job.Files)
		}
		file, err := service.Artifact(context.Background(), job.ID, name)
		if err != nil {
			t.Fatalf("artifact %q unavailable: %v", name, err)
		}
		file.Close()
	}
}

func TestArtifactPathsRejectTraversalAndSymlinks(t *testing.T) {
	for _, name := range []string{"../video.mp4", "Season 1/../../secret.json", "Season 1\\video.mp4", "Season 1/CON.txt", "Season 1/name."} {
		if _, err := downloads.NormalizeArtifactPath(name); !errors.Is(err, downloads.ErrInvalid) {
			t.Errorf("unsafe artifact path accepted %q: %v", name, err)
		}
	}

	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "artifact-symlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, multiPageMediaRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	job := downloads.Job{ID: "artifact-symlink", URL: "https://www.bilibili.com/video/BV17x411w7KC/", State: "completed", FileName: "Season 1/video.mp4", Files: []string{"Season 1/video.mp4"}}
	if err := store.SaveDownload(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.mp4")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "media", job.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "media", job.ID, "Season 1")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Artifact(context.Background(), job.ID, job.FileName); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatalf("artifact path followed symlink: %v", err)
	}
}
