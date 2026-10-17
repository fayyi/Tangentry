package sqlite

import (
	"context"
	"strings"
	"tangentry/internal/notes"
	"testing"
)

func TestNoteQuoteSearchAndExportUseOnlyReadableQuote(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	service := notes.NewService(s)
	hash := strings.Repeat("a", 64)
	quote := "Quoted 100%_摘录"
	article, err := service.Create(ctx, notes.Input{ContentID: "rss:" + hash, Body: "自己的想法", TextAnchor: &notes.TextAnchor{Quote: quote, Start: 0, End: len([]rune(quote)), Prefix: "定位前缀", Suffix: "定位后缀", SourceHash: hash}})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := service.Create(ctx, notes.Input{ContentID: "pdf:" + hash, Body: "PDF想法", PDFAnchor: &notes.PDFAnchor{DocumentID: hash, Page: 2, Quote: quote, Rects: []notes.PDFRect{{X: 0, Y: 0, Width: .5, Height: .1}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"quoted", "100%_摘录"} {
		page, err := service.Search(ctx, text, "")
		if err != nil || len(page.Notes) != 2 {
			t.Fatal(text, page, err)
		}
		entries, err := s.ExportNotes(ctx, notes.SearchQuery{Text: text}, "")
		if err != nil || len(entries) != 2 {
			t.Fatal(text, entries, err)
		}
	}
	for _, text := range []string{hash, "定位前缀", "定位后缀", "100XYZ摘录"} {
		page, err := service.Search(ctx, text, "")
		if err != nil || len(page.Notes) != 0 {
			t.Fatal("hidden fields matched", text, page, err)
		}
	}
	if _, err := service.SetPinned(ctx, article.ID, true); err != nil {
		t.Fatal(err)
	}
	page, err := service.SearchFiltered(ctx, quote, "", notes.Filters{PinnedOnly: true})
	if err != nil || len(page.Notes) != 1 || page.Notes[0].ID != article.ID {
		t.Fatal(page, err)
	}
	entries, err := s.ExportNotes(ctx, notes.SearchQuery{Text: quote}, pdf.ContentID)
	if err != nil || len(entries) != 1 || entries[0].Note.ID != pdf.ID {
		t.Fatal(entries, err)
	}
}
