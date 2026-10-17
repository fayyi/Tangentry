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

func TestPDFAnchorsPersistAndLegacyEditsPreserveLocation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pdf-notes.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := notes.NewService(store)
	anchor := &notes.PDFAnchor{DocumentID: strings.Repeat("c", 64), Page: 2, Quote: "页面原文😀", Rects: []notes.PDFRect{{X: .1, Y: .2, Width: .3, Height: .05}, {X: .2, Y: .3, Width: .1, Height: .02}}}
	id := "rss:" + strings.Repeat("b", 64)
	note, err := s.Create(ctx, notes.Input{ContentID: id, Body: "PDF Markdown Memo", PDFAnchor: anchor})
	if err != nil || !reflect.DeepEqual(note.PDFAnchor, anchor) {
		t.Fatal(note, err)
	}
	updated, err := s.Edit(ctx, note.ID, notes.Edit{Body: "旧客户端编辑"})
	if err != nil || !reflect.DeepEqual(updated.PDFAnchor, anchor) {
		t.Fatal("legacy edit removed PDF anchor", updated, err)
	}
	page, err := s.Search(ctx, "旧客户端", "")
	if err != nil || len(page.Notes) != 1 || !reflect.DeepEqual(page.Notes[0].PDFAnchor, anchor) {
		t.Fatal(page, err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s = notes.NewService(store)
	rows, err := s.List(ctx, id)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].PDFAnchor, anchor) {
		t.Fatal("restart changed location", rows, err)
	}
	if other, _ := s.List(ctx, "pdf:"+strings.Repeat("d", 64)); len(other) != 0 {
		t.Fatal("RSS notes moved to file identity")
	}
	detached, err := s.Edit(ctx, note.ID, notes.Edit{Body: "保留 Memo", PDFAnchor: json.RawMessage(`null`)})
	if err != nil || detached.PDFAnchor != nil {
		t.Fatal(detached, err)
	}
}

func TestPDFAnchorValidationAndMixedAnchorEditRollback(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pdf-notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := notes.NewService(store)
	ctx := context.Background()
	valid := notes.PDFAnchor{DocumentID: strings.Repeat("a", 64), Page: 1, Quote: "引用", Rects: []notes.PDFRect{{X: .1, Y: .2, Width: .3, Height: .05}}}
	for _, mutate := range []func(*notes.PDFAnchor){
		func(a *notes.PDFAnchor) { a.Page = 0 }, func(a *notes.PDFAnchor) { a.Page = 100001 }, func(a *notes.PDFAnchor) { a.DocumentID = "../file" }, func(a *notes.PDFAnchor) { a.Quote = "" },
		func(a *notes.PDFAnchor) { a.Quote = strings.Repeat("中", 2001) }, func(a *notes.PDFAnchor) { a.Rects = nil }, func(a *notes.PDFAnchor) { a.Rects = make([]notes.PDFRect, 201) },
		func(a *notes.PDFAnchor) { a.Rects[0].X = -.1 }, func(a *notes.PDFAnchor) { a.Rects[0].Y = .99 }, func(a *notes.PDFAnchor) { a.Rects[0].Width = 0 },
		func(a *notes.PDFAnchor) { a.Rects[0].Height = math.Inf(1) }, func(a *notes.PDFAnchor) { a.Rects[0].X = math.NaN() },
	} {
		a := valid
		a.Rects = append([]notes.PDFRect(nil), valid.Rects...)
		mutate(&a)
		if _, err := s.Create(ctx, notes.Input{ContentID: contentID, Body: "批注", PDFAnchor: &a}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("invalid PDF anchor accepted", a, err)
		}
	}
	note, err := s.Create(ctx, notes.Input{ContentID: contentID, Body: "原 Memo", PDFAnchor: &valid})
	if err != nil {
		t.Fatal(err)
	}
	textAnchor := notes.TextAnchor{Quote: "引用", Start: 0, End: 2, SourceHash: strings.Repeat("a", 64)}
	textJSON, _ := json.Marshal(textAnchor)
	if _, err := s.Edit(ctx, note.ID, notes.Edit{Body: "不能改变原 Memo", TextAnchor: textJSON}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal("mixed anchors accepted", err)
	}
	rows, _ := s.List(ctx, contentID)
	if rows[0].Body != note.Body || !reflect.DeepEqual(rows[0].PDFAnchor, &valid) {
		t.Fatal("rejected transaction changed data", rows)
	}
	for _, raw := range []string{`{"documentId":"` + strings.Repeat("a", 64) + `","page":1,"quote":"引用","rects":[{"x":0,"y":0,"width":1,"height":1,"unknown":true}]}`, `[]`, `null {}`, `{"page":1.5}`} {
		if _, err := s.Edit(ctx, note.ID, notes.Edit{Body: "不能改变", PDFAnchor: json.RawMessage(raw)}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("malformed anchor accepted", err)
		}
	}
	if _, err := s.Edit(ctx, note.ID, notes.Edit{Body: "明确改为文章关联", TextAnchor: textJSON, PDFAnchor: json.RawMessage(`null`)}); err != nil {
		t.Fatal(err)
	}
}
