package subscriptions_test

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/subscriptions"
)

func TestOPMLFoldersRoundTripAndDoNotReorganizeExistingSources(t *testing.T) {
	ctx := context.Background()
	first, err := sqlite.Open(filepath.Join(t.TempDir(), "group-export.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	service := subscriptions.NewService(first, nil)
	defer service.Close()
	groups := subscriptions.NewGroupService(first)
	group, _, err := groups.Create(ctx, "技术 & 播客")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := groups.Create(ctx, "空分组"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []subscriptions.Input{
		{Type: "rss", Query: "https://example.com/podcast.xml", Name: "播客"},
		{Type: "youtube", Query: "UCaaaaaaaaaaaaaaaaaaaaaa", Name: "频道"},
		{Type: "bilibili", Query: "12345", Name: "UP"},
	} {
		source, _, err := service.Create(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := groups.Assign(ctx, source.ID, group.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/ungrouped"}); err != nil {
		t.Fatal(err)
	}
	raw, err := service.ExportOPML(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Body struct {
			Outlines []struct {
				Text     string `xml:"text,attr"`
				Children []struct {
					XMLURL  string `xml:"xmlUrl,attr"`
					HTMLURL string `xml:"htmlUrl,attr"`
				} `xml:"outline"`
			} `xml:"outline"`
		} `xml:"body"`
	}
	if err := xml.Unmarshal(raw, &tree); err != nil || len(tree.Body.Outlines) != 2 || tree.Body.Outlines[0].Text != group.Name || len(tree.Body.Outlines[0].Children) != 3 {
		t.Fatal("folder structure lost", string(raw), err)
	}
	if strings.Contains(string(raw), "空分组") {
		t.Fatal("empty group exported")
	}
	second, err := sqlite.Open(filepath.Join(t.TempDir(), "group-import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other := subscriptions.NewService(second, nil)
	defer other.Close()
	otherGroups := subscriptions.NewGroupService(second)
	localGroup, _, err := otherGroups.Create(ctx, "本地分组")
	if err != nil {
		t.Fatal(err)
	}
	existing, _, err := other.Create(ctx, subscriptions.Input{Type: "bilibili", Query: "12345", Name: "本地名称"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherGroups.Assign(ctx, existing.ID, localGroup.ID, ""); err != nil {
		t.Fatal(err)
	}
	result, err := other.ImportOPML(ctx, raw)
	if err != nil || result.Created != 3 || result.Existing != 1 || result.Ungrouped != 0 {
		t.Fatal(result, err)
	}
	sources, err := other.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	importedGroups, err := otherGroups.List(ctx)
	if err != nil || len(importedGroups) != 2 {
		t.Fatal(importedGroups, err)
	}
	for _, source := range sources {
		if source.ID == existing.ID {
			if source.Name != existing.Name || source.GroupID != localGroup.ID {
				t.Fatal("existing source overwritten", source)
			}
		} else if source.URL == "https://example.com/ungrouped" {
			if source.GroupID != "" {
				t.Fatal("ungrouped source changed", source)
			}
		} else if source.GroupID == "" || source.GroupID == localGroup.ID || !source.Refresh.Enabled {
			t.Fatal("new source organization or refresh changed", source)
		}
	}
	if result, err := other.ImportOPML(ctx, raw); err != nil || result.Existing != 4 || result.Created != 0 {
		t.Fatal("duplicate import", result, err)
	}
	if actual, err := otherGroups.List(ctx); err != nil || len(actual) != 2 {
		t.Fatal("duplicate groups", actual, err)
	}
}

func TestOPMLNestedFoldersAndInvalidGroupNamesRemainExplicit(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "nested-import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()
	raw := `<opml><body><outline title="Parent"><outline text="Child"><outline text="First" xmlUrl="https://example.com/one"/><outline xmlUrl="https://example.com/two"/></outline></outline><outline text="` + strings.Repeat("长", 61) + `"><outline xmlUrl="https://example.com/three"/></outline></body></opml>`
	result, err := service.ImportOPML(context.Background(), []byte(raw))
	if err != nil || result.Created != 3 || result.Ungrouped != 1 {
		t.Fatal(result, err)
	}
	if result.Items[2].Status != "created" || result.Items[2].Reason != "来源已导入，分组未恢复" {
		t.Fatal("group failure hidden", result.Items)
	}
	groups, err := subscriptions.NewGroupService(store).List(context.Background())
	if err != nil || len(groups) != 1 || groups[0].Name != "Parent / Child" {
		t.Fatal(groups, err)
	}
	sources, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.URL == "https://example.com/three" {
			if source.GroupID != "" {
				t.Fatal("invalid group assigned")
			}
		} else if source.GroupID != groups[0].ID {
			t.Fatal("nested group not assigned", source)
		}
	}
}

func TestOPMLRoundTripIsLocalAndPreservesSourceKinds(t *testing.T) {
	ctx := context.Background()
	first, err := sqlite.Open(filepath.Join(t.TempDir(), "first.db"))
	if err != nil {
		t.Fatal(err)
	}
	service := subscriptions.NewService(first, nil)
	defer service.Close()
	if _, created, err := service.Create(ctx, subscriptions.Input{Type: "rss", Query: "https://example.com/podcast.xml", Name: "播客来源"}); err != nil || !created {
		t.Fatal("rss create", err)
	}
	if _, created, err := service.Create(ctx, subscriptions.Input{Type: "youtube", Query: "UCaaaaaaaaaaaaaaaaaaaaaa", Name: "YouTube 来源"}); err != nil || !created {
		t.Fatal("youtube create", err)
	}
	if _, created, err := service.Create(ctx, subscriptions.Input{Type: "bilibili", Query: "12345", Name: "B站来源"}); err != nil || !created {
		t.Fatal("bilibili create", err)
	}
	opml, err := service.ExportOPML(ctx)
	if err != nil || !strings.Contains(string(opml), "tangentryType=\"bilibili\"") || !strings.Contains(string(opml), "podcast.xml") {
		t.Fatalf("bad export: %v\n%s", err, opml)
	}
	second, err := sqlite.Open(filepath.Join(t.TempDir(), "second.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other := subscriptions.NewService(second, func(context.Context, string) (subscriptions.Snapshot, error) {
		t.Fatal("OPML import fetched a source")
		return subscriptions.Snapshot{}, nil
	})
	defer other.Close()
	result, err := other.ImportOPML(ctx, opml)
	if err != nil || result.Created != 3 || result.Failed != 0 || result.Skipped != 0 {
		t.Fatalf("bad import result: %+v %v", result, err)
	}
	sources, err := other.List(ctx)
	if err != nil || len(sources) != 3 {
		t.Fatalf("bad imported sources: %+v %v", sources, err)
	}
	duplicate, err := other.ImportOPML(ctx, opml)
	if err != nil || duplicate.Existing != 3 || duplicate.Created != 0 {
		t.Fatalf("duplicate import not idempotent: %+v %v", duplicate, err)
	}
}

func TestOPMLRejectsEmptyOrOversizedDocuments(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()
	if _, err := service.ImportOPML(context.Background(), nil); err != subscriptions.ErrInvalid {
		t.Fatal("empty OPML accepted")
	}
	if _, err := service.ImportOPML(context.Background(), []byte(strings.Repeat("x", 1<<20+1))); err != subscriptions.ErrInvalid {
		t.Fatal("oversized OPML accepted")
	}
	if _, err := service.ImportOPML(context.Background(), []byte(`<opml><body><outline text="no-address"/></body></opml>`)); err != subscriptions.ErrInvalid {
		t.Fatal("addressless OPML accepted")
	}
}

func TestOPMLValidatesWholeDocumentBeforeWriting(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "strict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()
	valid := `<opml><body><outline text="Keep" xmlUrl="https://example.com/feed"/></body></opml>`
	var tooMany strings.Builder
	tooMany.WriteString(`<opml><body>`)
	for i := 0; i < 501; i++ {
		fmt.Fprintf(&tooMany, `<outline xmlUrl="https://example.com/feed/%d"/>`, i)
	}
	tooMany.WriteString(`</body></opml>`)
	for label, raw := range map[string]string{
		"second root":    valid + valid,
		"trailing text":  valid + "discarded text",
		"broken tail":    valid + "<broken",
		"excess sources": tooMany.String(),
		"deep nesting":   `<opml><body>` + strings.Repeat(`<outline>`, 64) + `<outline xmlUrl="https://example.com/feed"/>` + strings.Repeat(`</outline>`, 64) + `</body></opml>`,
		"directive":      `<!DOCTYPE opml SYSTEM "https://example.com/private">` + valid,
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := service.ImportOPML(context.Background(), []byte(raw)); !errors.Is(err, subscriptions.ErrInvalid) {
				t.Fatal("invalid document accepted", err)
			}
			sources, err := service.List(context.Background())
			if err != nil || len(sources) != 0 {
				t.Fatal("validation created sources", sources, err)
			}
		})
	}
	result, err := service.ImportOPML(context.Background(), []byte(`<?xml version="1.0"?> <!-- before -->`+valid+`<!-- after -->`))
	if err != nil || result.Created != 1 {
		t.Fatal("valid comments or declaration rejected", result, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ImportOPML(cancelled, []byte(valid)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled import continued", err)
	}
}

func TestOPMLSourceLimitImportsAllFiveHundred(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "limit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := subscriptions.NewService(store, nil)
	defer service.Close()
	var raw strings.Builder
	raw.WriteString(`<opml><body><outline text="Folder">`)
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&raw, `<outline xmlUrl="https://example.com/feed/%d"/>`, i)
	}
	raw.WriteString(`</outline></body></opml>`)
	result, err := service.ImportOPML(context.Background(), []byte(raw.String()))
	if err != nil || result.Created != 500 || len(result.Items) != 500 {
		t.Fatal("valid boundary truncated", result, err)
	}
	sources, err := service.List(context.Background())
	if err != nil || len(sources) != 500 {
		t.Fatal("sources missing", len(sources), err)
	}
}
