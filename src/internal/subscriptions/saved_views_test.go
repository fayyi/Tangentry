package subscriptions

import (
	"errors"
	"testing"
	"time"
)

func TestSavedViewDefinitionValidation(t *testing.T) {
	valid := SavedViewDefinition{Version: 1, Filters: SavedViewFilters{Query: "  Rust  ", Kind: "video", Read: "unread", Favorite: true, Platform: "youtube"}}
	view, err := savedViewFromInput(SavedViewInput{Name: "  本周视频 ", Definition: valid, Icon: "play", Color: "blue"})
	if err != nil || view.Name != "本周视频" || view.Definition.Filters.Query != "Rust" {
		t.Fatal(view, err)
	}
	for _, input := range []SavedViewInput{
		{Name: "", Definition: valid},
		{Name: "坏版本", Definition: SavedViewDefinition{Version: 9}},
		{Name: "坏类型", Definition: SavedViewDefinition{Version: 1, Filters: SavedViewFilters{Kind: "unknown"}}},
		{Name: "坏日期", Definition: SavedViewDefinition{Version: 1, Filters: SavedViewFilters{Since: 200, Until: 100}}},
		{Name: "坏图标", Definition: SavedViewDefinition{Version: 1}, Icon: "script"},
	} {
		if _, err := savedViewFromInput(input); !errors.Is(err, ErrSavedViewInvalid) {
			t.Fatalf("accepted %#v: %v", input, err)
		}
	}
}

func TestBuildSavedViewQueryUsesStableLocalDateBoundaries(t *testing.T) {
	location := time.FixedZone("fixture", -6*60*60)
	now := time.Date(2026, time.October, 17, 13, 20, 0, 0, location)
	view := SavedView{DefinitionVersion: 1, Definition: SavedViewDefinition{Version: 1, Filters: SavedViewFilters{Platform: "youtube", SourceID: "youtube:channel:UCaaaaaaaaaaaaaaaaaaaaaa", Kind: "video", Read: "unread", SinceOffset: "week", Favorite: true}}}
	query, err := BuildSavedViewQuery(view, "cursor", 20, now)
	if err != nil || query.Platform != "youtube" || query.SourceID == "" || query.Kind != "video" || query.Read != "unread" || !query.Favorite || query.Cursor != "cursor" || query.Limit != 20 {
		t.Fatal(query, err)
	}
	want := time.Date(2026, time.October, 12, 0, 0, 0, 0, location).Unix()
	if query.Since != want || query.Until != 0 {
		t.Fatalf("week boundary = %d, want %d", query.Since, want)
	}
}
