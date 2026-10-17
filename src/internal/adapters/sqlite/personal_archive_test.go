package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/subtitles"
)

func TestPersonalRecommendationMigrationRetainsCredentialsAndMarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	item := content.Item{ID: "youtube:jNQXAC9IVRw", Platform: "youtube", PlatformID: "jNQXAC9IVRw", Kind: "video", Title: "Original", URL: "https://www.youtube.com/watch?v=jNQXAC9IVRw"}
	for i, account := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		scope := discovery.QueueScope{Platform: "youtube", Account: account}
		item.Title = []string{"Original", "Newest"}[i]
		if err := s.EnqueueRecommendations(ctx, scope, []content.Item{item}, int64(i+1)); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := s.MarkQueuedRecommendation(ctx, scope, item.ID, false, 3); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.db.Exec(`DELETE FROM schema_version WHERE version=23`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values, err := s.ListQueuedRecommendations(ctx, discovery.QueueScope{Platform: "youtube", Account: discovery.PersonalAccount}, 60)
	if err != nil || len(values) != 1 || values[0].Item.Title != "Newest" || values[0].SeenAt != 3 || values[0].FetchedAt != 1 {
		t.Fatalf("migration lost data: %+v %v", values, err)
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM recommendation_queue`).Scan(&count)
	if count != 3 {
		t.Fatal("original rows lost", count)
	}
}

func TestSubtitleCapturePersistsAndRetainsDistinctVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	saved := subtitles.NewSavedService(s)
	id := "youtube:jNQXAC9IVRw"
	track := subtitles.Track{Language: "en", Label: "English", Cues: []subtitles.Cue{{Start: 0, End: 2, Text: "First"}}}
	doc := subtitles.Document{Status: "available", Tracks: []subtitles.Track{track}}
	if err := saved.Capture(ctx, id, doc); err != nil {
		t.Fatal(err)
	}
	doc.Tracks[0].Cues[0].Text = "New version"
	for i := 0; i < 2; i++ {
		if err := saved.Capture(ctx, id, doc); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	values, err := subtitles.NewSavedService(s).List(ctx, id)
	if err != nil || len(values) != 1 || values[0].Track.Cues[0].Text != "First" {
		t.Fatal("active caption overwritten or lost", values, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subtitle_archive WHERE content_id=?`, id).Scan(&count); err != nil || count != 2 {
		t.Fatal("versions missing or duplicated", count, err)
	}
}
