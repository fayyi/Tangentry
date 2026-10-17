package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"tangentry/internal/content"
	"testing"
)

func TestOrderedAlbumSurvivesLibraryReopenWithoutSchemaChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item := content.Item{ID: "xiaohongshu:6411cf99000000001300b6d9", Platform: "xiaohongshu", PlatformID: "6411cf99000000001300b6d9", Kind: "image", Title: "Album", URL: "https://www.xiaohongshu.com/explore/6411cf99000000001300b6d9?xsec_token=fixture", Description: "Body", Images: []content.Image{{URL: "https://sns-webpic-qc.xhscdn.com/second", Width: 1200, Height: 800}, {URL: "https://sns-webpic-qc.xhscdn.com/first", Width: 900, Height: 1200}}}
	if _, err = s.Save(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.List(context.Background())
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0].Images, item.Images) || items[0].URL != item.URL {
		t.Fatal(items, err)
	}
}
