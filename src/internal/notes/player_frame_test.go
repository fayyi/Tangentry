package notes_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestPlayerFrameNotesSaveRetryAttachAndExport(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	frames := notes.NewFrames(store, func(context.Context, string, string) (*os.File, error) { return nil, notes.ErrFrameSource }, nil)
	position := 83.4
	image := frameJPEG(t)
	reference := notes.SubtitleReference{Start: 82, End: 85.5, Text: "这一句是截屏时的上下文", Language: "zh"}
	input := notes.ImageNoteInput{
		Note:  notes.Input{CreateID: strings.Repeat("c", 32), ContentID: "bilibili:BV1xx411c7mD", Body: "画面里的结构值得记住", PositionSeconds: &position, SubtitleReferences: []notes.SubtitleReference{reference}},
		Image: notes.FrameImage{Data: image, PositionSeconds: &position},
	}
	saved, err := frames.CreateWithImage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Frame == nil || saved.Frame.Source != notes.FrameSourcePlayer || saved.Frame.DownloadID != "" || saved.Frame.PositionSeconds != position || saved.Frame.Width != 64 {
		t.Fatal(saved.Frame)
	}
	retried, err := frames.CreateWithImage(ctx, input)
	if err != nil || retried.ID != saved.ID || retried.Frame.ID != saved.Frame.ID {
		t.Fatal("retry should return the first note", retried, err)
	}
	data, err := frames.Image(ctx, saved.ID, saved.Frame.ID)
	if err != nil || string(data) != string(image) {
		t.Fatal("stored image differs", err)
	}

	broken := input
	broken.Note.CreateID = strings.Repeat("d", 32)
	broken.Image.Data = []byte("not a jpeg")
	if _, err := frames.CreateWithImage(ctx, broken); !errors.Is(err, notes.ErrFrameImage) {
		t.Fatal("invalid image accepted", err)
	}
	memo := input
	memo.Note.CreateID = strings.Repeat("e", 32)
	memo.Note.ContentID = ""
	memo.Note.PositionSeconds = nil
	memo.Note.SubtitleReferences = nil
	if _, err := frames.CreateWithImage(ctx, memo); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("standalone memo accepted a video frame", err)
	}

	plain, err := notes.NewService(store).Create(ctx, notes.Input{ContentID: "bilibili:BV1xx411c7mD", Body: "稍后补一张截图"})
	if err != nil {
		t.Fatal(err)
	}
	later := 12.0
	attached, err := frames.AttachImage(ctx, plain.ID, plain.UpdatedAt, notes.FrameImage{Data: image, PositionSeconds: &later})
	if err != nil || attached.Frame == nil || attached.Frame.PositionSeconds != later || attached.Body != plain.Body {
		t.Fatal("attach failed", attached, err)
	}
	if _, err := frames.AttachImage(ctx, plain.ID, plain.UpdatedAt, notes.FrameImage{Data: image, PositionSeconds: &later}); !errors.Is(err, notes.ErrConflict) {
		t.Fatal("stale version accepted", err)
	}

	result, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{ContentID: "bilibili:BV1xx411c7mD"})
	if err != nil {
		t.Fatal(err)
	}
	_, files := unpackExport(t, result)
	var document string
	for name, body := range files {
		if strings.HasPrefix(name, "sources/") {
			document = body
		}
	}
	timeline := []string{"## 0:12 · 稍后补一张截图", "## 1:23 · 画面里的结构值得记住", "### 关联时间", "[1:23](https://www.bilibili.com/video/BV1xx411c7mD/?p=1&t=83)", "> 这一句是截屏时的上下文", "### 视频画面", "![视频画面 1:23](../images/" + saved.Frame.ID + ".jpg)", "### 我的记录\n\n画面里的结构值得记住"}
	last := -1
	for _, text := range timeline {
		at := strings.Index(document, text)
		if at < 0 || at < last && !strings.HasPrefix(text, "###") {
			t.Fatalf("source document missing or misordered %q:\n%s", text, document)
		}
		last = at
	}
	if files["images/"+saved.Frame.ID+".jpg"] != string(image) {
		t.Fatal("export missing captured image")
	}
}

func TestPlayerFrameCreateSupportsMultipleImagesAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	frames := notes.NewFrames(store, nil, nil)
	first, second := 12.5, 18.25
	input := notes.ImageNoteInput{
		Note:   notes.Input{CreateID: strings.Repeat("f", 32), ContentID: "bilibili:BV1xx411c7mD", Body: "两张画面属于同一个想法", PositionSeconds: &first},
		Images: []notes.FrameImage{{Data: frameJPEG(t), PositionSeconds: &first}, {Data: frameJPEG(t), PositionSeconds: &second}},
	}
	saved, err := frames.CreateWithImage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Frames) != 2 || saved.Frames[0].PositionSeconds != first || saved.Frames[1].PositionSeconds != second {
		t.Fatalf("expected both screenshots in order, got %+v", saved.Frames)
	}
	retried, err := frames.CreateWithImage(ctx, input)
	if err != nil || retried.ID != saved.ID || len(retried.Frames) != 2 {
		t.Fatalf("multi-image retry should return the same note and captures: %+v %v", retried, err)
	}
	for _, frame := range saved.Frames {
		if _, err := frames.Image(ctx, saved.ID, frame.ID); err != nil {
			t.Fatalf("missing image %s: %v", frame.ID, err)
		}
	}
}

func TestPlayerFrameCanAttachToAnchoredNote(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	anchor := &notes.TextAnchor{Quote: "引用", Start: 0, End: 2, SourceHash: strings.Repeat("b", 64)}
	note, err := notes.NewService(store).Create(ctx, notes.Input{
		ContentID:  "rss:" + strings.Repeat("b", 64),
		Body:       "关联原文的想法",
		TextAnchor: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	seconds := 12.5
	frames := notes.NewFrames(store, nil, nil)
	updated, err := frames.AttachImage(ctx, note.ID, note.UpdatedAt, notes.FrameImage{Data: frameJPEG(t), PositionSeconds: &seconds})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TextAnchor == nil || updated.TextAnchor.Quote != anchor.Quote || len(updated.Frames) != 1 || updated.Frames[0].PositionSeconds != seconds {
		t.Fatalf("anchored note lost its association or screenshot: %+v", updated)
	}
}
