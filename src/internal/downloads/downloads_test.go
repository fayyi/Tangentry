package downloads_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/discovery"
	"tangentry/internal/downloads"
)

type gatedRunner struct {
	started chan struct{}
	release chan struct{}
}

type unavailableRunner struct{}

func (unavailableRunner) Ready() bool { return false }
func (unavailableRunner) Download(context.Context, string, string, int) (string, error) {
	return "", downloads.ErrTools
}

func TestDouyinDownloadCanEnterQueueWhenToolsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "douyin-queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, unavailableRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	job, err := service.Start(context.Background(), downloads.Request{URL: "https://www.douyin.com/video/6961737553342991651"})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "queued" {
		t.Fatalf("job state = %q", job.State)
	}
}

func TestDouyinShortLinkNeedsResolvedIDBeforeQueueWithoutTools(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "douyin-short.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := downloads.NewService(store, unavailableRunner{}, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Start(context.Background(), downloads.Request{URL: "https://v.douyin.com/fixtureCode/"}); !errors.Is(err, downloads.ErrDouyinShortLink) {
		t.Fatalf("unresolved short link error = %v", err)
	}
	service.SetDouyinResolver(func(context.Context, string) (discovery.DouyinReference, error) {
		return discovery.DouyinReference{ID: "6961737553342991651", URL: "https://www.douyin.com/video/6961737553342991651"}, nil
	})
	job, err := service.Start(context.Background(), downloads.Request{URL: "https://v.douyin.com/fixtureCode/"})
	if err != nil || job.URL != "https://www.douyin.com/video/6961737553342991651" {
		t.Fatalf("resolved short link job=%+v err=%v", job, err)
	}
}

func (r gatedRunner) Ready() bool { return true }
func (r gatedRunner) Download(ctx context.Context, url, directory string, height int) (string, error) {
	r.started <- struct{}{}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.release:
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	err := os.WriteFile(filepath.Join(directory, "video.mp4"), []byte("explicit test fixture"), 0600)
	return "video.mp4", err
}

func waitState(t *testing.T, store *sqlite.Store, id, state string) downloads.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.GetDownload(context.Background(), id)
		if err == nil && job.State == state {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := store.GetDownload(context.Background(), id)
	t.Fatalf("wanted %s, got %+v", state, job)
	return job
}

func TestExplicitDownloadDeduplicatesAndServesOnlyCompletedFiles(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	job, err := service.Start(context.Background(), downloads.Request{URL: "BV17x411w7KC", Title: "Test video"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	duplicate, err := service.Start(context.Background(), downloads.Request{URL: job.URL + "?from=sharing"})
	if err != nil || duplicate.ID != job.ID {
		t.Fatal("active download was not deduplicated", err)
	}
	if _, err := service.File(context.Background(), job.ID); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("unfinished file was served", err)
	}
	close(runner.release)
	waitState(t, store, job.ID, "completed")
	file, err := service.File(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	if string(data) != "explicit test fixture" {
		t.Fatal("wrong file")
	}
	items, err := store.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatal("download automatically saved to library")
	}
}

func TestChangingDirectoryPreservesRunningJobAndPersistsNewDestination(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := filepath.Join(root, "media")
	runner := gatedRunner{make(chan struct{}, 2), make(chan struct{})}
	service, err := downloads.NewService(store, runner, original, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Start(context.Background(), downloads.Request{URL: "BV17x411w7KC"})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	destination := t.TempDir()
	if err := service.SetDirectory(destination); err != nil {
		t.Fatal(err)
	}
	second, err := service.Start(context.Background(), downloads.Request{URL: "BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	waitState(t, store, first.ID, "completed")
	waitState(t, store, second.ID, "completed")
	service.Close()
	service, err = downloads.NewService(store, runner, original, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if service.Directory() != destination || !service.DirectorySelected() {
		t.Fatal("destination lost")
	}
	for id, directory := range map[string]string{first.ID: original, second.ID: destination} {
		file, err := service.File(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if file.Name() != filepath.Join(directory, id, "video.mp4") {
			t.Fatal("job moved unexpectedly", file.Name())
		}
		file.Close()
	}
	if err := service.SetDirectory("relative"); err == nil {
		t.Fatal("relative directory accepted")
	}
}

func TestReadableOutputDirectoriesAreUniqueRestoredAndIndividuallyDeleted(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "readable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mediaRoot := filepath.Join(root, "media")
	runner := gatedRunner{make(chan struct{}, 2), make(chan struct{})}
	service, err := downloads.NewService(store, runner, mediaRoot, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	request := downloads.Request{URL: "https://www.bilibili.com/video/BV17x411w7KC", Title: "Video", Subdirectory: "B站收藏夹/测试 (12345)", OutputDirectory: "同名视频 [BVfixture]"}
	first, err := service.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	request.URL = "https://www.bilibili.com/video/BV1xx411c7mD"
	second, err := service.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.OutputDirectory != "同名视频 [BVfixture]" || second.OutputDirectory != "同名视频 [BVfixture] (2)" {
		t.Fatalf("output directories collided: %q, %q", first.OutputDirectory, second.OutputDirectory)
	}
	close(runner.release)
	waitState(t, store, first.ID, "completed")
	waitState(t, store, second.ID, "completed")
	service.Close()

	service, err = downloads.NewService(store, runner, mediaRoot, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, job := range []downloads.Job{first, second} {
		file, err := service.File(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(mediaRoot, "B站收藏夹", "测试 (12345)", job.OutputDirectory, "video.mp4")
		if file.Name() != want {
			t.Fatalf("restored path = %q, want %q", file.Name(), want)
		}
		file.Close()
	}
	if err := service.Delete(ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mediaRoot, "B站收藏夹", "测试 (12345)")); err != nil {
		t.Fatal("deleting one video removed its favorite parent", err)
	}
	if _, err := os.Stat(filepath.Join(mediaRoot, "B站收藏夹", "测试 (12345)", second.OutputDirectory, "video.mp4")); err != nil {
		t.Fatal("deleting one video removed its sibling", err)
	}
}

func TestDeletingRetryHistoryDoesNotRemoveSharedOutputDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "shared-output.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mediaRoot := filepath.Join(root, "media")
	if err := os.MkdirAll(filepath.Join(mediaRoot, "B站收藏夹", "测试 (12345)", "视频 [BVfixture]"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "B站收藏夹", "测试 (12345)", "视频 [BVfixture]", "video.mp4"), []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, mediaRoot, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	shared := downloads.Job{URL: "https://www.bilibili.com/video/BV17x411w7KC/", Subdirectory: "B站收藏夹/测试 (12345)", OutputDirectory: "视频 [BVfixture]", State: "queued"}
	failed := shared
	failed.ID, failed.State = "failed-attempt", "failed"
	queued := shared
	queued.ID = "queued-retry"
	if err := store.SaveDownload(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDownload(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), failed.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mediaRoot, shared.Subdirectory, shared.OutputDirectory, "video.mp4")); err != nil {
		t.Fatal("deleting old retry removed a path still used by another job", err)
	}
}

func TestReadableDirectoryIsReusedOnlyForAnEquivalentRetry(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "retry-directory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mediaRoot := filepath.Join(root, "media")
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, mediaRoot, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	request := downloads.Request{
		URL:             "https://www.bilibili.com/video/BV17x411w7KC/",
		Title:           "Fixture",
		MaxHeight:       1080,
		Options:         downloads.Options{Container: "mp4", SubtitleLanguages: []string{"zh-Hans"}},
		Subdirectory:    "B站收藏夹/Fixture (12345)",
		OutputDirectory: "Fixture [BV17x411w7KC]",
	}
	failed := downloads.Job{
		Options: request.Options, ID: "failed-attempt", URL: request.URL, Title: request.Title,
		Subdirectory: request.Subdirectory, OutputDirectory: request.OutputDirectory,
		MaxHeight: request.MaxHeight, State: "failed",
	}
	if err := store.SaveDownload(ctx, failed); err != nil {
		t.Fatal(err)
	}
	retry, err := service.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if retry.OutputDirectory != failed.OutputDirectory {
		t.Fatalf("equivalent retry changed directory: %q", retry.OutputDirectory)
	}
	if err := service.Cancel(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	request.MaxHeight = 720
	changed, err := service.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if changed.OutputDirectory == failed.OutputDirectory || changed.OutputDirectory != failed.OutputDirectory+" (2)" {
		t.Fatalf("changed strategy reused directory: %q", changed.OutputDirectory)
	}
	if err := service.Cancel(ctx, changed.ID); err != nil {
		t.Fatal(err)
	}

	completed := failed
	completed.ID, completed.State, completed.CreatedAt = "completed-attempt", "completed", 1
	if err := store.SaveDownload(ctx, completed); err != nil {
		t.Fatal(err)
	}
	request.MaxHeight = failed.MaxHeight
	completedRetry, err := service.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if completedRetry.OutputDirectory == completed.OutputDirectory {
		t.Fatalf("completed destination was reused: %q", completedRetry.OutputDirectory)
	}
	if err := service.Cancel(ctx, completedRetry.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSubdirectoryValidationRejectsTraversalAndPortablePathHazards(t *testing.T) {
	for _, path := range []string{"../escape", "B站\\收藏夹", "B站/CON", "B站//夹", "B站/name.", "B站/带\n控制符", "/absolute"} {
		if _, err := downloads.NormalizeSubdirectory(path); !errors.Is(err, downloads.ErrInvalid) {
			t.Errorf("accepted unsafe subdirectory %q: %v", path, err)
		}
	}
	if _, err := downloads.NormalizeSubdirectory("B站收藏夹/课程 (12345)"); err != nil {
		t.Fatal("valid multilingual path rejected", err)
	}
}

func TestFavoriteSubdirectorySymlinkCannotRedirectDownload(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("symlink fixture")
	}
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "symlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mediaRoot := filepath.Join(root, "media")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(mediaRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(mediaRoot, "B站收藏夹")); err != nil {
		t.Fatal(err)
	}
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, mediaRoot, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	job, err := service.Start(context.Background(), downloads.Request{URL: "https://www.bilibili.com/video/BV17x411w7KC", Subdirectory: "B站收藏夹/测试 (12345)", OutputDirectory: "测试视频 [BV17x411w7KC]"})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, store, job.ID, "failed")
	select {
	case <-runner.started:
		t.Fatal("runner wrote through a subdirectory symlink")
	default:
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside directory was modified", entries, err)
	}
}

func TestCancelAndRestartPreserveHistoryWithoutAutomaticallyResuming(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Start(context.Background(), downloads.Request{URL: "BV17x411w7KC"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	second, err := service.Start(context.Background(), downloads.Request{URL: "BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	service.Close()
	store.Close()
	store, err = sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = downloads.NewService(store, runner, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	waitState(t, store, first.ID, "interrupted")
	waitState(t, store, second.ID, "cancelled")
	select {
	case <-runner.started:
		t.Fatal("restart silently resumed download")
	default:
	}
}

func TestDeleteHistoryRequiresTerminalStateAndCanRemoveFiles(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	directory := filepath.Join(root, "media")
	service, err := downloads.NewService(store, runner, directory, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	completed := downloads.Job{ID: "completed-delete", URL: "https://www.bilibili.com/video/BV17x411w7KC/", State: "completed", FileName: "video.mp4", CreatedAt: 1}
	if err := store.SaveDownload(ctx, completed); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, completed.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, completed.ID, completed.FileName), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, completed.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDownload(ctx, completed.ID); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, completed.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file directory retained")
	}
	active, err := service.Start(ctx, downloads.Request{URL: "https://www.bilibili.com/video/BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, active.ID, false); !errors.Is(err, downloads.ErrActive) {
		t.Fatal(err)
	}
}

func TestClearHistoryLeavesActiveTasksForCancellation(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "clear.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, filepath.Join(root, "media"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	terminal := downloads.Job{ID: "terminal-clear", URL: "https://www.bilibili.com/video/BV17x411w7KC/", State: "failed", CreatedAt: 1}
	if err := store.SaveDownload(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	active, err := service.Start(ctx, downloads.Request{URL: "https://www.bilibili.com/video/BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Clear(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDownload(ctx, terminal.ID); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("terminal history retained")
	}
	if _, err := store.GetDownload(ctx, active.ID); err != nil {
		t.Fatal("active task was cleared", err)
	}
	if err := service.Cancel(ctx, active.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedURLsNeverReachDownloadTools(t *testing.T) {
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
	for _, url := range []string{"http://127.0.0.1/private", "file:///etc/passwd", "https://user:secret@www.bilibili.com/video/BV17x411w7KC", "--exec=echo secrets", "12345", "http://192.168.1.1/video", "https://localhost/video", "http://[::1]/video"} {
		if _, err := service.Start(context.Background(), downloads.Request{URL: url}); !errors.Is(err, downloads.ErrInvalid) {
			t.Errorf("accepted %q: %v", url, err)
		}
	}
	select {
	case <-runner.started:
		t.Fatal("tool executed for invalid URL")
	default:
	}
}

func TestCompletedPathOnlyReusesMatchingCompleteExistingMedia(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory := filepath.Join(root, "media")
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, directory, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV17x411w7KC/"
	for _, value := range []struct{ id, state, file, body string }{
		{"valid", "completed", "video.mp4", "complete file"},
		{"empty", "completed", "video.mp4", ""},
		{"missing", "completed", "missing.mp4", ""},
		{"partial", "running", "video.mp4", "unfinished file"},
		{"symlink", "completed", "video.mp4", ""},
		{"directory", "completed", "video.mp4", ""},
	} {
		if err := os.MkdirAll(filepath.Join(directory, value.id), 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, value.id, value.file)
		if value.id == "directory" {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		} else if value.id == "symlink" {
			if err := os.Symlink(filepath.Join(directory, "valid", "video.mp4"), path); err != nil {
				t.Fatal(err)
			}
		} else if value.id != "missing" {
			if err := os.WriteFile(path, []byte(value.body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.SaveDownload(ctx, downloads.Job{ID: value.id, URL: url, State: value.state, FileName: value.file}); err != nil {
			t.Fatal(err)
		}
		if value.id != "valid" {
			file, err := service.File(ctx, value.id)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, downloads.ErrNotFound) {
				t.Fatalf("%s served unusable media: %v", value.id, err)
			}
		}
	}
	path, err := service.CompletedPath(ctx, url+"?from=sharing")
	if err != nil || path != filepath.Join(directory, "valid", "video.mp4") {
		t.Fatal(path, err)
	}
	if _, err := service.CompletedPath(ctx, "https://www.youtube.com/watch?v=jNQXAC9IVRw"); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("unrelated media reused", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompletedPath(ctx, url); !errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("partial or missing media reused", err)
	}
	select {
	case <-runner.started:
		t.Fatal("lookup started an automatic download")
	default:
	}
}

func TestUnreadableCompletedMediaIsNotReportedAsMissing(t *testing.T) {
	root := t.TempDir()
	store, err := sqlite.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory := filepath.Join(root, "media")
	runner := gatedRunner{make(chan struct{}, 1), make(chan struct{})}
	service, err := downloads.NewService(store, runner, directory, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	jobDir := filepath.Join(directory, "unreadable")
	if err := os.Mkdir(jobDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(jobDir, "video.mp4")
	if err := os.WriteFile(path, []byte("existing media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(jobDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(jobDir, 0700)
	if file, err := os.Open(path); err == nil {
		file.Close()
		t.Skip("filesystem allows reading despite removed directory permissions")
	}
	ctx := context.Background()
	url := "https://www.bilibili.com/video/BV17x411w7KC/"
	if err := store.SaveDownload(ctx, downloads.Job{ID: "unreadable", URL: url, State: "completed", FileName: "video.mp4"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.File(ctx, "unreadable"); !errors.Is(err, os.ErrPermission) {
		t.Fatal("permission failure hidden", err)
	}
	if _, err := service.CompletedPath(ctx, url); !errors.Is(err, os.ErrPermission) || errors.Is(err, downloads.ErrNotFound) {
		t.Fatal("unreadable media treated as missing", err)
	}
}
