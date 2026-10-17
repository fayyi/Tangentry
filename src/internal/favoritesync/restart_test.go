package favoritesync_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/downloads"
	"tangentry/internal/favoritesync"
)

type restartRemote struct{ item content.Item }

func (r restartRemote) Folders(ctx context.Context) ([]favoritesync.Folder, error) {
	return []favoritesync.Folder{{ID: "12345", Title: "Restart fixture", Count: 1}}, ctx.Err()
}
func (r restartRemote) Page(ctx context.Context, id string, _ int) (favoritesync.Page, error) {
	return favoritesync.Page{Folder: favoritesync.Folder{ID: id, Title: "Restart fixture", Count: 1}, Items: []content.Item{r.item}, Entries: 1}, ctx.Err()
}

type restartRunner struct{ calls atomic.Int32 }

func (*restartRunner) Ready() bool { return true }
func (r *restartRunner) Download(context.Context, string, string, int) (string, error) {
	r.calls.Add(1)
	return "", errors.New("explicit failed download fixture")
}

func TestSQLiteRestartPreservesFavoriteRetryBudgetAndCancellation(t *testing.T) {
	for _, state := range []string{"running", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			item := content.Item{ID: "bilibili:BV17x411w7KC", Platform: "bilibili", PlatformID: "BV17x411w7KC", Kind: "video", Title: "Restart fixture", URL: "https://www.bilibili.com/video/BV17x411w7KC/"}
			store, err := sqlite.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			policy := favoritesync.DefaultPolicy()
			policy.AutoDownload = true
			source := favoritesync.Source{FolderID: "12345", Title: "Restart fixture", Policy: policy, Items: []content.Item{item}, Pending: []string{}, Transfers: map[string]favoritesync.Transfer{item.ID: {JobID: strings.Repeat("a", 32), Attempts: 1}}, CreatedAt: time.Now().Unix()}
			if err := store.PutFavoriteSource(ctx, source); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveDownload(ctx, downloads.Job{ID: strings.Repeat("a", 32), URL: item.URL, Title: item.Title, State: state, MaxHeight: 1080, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}); err != nil {
				t.Fatal(err)
			}
			store.Close()
			runner := &restartRunner{}
			for round := 0; round < 3; round++ {
				store, err = sqlite.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				downloader, err := downloads.NewService(store, runner, filepath.Join(root, "media"), time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				service := favoritesync.New(store, restartRemote{item}, downloader)
				if _, err := service.Start(ctx, "12345"); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(3 * time.Second)
				for {
					source, err = service.Get(ctx, "12345")
					if err != nil {
						t.Fatal(err)
					}
					if !source.Active {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("sync did not finish")
					}
					time.Sleep(time.Millisecond)
				}
				service.Close()
				for {
					job, err := store.GetDownload(ctx, source.Transfers[item.ID].JobID)
					if err != nil {
						t.Fatal(err)
					}
					if job.State == "failed" || job.State == "cancelled" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("download did not reach terminal state", job)
					}
					time.Sleep(time.Millisecond)
				}
				downloader.Close()
				store.Close()
				wantAttempts := min(round+2, 3)
				if state == "cancelled" {
					wantAttempts = 1
				}
				if source.Transfers[item.ID].Attempts != wantAttempts || len(source.Pending) != 0 {
					t.Fatal("persistent retry budget changed", round, source)
				}
			}
			wantCalls := int32(2)
			if state == "cancelled" {
				wantCalls = 0
			}
			if runner.calls.Load() != wantCalls {
				t.Fatal("restart duplicated downloads or ignored cancellation", runner.calls.Load())
			}
		})
	}
}
