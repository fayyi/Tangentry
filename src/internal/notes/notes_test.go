package notes_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

const contentID = "bilibili:BV17x411w7KC"

func TestConcurrentEditsHaveExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	for attempt := 0; attempt < 20; attempt++ {
		original, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "original"})
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			note notes.Note
			err  error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		var ready sync.WaitGroup
		ready.Add(2)
		for _, body := range []string{"first editor", "second editor"} {
			go func(body string) {
				ready.Done()
				<-start
				note, err := service.Edit(ctx, original.ID, notes.Edit{Body: body, ExpectedUpdatedAt: &original.UpdatedAt})
				results <- result{note, err}
			}(body)
		}
		ready.Wait()
		close(start)
		a, b := <-results, <-results
		if a.err != nil {
			a, b = b, a
		}
		if a.err != nil || !errors.Is(b.err, notes.ErrConflict) {
			t.Fatalf("expected one winner: %v / %v", a.err, b.err)
		}
		if err := service.Delete(ctx, original.ID, original.UpdatedAt); !errors.Is(err, notes.ErrConflict) {
			t.Fatalf("stale delete: %v", err)
		}
		if err := service.Delete(ctx, original.ID, a.note.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Edit(ctx, original.ID, notes.Edit{Body: "cannot resurrect", ExpectedUpdatedAt: &a.note.UpdatedAt}); !errors.Is(err, notes.ErrNotFound) {
			t.Fatalf("missing edit: %v", err)
		}
		if err := service.Delete(ctx, original.ID, a.note.UpdatedAt); !errors.Is(err, notes.ErrNotFound) {
			t.Fatalf("missing delete: %v", err)
		}
	}
}

func TestEditRejectsStaleVersionIncludingSameSecondWrites(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	first, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "原笔记"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Edit(ctx, first.ID, notes.Edit{Body: "另一窗口已保存", ExpectedUpdatedAt: &first.UpdatedAt})
	if err != nil || updated.UpdatedAt <= first.UpdatedAt {
		t.Fatalf("version not advanced: %+v %v", updated, err)
	}
	if _, err := service.Edit(ctx, first.ID, notes.Edit{Body: "旧版本覆盖", ExpectedUpdatedAt: &first.UpdatedAt}); !errors.Is(err, notes.ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	items, err := service.List(ctx, contentID)
	if err != nil || len(items) != 1 || items[0].Body != updated.Body {
		t.Fatalf("conflict changed stored note: %+v %v", items, err)
	}
	if _, err := service.Edit(ctx, first.ID, notes.Edit{Body: "基于新版本修改", ExpectedUpdatedAt: &updated.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, first.ID, updated.UpdatedAt); !errors.Is(err, notes.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	items, err = service.List(ctx, contentID)
	if err != nil || len(items) != 1 {
		t.Fatalf("stale delete removed note: %v", err)
	}
	if err := service.Delete(ctx, first.ID, items[0].UpdatedAt); err != nil {
		t.Fatal(err)
	}
}

func TestNotesPersistIndependentlyAndKeepContentIdentityOnEdit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(store)
	zero := 0.0
	first, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "  第一条笔记\n多行内容  ", PositionSeconds: &zero})
	if err != nil || first.PositionSeconds == nil || *first.PositionSeconds != 0 || first.Body != "第一条笔记\n多行内容" {
		t.Fatalf("bad creation: %+v %v", first, err)
	}
	second, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "第二条笔记"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Create(ctx, notes.Input{ContentID: "bilibili:BV1Pyta66EDh", Body: "另一段视频"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Edit(ctx, first.ID, notes.Edit{Body: "修改后的内容"})
	if err != nil || updated.ID != first.ID || updated.ContentID != first.ContentID || updated.CreatedAt != first.CreatedAt || updated.UpdatedAt < updated.CreatedAt || updated.PositionSeconds != nil {
		t.Fatalf("edit changed identity: %+v %v", updated, err)
	}
	items, err := service.List(ctx, contentID)
	if err != nil || len(items) != 2 || items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("unstable note order or wrong source: %+v %v", items, err)
	}
	if items, err := service.List(ctx, other.ContentID); err != nil || len(items) != 1 || items[0].ID != other.ID {
		t.Fatalf("source isolation failed: %+v %v", items, err)
	}
	if library, _ := store.List(ctx); len(library) != 0 {
		t.Fatal("writing notes automatically collected content")
	}
	if jobs, _ := store.ListDownloads(ctx); len(jobs) != 0 {
		t.Fatal("writing notes started media downloads")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	items, err = service.List(ctx, contentID)
	if err != nil || len(items) != 2 || items[1].Body != updated.Body {
		t.Fatalf("restart lost notes: %+v %v", items, err)
	}
	if err := service.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, first.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Fatal("missing note deletion was not reported")
	}
	if _, err := service.Edit(ctx, first.ID, notes.Edit{Body: "cannot resurrect"}); !errors.Is(err, notes.ErrNotFound) {
		t.Fatal("edit recreated a deleted note")
	}
	items, _ = service.List(ctx, contentID)
	if len(items) != 1 || items[0].ID != second.ID {
		t.Fatal("deletion affected another note")
	}
}

func TestNoteValidationAndUnicodeLength(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	ctx := context.Background()
	for _, input := range []notes.Input{
		{ContentID: contentID, Body: " \n\t "},
		{ContentID: "https://www.bilibili.com/video/BV17x411w7KC/", Body: "bad source"},
		{ContentID: "bilibili:12345", Body: "creator is not content"},
		{ContentID: contentID, Body: strings.Repeat("中", 8001)},
	} {
		if _, err := service.Create(ctx, input); !errors.Is(err, notes.ErrInvalid) {
			t.Fatalf("accepted invalid input: %+v %v", input.ContentID, err)
		}
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), 7*24*3600 + 1} {
		if _, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "invalid time", PositionSeconds: &value}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatalf("accepted invalid time %v", value)
		}
	}
	if _, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: strings.Repeat("😀", 8000)}); err != nil {
		t.Fatalf("Unicode limit counted bytes: %v", err)
	}
	if _, err := service.List(ctx, "bilibili:BVbad"); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("invalid list source accepted")
	}
	if _, err := service.Edit(ctx, "../bad-id", notes.Edit{Body: "hello"}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("invalid note id accepted")
	}
}

func TestYouTubeNotesPersistAndSearchWithoutChangingSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "youtube-notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(store)
	const youtubeID = "youtube:jNQXAC9IVRw"
	position := 1.25
	youtube, err := service.Create(ctx, notes.Input{ContentID: youtubeID, Body: "共享关键字：YouTube 时间笔记", PositionSeconds: &position})
	if err != nil {
		t.Fatal(err)
	}
	bili, err := service.Create(ctx, notes.Input{ContentID: contentID, Body: "共享关键字：B站笔记"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Create(ctx, notes.Input{ContentID: "youtube:YTplayer000", Body: "不同的 YouTube 视频"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Search(ctx, "共享关键字", "")
	if err != nil || len(page.Notes) != 2 {
		t.Fatalf("mixed source search: %+v %v", page, err)
	}
	for _, source := range []string{youtubeID, contentID, other.ContentID} {
		values, err := service.List(ctx, source)
		if err != nil || len(values) != 1 || values[0].ContentID != source {
			t.Fatalf("source isolation: %+v %v", values, err)
		}
	}
	updated, err := service.Edit(ctx, youtube.ID, notes.Edit{Body: "编辑的 YouTube 笔记", PositionSeconds: &position})
	if err != nil || updated.ContentID != youtubeID || updated.ID != youtube.ID || updated.CreatedAt != youtube.CreatedAt {
		t.Fatalf("source changed: %+v %v", updated, err)
	}
	for _, invalid := range []string{"youtube:", "youtube:jNQXAC9IVR", "youtube:jNQXAC9IVRwextra", "youtube:../jNQXAC9I", "youtube:video?x=1", "https://youtu.be/jNQXAC9IVRw", "xiaoyuzhou:jNQXAC9IVRw"} {
		if _, err := service.Create(ctx, notes.Input{ContentID: invalid, Body: "invalid"}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatalf("accepted %s", invalid)
		}
		if _, err := service.List(ctx, invalid); !errors.Is(err, notes.ErrInvalid) {
			t.Fatalf("listed %s", invalid)
		}
	}
	if values, _ := store.List(ctx); len(values) != 0 {
		t.Fatal("notes collected content")
	}
	if values, _ := store.ListDownloads(ctx); len(values) != 0 {
		t.Fatal("notes downloaded media")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	values, err := service.List(ctx, youtubeID)
	if err != nil || len(values) != 1 || values[0].Body != updated.Body || *values[0].PositionSeconds != position {
		t.Fatalf("restart lost YouTube note: %+v %v", values, err)
	}
	if err := service.Delete(ctx, youtube.ID); err != nil {
		t.Fatal(err)
	}
	if values, _ := service.List(ctx, contentID); len(values) != 1 || values[0].ID != bili.ID {
		t.Fatal("deletion crossed sources")
	}
	if values, _ := service.List(ctx, other.ContentID); len(values) != 1 || values[0].ID != other.ID {
		t.Fatal("deletion crossed YouTube videos")
	}
}

func TestNotesSupportIndependentMultimediaIdentities(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "mixed-notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	ctx := context.Background()
	for _, id := range []string{"rss:" + strings.Repeat("a", 64), "pdf:" + strings.Repeat("b", 64), "xiaoyuzhou:" + strings.Repeat("c", 24)} {
		note, err := service.Create(ctx, notes.Input{ContentID: id, Body: "多媒体批注"})
		if err != nil || note.ContentID != id {
			t.Fatal(note, err)
		}
		records, err := service.List(ctx, id)
		if err != nil || len(records) != 1 {
			t.Fatal(records, err)
		}
	}
}
