package ai

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSummaryTitleOverridesPersistAndApplyToCachedDocument(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ai")
	service, err := New(root, NewMemoryResults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	stored := SummaryTitleOverrides{Key: key, Overrides: []SummaryTitleOverride{{NodeID: "section-1", Title: "自定义章节"}, {NodeID: "section-1-unit-1", Title: "自定义主题"}}}
	if _, err := service.writeSummaryTitleOverrides(context.Background(), key, stored); err != nil {
		t.Fatal(err)
	}
	got, err := service.SummaryTitleOverrides(context.Background(), key)
	if err != nil || len(got.Overrides) != 2 {
		t.Fatal(got, err)
	}
	result := SummaryResult{Key: key, Document: &SummaryDocument{FormatVersion: summaryFormatVersion, Levels: summaryLevels, Sections: []SummarySection{{SectionID: "section-1", Title: "原章节", Units: []SummaryUnit{{UnitID: "section-1-unit-1", Topic: "原主题"}}}}}}
	if err := service.applySummaryTitleOverrides(context.Background(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Document.Sections[0].Title != "自定义章节" || result.Document.Sections[0].Units[0].Topic != "自定义主题" {
		t.Fatalf("overrides not applied: %#v", result.Document)
	}
	if _, err := service.DeleteSummaryTitleOverride(context.Background(), key, "section-1"); err != nil {
		t.Fatal(err)
	}
	got, err = service.SummaryTitleOverrides(context.Background(), key)
	if err != nil || len(got.Overrides) != 1 || got.Overrides[0].NodeID != "section-1-unit-1" {
		t.Fatalf("delete failed: %#v %v", got, err)
	}
}
