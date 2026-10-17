package notes_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestTextAnchorRoundTripOldClientEditDetachAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "anchors.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(store)
	anchor := &notes.TextAnchor{Quote: "😀学习", Start: 3, End: 7, Prefix: "原文", Suffix: "上下文", SourceHash: strings.Repeat("a", 64)}
	created, err := service.Create(ctx, notes.Input{ContentID: "rss:" + strings.Repeat("b", 64), Body: "引用原文", TextAnchor: anchor})
	if err != nil || !reflect.DeepEqual(created.TextAnchor, anchor) {
		t.Fatal(created, err)
	}
	updated, err := service.Edit(ctx, created.ID, notes.Edit{Body: "旧客户端修改"})
	if err != nil || !reflect.DeepEqual(updated.TextAnchor, anchor) {
		t.Fatal("omitted field detached anchor", updated, err)
	}
	page, err := service.Search(ctx, "旧客户端", "")
	if err != nil || len(page.Notes) != 1 || !reflect.DeepEqual(page.Notes[0].TextAnchor, anchor) {
		t.Fatal("search lost anchor", page, err)
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	rows, err := service.List(ctx, created.ContentID)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].TextAnchor, anchor) {
		t.Fatal("restart lost anchor", rows, err)
	}
	detached, err := service.Edit(ctx, created.ID, notes.Edit{Body: "保留正文", TextAnchor: json.RawMessage(`null`)})
	if err != nil || detached.TextAnchor != nil {
		t.Fatal("explicit detach failed", detached, err)
	}
	if _, err := service.Edit(ctx, created.ID, notes.Edit{Body: "恢复关联", TextAnchor: json.RawMessage(`{"quote":"😀学习","start":3,"end":7,"prefix":"原文","suffix":"上下文","sourceHash":"` + strings.Repeat("a", 64) + `"}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestTextAnchorValidationRejectsInvalidOffsetsAndNestedFields(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "anchors.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := notes.NewService(store)
	ctx := context.Background()
	valid := notes.TextAnchor{Quote: "😀", Start: 0, End: 2, SourceHash: strings.Repeat("a", 64)}
	for _, mutate := range []func(*notes.TextAnchor){
		func(a *notes.TextAnchor) { a.Start = -1 }, func(a *notes.TextAnchor) { a.End = 1 }, func(a *notes.TextAnchor) { a.End = 2_000_001 },
		func(a *notes.TextAnchor) { a.Quote = "" }, func(a *notes.TextAnchor) { a.Prefix = strings.Repeat("中", 65) }, func(a *notes.TextAnchor) { a.SourceHash = "invalid" },
		func(a *notes.TextAnchor) { a.Quote = strings.Repeat("中", 2001); a.End = 2001 },
	} {
		a := valid
		mutate(&a)
		if _, err := s.Create(ctx, notes.Input{ContentID: contentID, Body: "原文", TextAnchor: &a}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("accepted invalid anchor", a, err)
		}
	}
	note, err := s.Create(ctx, notes.Input{ContentID: contentID, Body: "原文", TextAnchor: &valid})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"quote":"😀","start":0,"end":2,"sourceHash":"` + strings.Repeat("a", 64) + `","unknown":"private"}`, `[]`, `true`, `null {}`, `{"quote":"😀","start":0.5,"end":2}`} {
		if _, err := s.Edit(ctx, note.ID, notes.Edit{Body: "正文不能被损坏", TextAnchor: json.RawMessage(raw)}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("invalid patch accepted", err)
		}
	}
	rows, _ := s.List(ctx, contentID)
	if rows[0].Body != note.Body || !reflect.DeepEqual(rows[0].TextAnchor, &valid) {
		t.Fatal("rejected write changed note", rows)
	}
}
