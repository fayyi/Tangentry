package notes_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
	"testing"
)

func TestSearchAcrossSourcesIsLiteralPaginatedAndIndependent(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	service := notes.NewService(store)
	for i := 1; i <= 107; i++ {
		id := fmt.Sprintf("%032x", i)
		content := contentID
		if i%2 == 0 {
			content = "bilibili:BV1Pyta66EDh"
		}
		body := fmt.Sprintf("正文 %d Common 中文😀", i)
		if i == 1 {
			body += " 100%_Alpha'"
		}
		if _, err := store.CreateNote(ctx, notes.Note{ID: id, ContentID: content, Body: body, CreatedAt: int64(1000 + i/10), UpdatedAt: int64(1000 + i/10)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := service.Search(ctx, "", "")
	if err != nil || len(page.Notes) != 50 || page.NextCursor == "" || page.Notes[0].ID != fmt.Sprintf("%032x", 107) {
		t.Fatalf("first page: %+v %v", page, err)
	}
	boundary := page.Notes[49]
	if err := service.Delete(ctx, boundary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Edit(ctx, page.Notes[0].ID, notes.Edit{Body: "updated without changing rank"}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, note := range page.Notes {
		seen[note.ID] = true
	}
	for page.NextCursor != "" {
		page, err = service.Search(ctx, "", page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, note := range page.Notes {
			if seen[note.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[note.ID] = true
		}
	}
	if len(seen) != 107 {
		t.Fatal("deleted boundary skipped other notes", len(seen))
	}
	for _, query := range []string{"100%_alpha'", "中文😀", " Common "} {
		page, err := service.Search(ctx, query, "")
		if err != nil || len(page.Notes) == 0 || page.Query != strings.TrimSpace(query) {
			t.Fatal("literal/Unicode search failed", err)
		}
		if query == "100%_alpha'" && (len(page.Notes) != 1 || page.Notes[0].ID != fmt.Sprintf("%032x", 1)) {
			t.Fatal("wildcards interpreted")
		}
	}
	for _, query := range []string{"' OR 1=1 --", "no such note"} {
		page, err := service.Search(ctx, query, "")
		if err != nil || page.Notes == nil || len(page.Notes) != 0 || page.NextCursor != "" {
			t.Fatal("empty/search isolation failed")
		}
	}
	if items, _ := store.List(ctx); len(items) != 0 {
		t.Fatal("search created collections")
	}
	if jobs, _ := store.ListDownloads(ctx); len(jobs) != 0 {
		t.Fatal("search created downloads")
	}
	for _, cursor := range []string{"missing-separator", "0:" + fmt.Sprintf("%032x", 1), "-1:" + fmt.Sprintf("%032x", 1), "1000:invalid", strings.Repeat("x", 54)} {
		if _, err := service.Search(ctx, "", cursor); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("invalid cursor accepted")
		}
	}
	for _, query := range []string{strings.Repeat("中", 201), string([]byte{0xff})} {
		if _, err := service.Search(ctx, query, ""); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("invalid query accepted")
		}
	}
	if _, err := service.Search(ctx, strings.Repeat("😀", 200), ""); err != nil {
		t.Fatal("Unicode limit counts bytes", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Search(cancelled, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
