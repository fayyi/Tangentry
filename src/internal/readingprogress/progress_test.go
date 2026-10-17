package readingprogress_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/readingprogress"
	"testing"
)

func TestYouTubeProgressIsVersionedPersistentAndIsolatedFromBilibili(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "youtube.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	service := readingprogress.NewService(store)
	youtube, bilibili := "youtube:YTresume000", "bilibili:BVresume0000"
	saved, err := service.Save(ctx, youtube, readingprogress.Input{PositionSeconds: 5.5, DurationSeconds: 12})
	if err != nil || saved.ContentID != youtube || saved.Revision != 1 {
		t.Fatal("YouTube progress could not be saved", err)
	}
	bili, err := service.Save(ctx, bilibili, readingprogress.Input{PositionSeconds: 2, DurationSeconds: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(ctx, youtube, readingprogress.Input{PositionSeconds: 7, DurationSeconds: 12}); !errors.Is(err, readingprogress.ErrConflict) {
		t.Fatal("YouTube progress accepted a stale revision")
	}
	rows, err := service.Batch(ctx, []string{youtube, bilibili, youtube})
	if err != nil || len(rows) != 2 || rows[0] != bili || rows[1] != saved {
		t.Fatal("mixed-platform progress batch leaked or duplicated rows", err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = readingprogress.NewService(store)
	current, err := service.Get(ctx, youtube)
	if err != nil || current == nil || *current != saved {
		t.Fatal("YouTube progress did not survive reopen", err)
	}
	for _, id := range []string{"youtube:short", "youtube:YTresume000x", "youtube:YTresume00/", "rss:" + strings.Repeat("a", 63)} {
		if _, err := service.Get(ctx, id); !errors.Is(err, readingprogress.ErrInvalid) {
			t.Fatal("invalid or non-video progress identity accepted")
		}
	}
}

func TestNativePlatformProgressPersistsAndRejectsStaleWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := readingprogress.NewService(store)
	ctx := context.Background()
	ids := []string{"xiaohongshu:" + strings.Repeat("a", 24), "xiaoyuzhou:" + strings.Repeat("c", 24), "douyin:1234567890123456", "rss:" + strings.Repeat("b", 64)}
	for _, id := range ids {
		if _, err := service.Save(ctx, id, readingprogress.Input{PositionSeconds: 5.4, DurationSeconds: 12}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Save(ctx, id, readingprogress.Input{PositionSeconds: 6, DurationSeconds: 12}); !errors.Is(err, readingprogress.ErrConflict) {
			t.Fatal("stale native progress accepted", err)
		}
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = readingprogress.NewService(store)
	rows, err := service.Batch(ctx, ids)
	if err != nil || len(rows) != len(ids) {
		t.Fatal("native progress did not persist", err)
	}
	for _, row := range rows {
		if row.PositionSeconds != 5.4 || row.Revision != 1 {
			t.Fatal(row)
		}
	}
	for _, id := range []string{"xiaohongshu:" + strings.Repeat("z", 24), "douyin:0123456789012345", "rss:" + strings.Repeat("a", 65)} {
		if _, err := service.Get(ctx, id); !errors.Is(err, readingprogress.ErrInvalid) {
			t.Fatal("invalid native identity accepted")
		}
	}
}

func TestBatchQueriesAreBoundedIsolatedAndReadOnly(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := readingprogress.NewService(store)
	ctx := context.Background()
	id, missing, other := "bilibili:BV17x411w7KC", "bilibili:BVresume0000", "bilibili:BV1Pyta66EDh"
	saved, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: 5, DurationSeconds: 12})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, other, readingprogress.Input{PositionSeconds: 12, DurationSeconds: 12}); err != nil {
		t.Fatal(err)
	}
	values, err := s.Batch(ctx, []string{id, missing, id})
	if err != nil || len(values) != 1 || values[0] != saved {
		t.Fatalf("leak/duplicate/change: %+v %v", values, err)
	}
	if value, err := s.Get(ctx, missing); err != nil || value != nil {
		t.Fatal("batch created progress")
	}
	values, err = s.Batch(ctx, []string{missing})
	if err != nil || values == nil || len(values) != 0 {
		t.Fatal("empty query is not an array")
	}
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("bilibili:BVlist%06d", i)
	}
	if _, err := s.Batch(ctx, ids); err != nil {
		t.Fatal("maximum valid batch rejected", err)
	}
	for _, input := range [][]string{nil, {}, {id, "invalid"}, {"bilibili:BV17x411w7KC' OR 1=1"}, append(ids, id)} {
		if _, err := s.Batch(ctx, input); !errors.Is(err, readingprogress.ErrInvalid) {
			t.Fatal("invalid batch accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Batch(cancelled, []string{id}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestProgressPersistsAndStalePagesCannotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	s := readingprogress.NewService(store)
	ctx := context.Background()
	id := "bilibili:BV17x411w7KC"
	value, err := s.Get(ctx, id)
	if err != nil || value != nil {
		t.Fatal("missing progress should be null")
	}
	first, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: 5.123456, DurationSeconds: 12, Revision: 0})
	if err != nil || first.Revision != 1 || first.PositionSeconds != 5.123 || first.Completed || first.UpdatedAt <= 0 {
		t.Fatalf("save contract: %+v %v", first, err)
	}
	second, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: 12, DurationSeconds: 12, Revision: 1})
	if err != nil || second.Revision != 2 || !second.Completed {
		t.Fatal("end progress not stored")
	}
	for _, revision := range []int64{0, 1, 99} {
		if _, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: 0, DurationSeconds: 12, Revision: revision}); !errors.Is(err, readingprogress.ErrConflict) {
			t.Fatal("stale/forged revision overwrote progress")
		}
	}
	current, _ := s.Get(ctx, id)
	if *current != second {
		t.Fatal("conflict changed saved progress")
	}
	other, _ := s.Get(ctx, "bilibili:BV1Pyta66EDh")
	if other != nil {
		t.Fatal("video identity leaked")
	}
	if _, err := s.Save(ctx, "bilibili:BV1Pyta66EDh", readingprogress.Input{PositionSeconds: 3, DurationSeconds: 12, Revision: 5}); !errors.Is(err, readingprogress.ErrConflict) {
		t.Fatal("unknown record created with arbitrary revision")
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = readingprogress.NewService(store)
	current, err = s.Get(ctx, id)
	if err != nil || *current != second {
		t.Fatal("progress lost across database reopen")
	}
	reset, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: 0, DurationSeconds: 12, Revision: 2})
	if err != nil || reset.Completed || reset.Revision != 3 {
		t.Fatal("start from beginning did not reset completion")
	}
	if items, err := store.List(ctx); err != nil || len(items) != 0 {
		t.Fatal("progress implicitly collected video")
	}
	if jobs, err := store.ListDownloads(ctx); err != nil || len(jobs) != 0 {
		t.Fatal("progress created media task")
	}
	if notes, err := store.ListNotes(ctx, id); err != nil || len(notes) != 0 {
		t.Fatal("progress created notes")
	}
}

func TestConcurrentProgressWritersOnlyOneSucceeds(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, b := readingprogress.NewService(store), readingprogress.NewService(store)
	ctx := context.Background()
	id := "bilibili:BV17x411w7KC"
	start := make(chan struct{})
	out := make(chan error, 2)
	var wg sync.WaitGroup
	for i, s := range []*readingprogress.Service{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Save(ctx, id, readingprogress.Input{PositionSeconds: float64(i + 1), DurationSeconds: 12})
			out <- err
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	succeeded, conflicted := 0, 0
	for err := range out {
		if err == nil {
			succeeded++
		} else if errors.Is(err, readingprogress.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatal("concurrent writers overwrote each other")
	}
	value, _ := a.Get(ctx, id)
	if value == nil || value.Revision != 1 {
		t.Fatal("revision not atomic")
	}
}

func TestProgressBoundaries(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := readingprogress.NewService(store)
	ctx := context.Background()
	id := "bilibili:BV17x411w7KC"
	for _, input := range []readingprogress.Input{
		{PositionSeconds: -1, DurationSeconds: 12}, {PositionSeconds: 13, DurationSeconds: 12}, {DurationSeconds: 0},
		{DurationSeconds: 604801}, {DurationSeconds: .0001}, {PositionSeconds: math.NaN(), DurationSeconds: 12},
		{DurationSeconds: math.Inf(1)}, {DurationSeconds: 12, Revision: -1}, {DurationSeconds: 12, Revision: 9007199254740991},
	} {
		if _, err := s.Save(ctx, id, input); !errors.Is(err, readingprogress.ErrInvalid) {
			t.Fatalf("invalid input accepted %+v", input)
		}
	}
	if _, err := s.Get(ctx, "bilibili:creator:12345"); !errors.Is(err, readingprogress.ErrInvalid) {
		t.Fatal("source identity accepted as video")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Save(canceled, id, readingprogress.Input{PositionSeconds: 3, DurationSeconds: 12}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not respected")
	}
	if value, _ := s.Get(ctx, id); value != nil {
		t.Fatal("invalid input created progress")
	}
}
