package favoritesync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"tangentry/internal/downloads"
)

func TestCancellationsAndRetryLimitPreserveIntent(t *testing.T) {
	s, _, remote, d := newFixture(t)
	ctx := context.Background()
	p := DefaultPolicy()
	p.AutoDownload = true
	p.MaxPerRun = 1
	s.Create(ctx, Input{Query: "12345", Policy: p})
	remote.mu.Lock()
	remote.pages[0].Items = remote.pages[0].Items[:1]
	remote.pages[0].Entries = 1
	remote.pages[0].Folder.Count = 1
	remote.mu.Unlock()
	first := syncOnce(t, s, "12345")
	if d.started != 1 {
		t.Fatal(first)
	}
	originalDirectory := d.jobs[0].OutputDirectory
	for attempt := 1; attempt < 3; attempt++ {
		d.mu.Lock()
		d.jobs[len(d.jobs)-1].State = "failed"
		d.mu.Unlock()
		syncOnce(t, s, "12345")
		if d.jobs[len(d.jobs)-1].OutputDirectory != originalDirectory {
			t.Fatal("automatic retry changed its video folder", d.jobs[len(d.jobs)-1].OutputDirectory, originalDirectory)
		}
	}
	d.mu.Lock()
	d.jobs[len(d.jobs)-1].State = "failed"
	d.mu.Unlock()
	syncOnce(t, s, "12345")
	if d.started != 3 {
		t.Fatal("retry limit failed", d.started)
	}
	s2, _, _, d2 := newFixture(t)
	s2.Create(ctx, Input{Query: "12345", Policy: p})
	syncOnce(t, s2, "12345")
	d2.mu.Lock()
	d2.jobs[0].State = "cancelled"
	d2.mu.Unlock()
	syncOnce(t, s2, "12345")
	d2.mu.Lock()
	for _, job := range d2.jobs[1:] {
		if job.URL == d2.jobs[0].URL {
			t.Fatal("cancelled job restarted")
		}
	}
	d2.mu.Unlock()
	remote.mu.Lock()
	remote.gate = make(chan struct{})
	remote.mu.Unlock()
	s.Start(ctx, "12345")
	if _, err := s.Configure(ctx, "12345", p); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, "12345"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := s.Cancel("12345"); err != nil {
		t.Fatal(err)
	}
	cancelled := waitIdle(t, s, "12345")
	if cancelled.LastSuccessAt != first.LastSuccessAt || len(cancelled.Items) != 1 || cancelled.Message != "同步已取消，上次成功内容已保留。" {
		t.Fatal(cancelled)
	}
}

func TestSharedFailedDownloadRetriesOnceAndCancellationPropagates(t *testing.T) {
	s, repo, remote, d := newFixture(t)
	ctx := context.Background()
	policy := DefaultPolicy()
	policy.AutoDownload = true
	remote.pages[0].Items = remote.pages[0].Items[:1]
	remote.pages[0].Entries, remote.pages[0].Folder.Count = 1, 1
	for _, id := range []string{"12345", "23456"} {
		if _, err := s.Create(ctx, Input{Query: id, Policy: policy}); err != nil {
			t.Fatal(err)
		}
		syncOnce(t, s, id)
	}
	// The two folders intentionally have independent download paths. A
	// transfer from another folder must not consume this folder's retry budget.
	if d.started != 2 {
		t.Fatal("folders should keep downloads in separate directories", d.started)
	}
	d.mu.Lock()
	d.jobs[0].State = "failed"
	d.jobs[1].State = "failed"
	d.mu.Unlock()
	first := syncOnce(t, s, "12345")
	second := syncOnce(t, s, "23456")
	itemID := video(0).ID
	if d.started != 4 || first.Transfers[itemID].JobID == second.Transfers[itemID].JobID || len(second.Pending) != 0 {
		t.Fatal("folder retries crossed destination boundaries", d.started, first, second)
	}
	if first.Transfers[itemID].Attempts != 2 || second.Transfers[itemID].Attempts != 2 {
		t.Fatal("sharing altered admission counts", first.Transfers, second.Transfers)
	}
	d.mu.Lock()
	d.jobs[3].State = "cancelled"
	d.mu.Unlock()
	for _, id := range []string{"12345", "23456"} {
		result := syncOnce(t, s, id)
		if len(result.Pending) != 0 {
			t.Fatal("folder cancellation was lost", result)
		}
	}
	if d.started != 4 {
		t.Fatal("cancelled retry was restarted", d.started)
	}
	// Re-create the service over durable source records: cancellation remains
	// authoritative even though the scheduler's in-memory state is new.
	s.Close()
	restarted := New(repo, remote, d)
	restarted.pageDelay = 0
	t.Cleanup(restarted.Close)
	for _, id := range []string{"12345", "23456"} {
		syncOnce(t, restarted, id)
	}
	if d.started != 4 {
		t.Fatal("restart changed isolated transfer state", d.started)
	}
}

func TestFailedTransfersRetryInSnapshotOrderWithinRunLimit(t *testing.T) {
	for round := 0; round < 20; round++ {
		s, _, _, d := newFixture(t)
		policy := DefaultPolicy()
		policy.AutoDownload = true
		policy.MaxPerRun = 1
		source := Source{FolderID: "12345", Title: "retry fixture", Policy: policy, Transfers: map[string]Transfer{}}
		for i := 0; i < 6; i++ {
			item := video(i)
			source.Items = append(source.Items, item)
			jobID := fmt.Sprintf("failed-%d", i)
			source.Transfers[item.ID] = Transfer{JobID: jobID, Attempts: 1}
			d.jobs = append(d.jobs, downloads.Job{Options: policy.Download, ID: jobID, URL: item.URL, Subdirectory: "B站收藏夹/retry fixture (12345)", MaxHeight: policy.MaxHeight, State: "failed"})
		}
		s.pump(context.Background(), &source)
		if d.started != 1 || source.Transfers[video(0).ID].Attempts != 2 || len(source.Pending) != 5 {
			t.Fatal("retry limit or first item changed", round, source, d.started)
		}
		for i, id := range source.Pending {
			if id != video(i+1).ID {
				t.Fatal("pending retries reordered", round, source.Pending)
			}
		}
	}
}

func TestExistingFolderAdoptsExplicitRetryWithoutResettingAutomaticLimit(t *testing.T) {
	for _, state := range []string{"cancelled", "failed"} {
		t.Run(state, func(t *testing.T) {
			s, repo, remote, d := newFixture(t)
			ctx := context.Background()
			policy := DefaultPolicy()
			policy.AutoDownload = true
			remote.pages[0].Items = remote.pages[0].Items[:1]
			remote.pages[0].Entries = 1
			remote.pages[0].Folder.Count = 1
			if _, err := s.Create(ctx, Input{Query: "12345", Policy: policy}); err != nil {
				t.Fatal(err)
			}
			source := syncOnce(t, s, "12345")
			d.mu.Lock()
			d.jobs[0].State = state
			d.mu.Unlock()
			transfer := source.Transfers[video(0).ID]
			transfer.Attempts = 3
			source.Transfers[video(0).ID] = transfer
			if err := repo.PutFavoriteSource(ctx, source); err != nil {
				t.Fatal(err)
			}
			syncOnce(t, s, "12345")
			if d.started != 1 {
				t.Fatal("automatic limit ignored", d.started)
			}
			explicit, err := d.Start(ctx, downloads.Request{URL: video(0).URL, Title: "manual retry", MaxHeight: 1080, Options: policy.Download})
			if err != nil {
				t.Fatal(err)
			}
			adopted := syncOnce(t, s, "12345")
			if d.started != 2 || adopted.Transfers[video(0).ID].JobID != explicit.ID || adopted.Transfers[video(0).ID].Attempts != 3 || len(adopted.Pending) != 0 {
				t.Fatal("explicit retry not adopted", adopted, d.started)
			}
			d.mu.Lock()
			d.jobs[len(d.jobs)-1].State = "failed"
			d.mu.Unlock()
			syncOnce(t, s, "12345")
			if d.started != 2 {
				t.Fatal("sharing reset automatic retry limit", d.started)
			}
		})
	}
}
