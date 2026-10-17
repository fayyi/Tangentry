package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/notes"
	"tangentry/internal/subscriptions"
)

func TestExportSnapshotCoversAllPagesLiteralFiltersAndRetainedSource(t *testing.T) {
	s := inboxTestStore(t)
	ctx := context.Background()
	source := inboxTestSource(t, s, "旧来源", "https://example.com/feed")
	item := inboxTestItem(34, "article")
	s.UpdateSubscription(ctx, source.ID, subscriptions.Snapshot{Items: []content.Item{item}}, 10)
	s.DeleteSubscription(ctx, source.ID)
	for i := 1; i <= 107; i++ {
		body := fmt.Sprintf("Common 中文😀 100%%_Alpha' %d", i)
		tags := []string{"portable"}
		contentID := item.ID
		if i%2 == 0 {
			tags = []string{"other"}
			contentID = "youtube:abcdefghijk"
		}
		if _, err := s.CreateNote(ctx, notes.Note{ID: fmt.Sprintf("%032x", i), ContentID: contentID, Body: body, Tags: tags, CreatedAt: int64(1000 + i/10), UpdatedAt: int64(1000 + i/10)}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.ExportNotes(ctx, notes.SearchQuery{}, "")
	if err != nil || len(all) != 107 || all[0].Note.ID != fmt.Sprintf("%032x", 107) {
		t.Fatal(len(all), err)
	}
	search, err := s.SearchNotes(ctx, notes.SearchQuery{Tag: "portable"})
	if err != nil || len(search) == 0 || search[0].Source == nil || search[0].Source.Title != item.Title || search[0].Source.URL != "" {
		t.Fatal("retained source metadata missing or URL leaked", search, err)
	}
	listed, err := s.ListNotes(ctx, item.ID)
	if err != nil || len(listed) == 0 || listed[0].Source != nil {
		t.Fatal("source metadata became stored note data", err)
	}
	for _, query := range []notes.SearchQuery{{Text: "common"}, {Text: "100%_alpha'"}, {Text: "中文😀"}} {
		rows, err := s.ExportNotes(ctx, query, "")
		if err != nil || len(rows) != 107 {
			t.Fatal(len(rows), err)
		}
	}
	filtered, err := s.ExportNotes(ctx, notes.SearchQuery{Tag: "portable", FromCreatedAt: 1004, ToCreatedAt: 1006}, item.ID)
	if err != nil || len(filtered) != 10 {
		t.Fatal(len(filtered), err)
	}
	for _, entry := range filtered {
		if entry.Source == nil || entry.Source.Title != item.Title || entry.Source.URL != item.URL || entry.Note.ContentID != item.ID || entry.Note.CreatedAt < 1004 || entry.Note.CreatedAt >= 1006 {
			t.Fatal(entry)
		}
	}
	// Snapshot rows are self-contained; a later edit cannot change an already read export.
	before := filtered[0]
	s.EditNote(ctx, before.Note.ID, notes.Edit{Body: "later edit"}, 2000)
	if !reflect.DeepEqual(filtered[0], before) {
		t.Fatal("materialized snapshot changed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.ExportNotes(canceled, notes.SearchQuery{}, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestExportLimitsNeverReturnTruncatedSuccess(t *testing.T) {
	for _, kind := range []string{"count", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			s := inboxTestStore(t)
			ctx := context.Background()
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			count, body := notes.ExportMaxNotes+1, "saved note"
			if kind == "bytes" {
				count = 2300
				body = strings.Repeat("x", 8000)
			}
			statement, err := tx.Prepare(`INSERT INTO notes(id,content_id,body,created_at,updated_at) VALUES(?,?,?,?,?)`)
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= count; i++ {
				if _, err = statement.Exec(fmt.Sprintf("%032x", i), "youtube:abcdefghijk", body, 10, 10); err != nil {
					t.Fatal(err)
				}
			}
			statement.Close()
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			rows, err := s.ExportNotes(ctx, notes.SearchQuery{}, "")
			if !errors.Is(err, notes.ErrExportLimit) || rows != nil {
				t.Fatal(len(rows), err)
			}
		})
	}
}
