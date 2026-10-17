package notes_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestCreationRetryPreservesLatestAndDeletedNotes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(store)
	input := notes.Input{CreateID: strings.Repeat("a", 32), Body: "  想法  ", Tags: []string{"读书", "读书"}}
	saved, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Body = "想法"
	input.Tags = []string{"读书"}
	retried, err := service.Create(ctx, input)
	if err != nil || retried.ID != saved.ID {
		t.Fatal(retried, err)
	}
	latest, err := service.Edit(ctx, saved.ID, notes.Edit{Body: "后来修改", ExpectedUpdatedAt: &saved.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	retried, err = service.Create(ctx, input)
	if err != nil || retried.Body != latest.Body || retried.UpdatedAt != latest.UpdatedAt {
		t.Fatal("retry overwrote edit", retried, err)
	}
	changed := input
	changed.Body = "不同草稿"
	if _, err := service.Create(ctx, changed); !errors.Is(err, notes.ErrCreationConflict) {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	if _, err := service.Create(ctx, input); !errors.Is(err, notes.ErrCreationDeleted) {
		t.Fatal("retry resurrected deleted note", err)
	}
	found, err := service.List(ctx, saved.ContentID)
	if err != nil || len(found) != 0 {
		t.Fatal(found, err)
	}
}

func TestConcurrentCreationHasOneResult(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	service := notes.NewService(store)
	input := notes.Input{CreateID: strings.Repeat("b", 32), ContentID: "youtube:abcdefghijk", Body: "并发重试"}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			note, err := service.Create(ctx, input)
			if err != nil || note.ID != input.CreateID {
				t.Errorf("retry: %v %v", note, err)
			}
		}()
	}
	group.Wait()
	found, err := service.List(ctx, input.ContentID)
	if err != nil || len(found) != 1 {
		t.Fatal(found, err)
	}
}

func TestFrameCreationAtomicRetryAndFailure(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(source, []byte("owned fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	image := frameJPEG(t)
	fail := true
	captures := 0
	position := 2.125
	input := notes.FrameNoteInput{
		Note:    notes.Input{CreateID: strings.Repeat("c", 32), ContentID: "youtube:abcdefghijk", Body: "文字和画面", PositionSeconds: &position},
		Capture: notes.FrameSpec{DownloadID: strings.Repeat("d", 32), PositionSeconds: &position},
	}
	frames := notes.NewFrames(store, func(_ context.Context, contentID, downloadID string) (*os.File, error) {
		if contentID != input.Note.ContentID || downloadID != input.Capture.DownloadID {
			return nil, notes.ErrFrameSource
		}
		return os.Open(source)
	}, func(ctx context.Context, _ string, seconds float64) ([]byte, error) {
		captures++
		if fail {
			return nil, notes.ErrFrameUnavailable
		}
		if seconds != position {
			t.Fatalf("wrong capture time: %v", seconds)
		}
		return image, nil
	})
	if _, err := frames.Create(ctx, input); !errors.Is(err, notes.ErrFrameUnavailable) {
		t.Fatal(err)
	}
	found, err := store.ListNotes(ctx, input.Note.ContentID)
	if err != nil || len(found) != 0 {
		t.Fatal("partial note", found, err)
	}
	if _, exists, err := store.FindNoteCreation(ctx, input.Note.CreateID, "unknown"); err != nil || exists {
		t.Fatal("failed attempt has receipt", err)
	}
	fail = false
	saved, err := frames.Create(ctx, input)
	if err != nil || saved.Frame == nil {
		t.Fatal(saved, err)
	}
	data, err := frames.Image(ctx, saved.ID, saved.Frame.ID)
	if err != nil || !bytes.Equal(data, image) {
		t.Fatal("image not saved", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	retried, err := frames.Create(ctx, input)
	if err != nil || retried.Frame.ID != saved.Frame.ID || captures != 2 {
		t.Fatal("retry recaptured", retried, captures, err)
	}
	if _, err := notes.NewService(store).Create(ctx, input.Note); !errors.Is(err, notes.ErrCreationConflict) {
		t.Fatal("text retry ignored frame intent", err)
	}
	other := input
	other.Capture.DownloadID = strings.Repeat("e", 32)
	if _, err := frames.Create(ctx, other); !errors.Is(err, notes.ErrCreationConflict) {
		t.Fatal("changed source ignored", err)
	}
	if err := notes.NewService(store).Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := frames.Create(ctx, input); !errors.Is(err, notes.ErrCreationDeleted) {
		t.Fatal("deleted paired note resurrected", err)
	}
}
