package notes_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/notes"
)

type exportedManifest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Selection     notes.ExportInput   `json:"selection"`
	Entries       []notes.ExportEntry `json:"entries"`
}

func unpackExport(t *testing.T, result notes.ExportResult) (exportedManifest, map[string]string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(result.Data), int64(len(result.Data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range reader.File {
		if path.IsAbs(file.Name) || strings.Contains(file.Name, "\\") || strings.Contains(file.Name, "..") || path.Clean(file.Name) != file.Name {
			t.Fatal("unsafe archive path", file.Name)
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(data)
	}
	var manifest exportedManifest
	if err = json.Unmarshal([]byte(files["tangentry-notes.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest, files
}
func TestMarkdownExportKeepsSavedBodiesPropertiesAnchorsAndWhitelistedSources(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	id := "rss:" + strings.Repeat("a", 64)
	item := content.Item{ID: id, Platform: "rss", PlatformID: strings.Repeat("a", 64), Kind: "article", Title: "原站标题", Author: "作者", URL: "https://example.com/story", Description: "SOURCE_DESCRIPTION_PRIVATE_NOT_EXPORTED", MediaURL: "https://example.com/private-media"}
	if _, err = store.Save(ctx, item); err != nil {
		t.Fatal(err)
	}
	seconds := 9258.100000000364
	input := []notes.Input{
		{Body: "# ../.. \\ CON: <script>含😀 [x]\n\n```js\nconst value = 1;\n```", Tags: []string{"学习", "export"}},
		{ContentID: "youtube:abcdefghijk", Body: "视频学习", PositionSeconds: &seconds},
		{ContentID: id, Body: "> 原文😀\n\n自己的想法", TextAnchor: &notes.TextAnchor{Quote: "原文😀", Start: 3, End: 7, Prefix: "前文", Suffix: "后文", SourceHash: strings.Repeat("b", 64)}},
		{ContentID: "pdf:" + strings.Repeat("c", 64), Body: "文档批注", PDFAnchor: &notes.PDFAnchor{DocumentID: strings.Repeat("d", 64), Page: 2, Quote: "PDF 引用", Rects: []notes.PDFRect{{X: .1, Y: .2, Width: .3, Height: .1}}}},
	}
	service := notes.NewService(store)
	saved := map[string]notes.Note{}
	for _, value := range input {
		note, err := service.Create(ctx, value)
		if err != nil {
			t.Fatal(err)
		}
		saved[note.ID] = note
	}
	result, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{})
	if err != nil || result.Count != 4 {
		t.Fatal(result.Count, err)
	}
	manifest, files := unpackExport(t, result)
	for _, expected := range []string{"来源：[原站标题](<https://example.com/story>)", "关联时间：[2:34:18](https://www.youtube.com/watch?v=abcdefghijk&t=9258s)", "PDF：第 2 页", "> 原文😀", "> PDF 引用"} {
		if !strings.Contains(files["INDEX.md"], expected) {
			t.Fatalf("readable index missing %q", expected)
		}
	}
	if manifest.SchemaVersion != 1 || len(files) != 12 || !strings.Contains(files["sources/YouTube 视频.md"], "## 2:34:18 · 视频学习") || !strings.Contains(files["TAGS.md"], "## #学习") || strings.Contains(files["tangentry-notes.json"], "SOURCE_DESCRIPTION_PRIVATE_NOT_EXPORTED") || strings.Contains(files["tangentry-notes.json"], "private-media") {
		t.Fatal(manifest, len(files))
	}
	for _, entry := range manifest.Entries {
		if !reflect.DeepEqual(entry.Note, saved[entry.Note.ID]) {
			t.Fatal("original record changed", entry.Note)
		}
		raw := strings.TrimPrefix(files[entry.File], "---\n")
		metadata, body, ok := strings.Cut(raw, "\n---\n\n")
		if want := entry.Note.Body + "\n"; !ok || body != want && (entry.Note.PositionSeconds == nil || !strings.HasPrefix(body, "## 关联时间\n\n") || !strings.HasSuffix(body, "## 我的记录\n\n"+want)) {
			t.Fatal("Markdown body changed", body)
		}
		var properties map[string]any
		if err = json.Unmarshal([]byte(metadata), &properties); err != nil {
			t.Fatal(err)
		}
		if properties["tangentry_note_id"] != entry.Note.ID || properties["tangentry_content_id"] != entry.Note.ContentID || !strings.Contains(files["INDEX.md"], entry.Note.ID) {
			t.Fatal(properties)
		}
		if entry.Note.PositionSeconds != nil {
			if properties["position_seconds"] != float64(9258) || properties["position"] != "2:34:18" || properties["source_url_at_time"] != "https://www.youtube.com/watch?v=abcdefghijk&t=9258s" {
				t.Fatal(properties)
			}
		}
		if entry.Note.TextAnchor != nil {
			if properties["quoted_text"] != "原文😀" || entry.Source.Title != item.Title || entry.Source.Author != item.Author || entry.Source.URL != item.URL {
				t.Fatal(properties, entry.Source)
			}
		}
		if entry.Note.PDFAnchor != nil {
			if properties["pdf_page"] != float64(2) || properties["pdf_document_id"] != strings.Repeat("d", 64) {
				t.Fatal(properties)
			}
		}
		if _, err = time.Parse(time.RFC3339, properties["created"].(string)); err != nil {
			t.Fatal(err)
		}
	}
	for _, note := range saved {
		rows, err := service.List(ctx, note.ContentID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.ID == note.ID {
				found = reflect.DeepEqual(row, note)
			}
		}
		if !found {
			t.Fatal("export mutated saved note")
		}
	}
	if _, err = notes.NewExporter(store).Export(ctx, notes.ExportInput{ContentID: "rss:" + strings.Repeat("e", 64)}); !errors.Is(err, notes.ErrExportEmpty) {
		t.Fatal(err)
	}
	for _, input := range []notes.ExportInput{{Query: strings.Repeat("中", 201)}, {Tag: "bad tag"}, {From: 5, To: 5}, {ContentID: "../../escape"}} {
		if _, err = notes.NewExporter(store).Export(ctx, input); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

type waitingExportRepository struct{ entered chan struct{} }

func (r waitingExportRepository) ExportNotes(ctx context.Context, _ notes.SearchQuery, _ string) ([]notes.ExportEntry, error) {
	r.entered <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestExportIsBoundedCancelableAndReleasesItsSlot(t *testing.T) {
	entered := make(chan struct{}, 1)
	service := notes.NewExporter(waitingExportRepository{entered})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.Export(ctx, notes.ExportInput{}); done <- err }()
	<-entered
	if _, err := service.Export(context.Background(), notes.ExportInput{}); !errors.Is(err, notes.ErrExportBusy) {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _, err := service.Export(ctx2, notes.ExportInput{}); done <- err }()
	<-entered
	cancel2()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
