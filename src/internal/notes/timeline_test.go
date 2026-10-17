package notes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestStandaloneMemoTagsAndAtomicAnchorRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memos.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	service := notes.NewService(store)
	memo, err := service.Create(ctx, notes.Input{Body: "## 独立想法\n无需先导入内容", Tags: []string{"#Go", "学习", "go"}})
	if err != nil || memo.ContentID != "memo:"+memo.ID || len(memo.Tags) != 2 || memo.Tags[0] != "go" {
		t.Fatal(memo, err)
	}
	changed, err := service.Edit(ctx, memo.ID, notes.Edit{Body: "正文修改"})
	if err != nil || len(changed.Tags) != 2 || changed.CreatedAt != memo.CreatedAt {
		t.Fatal(changed, err)
	}
	// Reject attaching media metadata to a source-free memo, and roll back body/tags too.
	position := 1.0
	tags := []string{"changed"}
	if _, err := service.Edit(ctx, memo.ID, notes.Edit{Body: "must roll back", PositionSeconds: &position, Tags: &tags}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal(err)
	}
	items, _ := service.List(ctx, memo.ContentID)
	if len(items) != 1 || items[0].Body != "正文修改" || items[0].Tags[0] != "go" {
		t.Fatal(items)
	}
	invalidPDF := json.RawMessage(`{"documentId":"` + strings.Repeat("a", 64) + `","page":1,"quote":"x","rects":[{"x":0,"y":0,"width":0.1,"height":0.1}]}`)
	if _, err := service.Edit(ctx, memo.ID, notes.Edit{Body: "must roll back", PDFAnchor: invalidPDF}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, notes.Input{Body: "invalid memo time", PositionSeconds: &position}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"bad tag"}, {"<script>"}, {""}, {strings.Repeat("中", 33)}, strings.Split(strings.Repeat("tag,", 13), ",")} {
		if _, err := service.Create(ctx, notes.Input{Body: "invalid tags", Tags: bad}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("accepted", bad, err)
		}
	}
	facets, err := service.Tags(ctx)
	if err != nil || len(facets) != 2 || facets[0].Count != 1 {
		t.Fatal(facets, err)
	}
	if rows, _ := store.List(ctx); len(rows) != 0 {
		t.Fatal("memo created saved content")
	}
	if rows, _ := store.ListDownloads(ctx); len(rows) != 0 {
		t.Fatal("memo downloaded media")
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	items, err = service.List(ctx, memo.ContentID)
	if err != nil || len(items) != 1 || len(items[0].Tags) != 2 {
		t.Fatal(items, err)
	}
	empty := []string{}
	changed, err = service.Edit(ctx, memo.ID, notes.Edit{Body: "cleared tags", Tags: &empty})
	if err != nil || changed.Tags == nil || len(changed.Tags) != 0 {
		t.Fatal(changed, err)
	}
	facets, err = service.Tags(ctx)
	if err != nil || facets == nil || len(facets) != 0 {
		t.Fatal(facets, err)
	}
}

func TestTimelineFiltersApplyBeforePaginationAndUseExclusiveEnd(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	service := notes.NewService(store)
	for i := 0; i < 130; i++ {
		tag := "go"
		if i%2 != 0 {
			tag = "golang"
		}
		_, err := store.CreateNote(ctx, notes.Note{ID: fmt.Sprintf("%032x", i+1), ContentID: contentID, Body: "timeline 中文", Tags: []string{tag}, CreatedAt: int64(1000 + i), UpdatedAt: int64(1000 + i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	filter := notes.Filters{Tag: "#Go", From: 1000, To: 1120}
	first, err := service.SearchFiltered(ctx, "timeline", "", filter)
	if err != nil || len(first.Notes) != 50 || first.Tag != "go" || first.From != 1000 || first.To != 1120 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := service.SearchFiltered(ctx, "timeline", first.NextCursor, filter)
	if err != nil || len(second.Notes) != 10 || second.NextCursor != "" {
		t.Fatal(second, err)
	}
	for _, n := range append(first.Notes, second.Notes...) {
		if n.CreatedAt < 1000 || n.CreatedAt >= 1120 || n.Tags[0] != "go" {
			t.Fatal(n)
		}
	}
	for _, bad := range []notes.Filters{{Tag: "go%"}, {From: -1}, {To: -1}, {From: 1120, To: 1120}, {From: 1120, To: 1000}, {To: 253402300800}} {
		if _, err := service.SearchFiltered(ctx, "", "", bad); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("accepted filters", bad, err)
		}
	}
}
