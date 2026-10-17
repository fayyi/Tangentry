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
	"tangentry/internal/downloads"
)

type gatedRunner struct {
	started chan struct{}
	release chan struct{}
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
