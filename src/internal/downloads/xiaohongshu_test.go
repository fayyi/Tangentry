package downloads_test

import (
	"context"
	"path/filepath"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/downloads"
	"testing"
	"time"
)

func TestShareAccessChangesDoNotDuplicateActiveOrHideCompletedVideo(t *testing.T) {
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
	url := "https://www.xiaohongshu.com/explore/64aaaaaaaaaaaaaaaaaaaaaa"
	job, err := service.Start(context.Background(), downloads.Request{URL: url + "?xsec_token=old-access", Title: "Video"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	duplicate, err := service.Start(context.Background(), downloads.Request{URL: url + "?xsec_token=new-access"})
	if err != nil || duplicate.ID != job.ID {
		t.Fatal(duplicate, err)
	}
	close(runner.release)
	waitState(t, store, job.ID, "completed")
	path, err := service.CompletedPath(context.Background(), url+"?xsec_token=third-access")
	if err != nil || path == "" {
		t.Fatal(path, err)
	}
}
