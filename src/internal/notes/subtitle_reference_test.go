package notes_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestSubtitleReferencesPersistSearchExportAndSurviveOldClientEdits(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(store)
	position := 12.25
	references := []notes.SubtitleReference{
		{Start: 12.125, End: 16.75, Text: "第一句原话", Language: "zh-Hans"},
		{Start: 102.2, End: 105.9, Text: "独立原话搜索词 😀", Language: "en", TrackHash: strings.Repeat("b", 64)},
	}
	note, err := service.Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "我的理解", PositionSeconds: &position, SubtitleReferences: references})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	rows, err := service.List(ctx, note.ContentID)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].SubtitleReferences, references) {
		t.Fatalf("reopened references: %+v %v", rows, err)
	}
	page, err := service.Search(ctx, "独立原话搜索词", "")
	if err != nil || len(page.Notes) != 1 || page.Notes[0].ID != note.ID {
		t.Fatalf("reference search: %+v %v", page, err)
	}
	edited, err := service.Edit(ctx, note.ID, notes.Edit{Body: "旧客户端改正文", PositionSeconds: &position})
	if err != nil || !reflect.DeepEqual(edited.SubtitleReferences, references) {
		t.Fatalf("omission erased references: %+v %v", edited, err)
	}
	export, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{Query: "独立原话搜索词"})
	if err != nil {
		t.Fatal(err)
	}
	manifest, files := unpackExport(t, export)
	if len(manifest.Entries) != 1 || !reflect.DeepEqual(manifest.Entries[0].Note.SubtitleReferences, references) {
		t.Fatal("manifest lost exact subtitle times", manifest)
	}
	markdown := files[manifest.Entries[0].File]
	for _, text := range []string{"subtitle_references", "12.125", "16.75", "独立原话搜索词", "## 关联时间", "&t=102s", `"trackHash": "` + strings.Repeat("b", 64) + `"`} {
		if !strings.Contains(markdown, text) {
			t.Fatalf("export missing %q", text)
		}
	}
	stale := note.UpdatedAt
	if _, err := service.Edit(ctx, note.ID, notes.Edit{Body: "过时的清除", ExpectedUpdatedAt: &stale, SubtitleReferences: json.RawMessage(`[]`)}); !errors.Is(err, notes.ErrConflict) {
		t.Fatal("stale edit removed references", err)
	}
	cleared, err := service.Edit(ctx, note.ID, notes.Edit{Body: "正文仍在", PositionSeconds: &position, SubtitleReferences: json.RawMessage(`null`)})
	if err != nil || len(cleared.SubtitleReferences) != 0 || cleared.PositionSeconds == nil || *cleared.PositionSeconds != position {
		t.Fatalf("clear changed main time: %+v %v", cleared, err)
	}
}

func TestSubtitleReferenceValidationAndFailedEditsAreAtomic(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	ctx := context.Background()
	valid := notes.SubtitleReference{Start: 0, End: 1.5, Text: "一句原话", Language: "zh"}
	for _, mutate := range []func(*notes.SubtitleReference){
		func(r *notes.SubtitleReference) { r.Start = -1 },
		func(r *notes.SubtitleReference) { r.End = r.Start },
		func(r *notes.SubtitleReference) { r.End = 604801 },
		func(r *notes.SubtitleReference) { r.Start = math.NaN() },
		func(r *notes.SubtitleReference) { r.End = math.Inf(1) },
		func(r *notes.SubtitleReference) { r.Text = " \n" },
		func(r *notes.SubtitleReference) { r.Text = strings.Repeat("字", 2001) },
		func(r *notes.SubtitleReference) { r.Language = "" },
		func(r *notes.SubtitleReference) { r.Language = strings.Repeat("x", 65) },
		func(r *notes.SubtitleReference) { r.TrackHash = "not-a-version" },
		func(r *notes.SubtitleReference) { r.TrackHash = strings.Repeat("a", 63) },
		func(r *notes.SubtitleReference) { r.TrackHash = strings.Repeat("A", 64) },
	} {
		reference := valid
		mutate(&reference)
		if _, err := service.Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "正文", SubtitleReferences: []notes.SubtitleReference{reference}}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("invalid reference accepted", reference, err)
		}
	}
	if _, err := service.Create(ctx, notes.Input{Body: "独立随记", SubtitleReferences: []notes.SubtitleReference{valid}}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("standalone memo accepted media", err)
	}
	if _, err := service.Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "正文", SubtitleReferences: []notes.SubtitleReference{valid, valid}}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("duplicate reference accepted", err)
	}
	var many []notes.SubtitleReference
	for i := 0; i < 20; i++ {
		many = append(many, notes.SubtitleReference{Start: float64(i), End: float64(i) + .5, Text: strings.Repeat("字", 400), Language: "zh"})
	}
	if notes.ValidateSubtitleReferences(many) != nil {
		t.Fatal("valid limit rejected")
	}
	many[0].Text += "字"
	if !errors.Is(notes.ValidateSubtitleReferences(many), notes.ErrInvalid) {
		t.Fatal("total text limit not enforced")
	}
	many[0].Text = "字"
	many = append(many, notes.SubtitleReference{Start: 30, End: 31, Text: "字", Language: "zh"})
	if !errors.Is(notes.ValidateSubtitleReferences(many), notes.ErrInvalid) {
		t.Fatal("reference count limit not enforced")
	}
	note, err := service.Create(ctx, notes.Input{ContentID: "youtube:abcdefghijk", Body: "保留正文", SubtitleReferences: []notes.SubtitleReference{valid}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `false`, `[{"start":0,"end":1,"text":"原话","language":"zh","extra":"bad"}]`, `[{"start":0,"end":1,"text":"原话","language":"zh","trackHash":"bad"}]`, `[{"start":0,"end":1,"text":"原话","language":"zh","trackHash":123}]`, `[] {}`, `[null]`} {
		if _, err := service.Edit(ctx, note.ID, notes.Edit{Body: "不能写入", SubtitleReferences: json.RawMessage(raw)}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("malformed references accepted", raw, err)
		}
	}
	anchor := notes.TextAnchor{Quote: "原文", Start: 0, End: 2, SourceHash: strings.Repeat("a", 64)}
	data, _ := json.Marshal(anchor)
	if _, err := service.Edit(ctx, note.ID, notes.Edit{Body: "不能混用", TextAnchor: data}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("article anchor mixed with subtitle", err)
	}
	rows, err := service.List(ctx, note.ContentID)
	if err != nil || rows[0].Body != "保留正文" || rows[0].UpdatedAt != note.UpdatedAt || !reflect.DeepEqual(rows[0].SubtitleReferences, []notes.SubtitleReference{valid}) {
		t.Fatal("failed edit changed note", rows, err)
	}
}
