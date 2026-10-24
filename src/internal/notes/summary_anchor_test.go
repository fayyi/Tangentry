package notes_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/notes"
)

func TestSummaryAnchorRoundTripAndValidation(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "summary-anchor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := notes.NewService(store)
	anchor := &notes.SummaryAnchor{NodeID: "section-1", UnitID: "section-1-unit-2", Level: 3, SentenceIDs: []string{"sentence-4"}, Quote: "原文证据", Shown: "证据"}
	saved, err := service.Create(context.Background(), notes.Input{ContentID: "rss:" + strings.Repeat("a", 64), Body: "折叠阅读笔记", SummaryAnchor: anchor})
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.List(context.Background(), saved.ContentID)
	if err != nil || len(items) != 1 || items[0].SummaryAnchor == nil || items[0].SummaryAnchor.NodeID != anchor.NodeID || items[0].SummaryAnchor.Level != anchor.Level {
		t.Fatalf("summary anchor did not round-trip: %+v %v", items, err)
	}
	if _, err := service.Create(context.Background(), notes.Input{ContentID: saved.ContentID, Body: "bad", SummaryAnchor: &notes.SummaryAnchor{NodeID: "", UnitID: "u", Level: 0, Shown: "x"}}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("invalid summary anchor accepted: %v", err)
	}
	if _, err := service.Create(context.Background(), notes.Input{Body: "memo", SummaryAnchor: anchor}); !errors.Is(err, notes.ErrInvalid) {
		t.Fatalf("memo summary anchor accepted: %v", err)
	}
}
