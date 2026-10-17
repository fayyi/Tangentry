package sqlite

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"tangentry/internal/notes"
)

func TestMultipleFrameExportKeepsEveryImageAndAssociation(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	position := 12.0
	note, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "比较两个镜头", PositionSeconds: &position})
	if err != nil {
		t.Fatal(err)
	}
	first := notes.Frame{ID: strings.Repeat("a", 32), DownloadID: strings.Repeat("b", 32), PositionSeconds: 12, Approximate: true, Width: 64, Height: 36, CreatedAt: note.CreatedAt}
	if _, err = store.AppendNoteFrame(ctx, note.ID, note.UpdatedAt, &first, []byte("first image")); err != nil {
		t.Fatal(err)
	}
	note, err = store.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := notes.Frame{ID: strings.Repeat("c", 32), DownloadID: strings.Repeat("d", 32), PositionSeconds: 24, Approximate: true, Width: 64, Height: 36, CreatedAt: note.CreatedAt}
	if _, err = store.AppendNoteFrame(ctx, note.ID, note.UpdatedAt, &second, []byte("second image")); err != nil {
		t.Fatal(err)
	}
	result, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{ContentID: note.ContentID})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Data), int64(len(result.Data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		data, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[file.Name] = string(data)
	}
	if files["images/"+first.ID+".jpg"] != "first image" || files["images/"+second.ID+".jpg"] != "second image" {
		t.Fatal("export did not include both image payloads", files)
	}
	var manifest struct {
		Entries []notes.ExportEntry `json:"entries"`
	}
	if err = json.Unmarshal([]byte(files["tangentry-notes.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || len(manifest.Entries[0].Note.Frames) != 2 {
		t.Fatal("manifest lost screenshot metadata", manifest)
	}
	markdown := files[manifest.Entries[0].File]
	for _, frame := range []notes.Frame{first, second} {
		if !strings.Contains(markdown, "../images/"+frame.ID+".jpg") {
			t.Fatalf("markdown omitted frame %s: %s", frame.ID, markdown)
		}
	}
}

func TestAppendNoteFrameEnforcesScreenshotLimit(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	position := 1.0
	note, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "最多十张画面", PositionSeconds: &position})
	if err != nil {
		t.Fatal(err)
	}
	for index := range notes.MaxNoteFrames {
		frame := notes.Frame{ID: strings.Repeat(string("0123456789"[index]), 32), PositionSeconds: float64(index), Width: 64, Height: 36, CreatedAt: note.CreatedAt, Source: notes.FrameSourcePlayer}
		note, err = store.AppendNoteFrame(ctx, note.ID, note.UpdatedAt, &frame, []byte("image"))
		if err != nil {
			t.Fatalf("append frame %d: %v", index+1, err)
		}
	}
	extra := notes.Frame{ID: strings.Repeat("f", 32), PositionSeconds: 10, Width: 64, Height: 36, CreatedAt: note.CreatedAt, Source: notes.FrameSourcePlayer}
	if _, err = store.AppendNoteFrame(ctx, note.ID, note.UpdatedAt, &extra, []byte("image")); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("expected frame limit rejection, got %v", err)
	}
	stored, err := store.GetNote(ctx, note.ID)
	if err != nil || len(stored.Frames) != notes.MaxNoteFrames {
		t.Fatalf("frame limit rejection changed existing images: %d %v", len(stored.Frames), err)
	}
}

func TestMissingFramePayloadDoesNotPreventTextExport(t *testing.T) {
	store := inboxTestStore(t)
	ctx := context.Background()
	position := 2.125
	note, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "图片缺失时仍保留的正文", PositionSeconds: &position})
	if err != nil {
		t.Fatal(err)
	}
	frame := notes.Frame{ID: strings.Repeat("a", 32), DownloadID: strings.Repeat("b", 32), PositionSeconds: position, Approximate: true, Width: 64, Height: 36, CreatedAt: note.CreatedAt}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate restoring metadata without its binary attachment payload.
	if _, err := store.db.Exec(`UPDATE notes SET frame=? WHERE id=?`, string(data), note.ID); err != nil {
		t.Fatal(err)
	}
	result, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{ContentID: note.ContentID})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Data), int64(len(result.Data)))
	if err != nil {
		t.Fatal(err)
	}
	foundMarkdown, foundManifest := false, false
	for _, file := range archive.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(file.Name, "images/") {
			t.Fatal("phantom image file")
		}
		if strings.HasPrefix(file.Name, "notes/") {
			foundMarkdown = strings.Contains(string(body), note.Body) && strings.Contains(string(body), "图片未能读取") && !strings.Contains(string(body), "![")
		}
		if file.Name == "tangentry-notes.json" {
			foundManifest = strings.Contains(string(body), `"frameMissing": true`) && !strings.Contains(string(body), `"frameFile"`)
		}
	}
	if !foundMarkdown || !foundManifest {
		t.Fatal("missing image was hidden", foundMarkdown, foundManifest)
	}
}
