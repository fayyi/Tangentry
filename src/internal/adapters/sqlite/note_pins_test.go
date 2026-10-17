package sqlite

import (
	"context"
	"path/filepath"
	"tangentry/internal/notes"
	"testing"
)

func TestNotePinPersistsWithoutChangingBodyVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	service := notes.NewService(store)
	note, err := service.Create(ctx, notes.Input{Body: "保留草稿版本", Tags: []string{"研究"}})
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := service.SetPinned(ctx, note.ID, true)
	if err != nil || !pinned.Pinned || pinned.UpdatedAt != note.UpdatedAt || pinned.Body != note.Body {
		t.Fatal(pinned, err)
	}
	version := note.UpdatedAt
	edited, err := service.Edit(ctx, note.ID, notes.Edit{Body: "固定期间编辑", ExpectedUpdatedAt: &version})
	if err != nil || !edited.Pinned {
		t.Fatal(edited, err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service = notes.NewService(store)
	page, err := service.Search(ctx, "", "")
	if err != nil || len(page.Notes) != 1 || !page.Notes[0].Pinned {
		t.Fatal(page, err)
	}
	if _, err := service.Create(ctx, notes.Input{Body: "未固定"}); err != nil {
		t.Fatal(err)
	}
	filtered, err := service.SearchFiltered(ctx, "", "", notes.Filters{PinnedOnly: true})
	if err != nil || len(filtered.Notes) != 1 || filtered.Notes[0].ID != note.ID || !filtered.PinnedOnly {
		t.Fatal(filtered, err)
	}
	entries, err := store.ExportNotes(ctx, notes.SearchQuery{PinnedOnly: true}, "")
	if err != nil || len(entries) != 1 || entries[0].Note.ID != note.ID {
		t.Fatal(entries, err)
	}
	unpinned, err := service.SetPinned(ctx, note.ID, false)
	if err != nil || unpinned.Pinned || unpinned.UpdatedAt != edited.UpdatedAt {
		t.Fatal(unpinned, err)
	}
	filtered, err = service.SearchFiltered(ctx, "", "", notes.Filters{PinnedOnly: true})
	if err != nil || len(filtered.Notes) != 0 {
		t.Fatal(filtered, err)
	}
	if _, err := service.SetPinned(ctx, "invalid", true); err != notes.ErrInvalid {
		t.Fatal(err)
	}
	if _, err := service.SetPinned(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true); err != notes.ErrNotFound {
		t.Fatal(err)
	}
}
