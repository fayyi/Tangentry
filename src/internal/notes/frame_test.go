package notes_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func frameJPEG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 64, 36)), nil); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestNoteFramePersistenceConflictExportAndCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	position := 5.125
	note, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "自己的想法", PositionSeconds: &position})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(source, []byte("local video fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	jpegData := frameJPEG(t)
	resolve := func(_ context.Context, contentID, downloadID string) (*os.File, error) {
		if contentID != note.ContentID || downloadID != strings.Repeat("a", 32) {
			return nil, notes.ErrFrameSource
		}
		return os.Open(source)
	}
	frames := notes.NewFrames(store, resolve, func(context.Context, string, float64) ([]byte, error) { return jpegData, nil })
	input := notes.FrameInput{DownloadID: strings.Repeat("a", 32), PositionSeconds: 5.125, ExpectedUpdatedAt: note.UpdatedAt}
	saved, err := frames.Capture(ctx, note.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Frame == nil || saved.Body != note.Body || *saved.PositionSeconds != position || !saved.Frame.Approximate {
		t.Fatal(saved)
	}
	if _, err := frames.Capture(ctx, note.ID, input); !errors.Is(err, notes.ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
	if data, err := frames.Image(ctx, saved.ID, saved.Frame.ID); err != nil || !bytes.Equal(data, jpegData) {
		t.Fatal("image differs", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	input.ExpectedUpdatedAt = saved.UpdatedAt
	if _, err := frames.Capture(ctx, note.ID, input); !errors.Is(err, notes.ErrFrameSource) {
		t.Fatal("deleted source", err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	edited, err := service.Edit(ctx, saved.ID, notes.Edit{Body: "修改正文保留画面", PositionSeconds: &position, ExpectedUpdatedAt: &saved.UpdatedAt})
	if err != nil || edited.Frame == nil || edited.Frame.ID != saved.Frame.ID {
		t.Fatal("old client lost frame", err)
	}
	result, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{ContentID: note.ContentID})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Data), int64(len(result.Data)))
	if err != nil {
		t.Fatal(err)
	}
	foundImage, foundMarkdown, foundJSON := false, false, false
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "images/"+saved.Frame.ID+".jpg" {
			foundImage = bytes.Equal(data, jpegData)
		}
		if strings.HasPrefix(file.Name, "notes/") {
			foundMarkdown = strings.Contains(string(data), "../images/"+saved.Frame.ID+".jpg")
		}
		if file.Name == "tangentry-notes.json" {
			foundJSON = strings.Contains(string(data), `"frameFile": "images/`+saved.Frame.ID+`.jpg"`)
		}
	}
	if !foundImage || !foundMarkdown || !foundJSON {
		t.Fatal("incomplete offline export", foundImage, foundMarkdown, foundJSON)
	}
	if err := service.Delete(ctx, saved.ID, saved.UpdatedAt); !errors.Is(err, notes.ErrConflict) {
		t.Fatal("stale delete", err)
	}
	if _, err := store.NoteFrameData(ctx, saved.ID, saved.Frame.ID); err != nil {
		t.Fatal("conflict removed image", err)
	}
	if err := service.Delete(ctx, saved.ID, edited.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NoteFrameData(ctx, saved.ID, saved.Frame.ID); !errors.Is(err, notes.ErrNotFound) {
		t.Fatal("deleted image accessible", err)
	}
}

func TestNoteFrameCancellationAndConcurrentEditingPreserveOldData(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "frame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	position := 2.0
	note, _ := service.Create(ctx, notes.Input{ContentID: "bilibili:BV17x411w7KC", Body: "旧想法", PositionSeconds: &position})
	source := filepath.Join(t.TempDir(), "video")
	os.WriteFile(source, []byte("fixture"), 0600)
	started := make(chan struct{})
	release := make(chan struct{})
	frames := notes.NewFrames(store, func(context.Context, string, string) (*os.File, error) { return os.Open(source) }, func(ctx context.Context, _ string, _ float64) ([]byte, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return frameJPEG(t), nil
		}
	})
	input := notes.FrameInput{DownloadID: strings.Repeat("b", 32), PositionSeconds: 2, ExpectedUpdatedAt: note.UpdatedAt}
	done := make(chan error, 1)
	go func() { _, err := frames.Capture(ctx, note.ID, input); done <- err }()
	<-started
	if _, err := frames.Capture(ctx, note.ID, input); !errors.Is(err, notes.ErrFrameBusy) {
		t.Fatal("concurrent capture", err)
	}
	edited, err := service.Edit(ctx, note.ID, notes.Edit{Body: "另一个窗口的修改", PositionSeconds: &position, ExpectedUpdatedAt: &note.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, notes.ErrConflict) {
		t.Fatal("capture overwrote new version", err)
	}
	current, _ := store.GetNote(ctx, note.ID)
	if current.Frame != nil || current.Body != edited.Body {
		t.Fatal(current)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := frames.Capture(cancelled, note.ID, input); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
}
