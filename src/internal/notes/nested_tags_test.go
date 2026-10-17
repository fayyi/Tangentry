package notes_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestNestedTagsPersistFilterCountAndExportConsistently(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	service := notes.NewService(store)
	var original notes.Note
	for i, tags := range [][]string{
		{"#学习", "学习/阅读", "学习/阅读/书摘", "学习/阅读"},
		{"学习/阅读"},
		{"学习/写作"},
		{"学习/阅读延伸", "学习资料"},
		{"其他"},
		{"learn_more/topic"},
		{"learnXmore/topic"},
	} {
		note, err := service.Create(ctx, notes.Input{Body: fmt.Sprintf("分级记录 %d", i), Tags: tags})
		if err != nil {
			t.Fatal(tags, err)
		}
		if i == 0 {
			original = note
		}
	}
	if !reflect.DeepEqual(original.Tags, []string{"学习", "学习/阅读", "学习/阅读/书摘"}) {
		t.Fatal("normalization lost paths or duplicated tags", original.Tags)
	}
	for tag, count := range map[string]int{"学习": 4, "学习/阅读": 2, "学习/阅读/书摘": 1, "learn_more": 1, "学习资料": 1} {
		page, err := service.SearchFiltered(ctx, "", "", notes.Filters{Tag: tag})
		if err != nil || len(page.Notes) != count {
			t.Fatal("parent boundary filter incorrect", tag, page, err)
		}
		export, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{Tag: tag})
		if err != nil || export.Count != count {
			t.Fatal("export differs from search", tag, export.Count, err)
		}
	}
	tags, err := service.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, tag := range tags {
		counts[tag.Tag] = tag.Count
	}
	if counts["学习"] != 4 || counts["学习/阅读"] != 2 || counts["学习/阅读/书摘"] != 1 || counts["learn_more"] != 1 {
		t.Fatal("parent facets double-counted notes", counts)
	}
	for _, tag := range []string{"/学习", "学习/", "学习//阅读", "学习/../阅读", strings.Repeat("中", 32) + "/a"} {
		invalid := []string{tag}
		if _, err := service.Edit(ctx, original.ID, notes.Edit{Body: "must not replace", Tags: &invalid}); !errors.Is(err, notes.ErrInvalid) {
			t.Fatal("accepted invalid path", tag, err)
		}
	}
	preserved, err := service.List(ctx, original.ContentID)
	if err != nil || len(preserved) != 1 || !reflect.DeepEqual(preserved[0], original) {
		t.Fatal("invalid edit changed note", preserved, err)
	}
	for i := 0; i < 60; i++ {
		if _, err := service.Create(ctx, notes.Input{Body: fmt.Sprintf("翻页记录 %d", i), Tags: []string{"学习/阅读/书摘"}}); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	store, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service = notes.NewService(store)
	first, err := service.SearchFiltered(ctx, "", "", notes.Filters{Tag: "#学习/阅读"})
	if err != nil || len(first.Notes) != 50 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := service.SearchFiltered(ctx, "", first.NextCursor, notes.Filters{Tag: "学习/阅读"})
	if err != nil || len(second.Notes) != 12 || second.NextCursor != "" {
		t.Fatal(second, err)
	}
	seen := map[string]bool{}
	for _, note := range append(first.Notes, second.Notes...) {
		if seen[note.ID] {
			t.Fatal("pagination repeated a note", note.ID)
		}
		seen[note.ID] = true
	}
	export, err := notes.NewExporter(store).Export(ctx, notes.ExportInput{Tag: "学习/阅读"})
	if err != nil || export.Count != 62 {
		t.Fatal("reopened full export differs from paginated search", export.Count, err)
	}
}
