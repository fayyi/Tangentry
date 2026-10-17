package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"tangentry/internal/content"
	"tangentry/internal/favoritesync"
)

func TestFavoritePolicyAndSnapshotPersistIndependently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.db")
	ctx := context.Background()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	source := favoritesync.Source{FolderID: "12345", Title: "明确测试收藏夹", Policy: favoritesync.DefaultPolicy(), CreatedAt: 1, LastSuccessAt: 2, Items: []content.Item{{ID: "bilibili:BV17x411w7KC", Title: "cached"}}, Pending: []string{"bilibili:BV17x411w7KC"}, Transfers: map[string]favoritesync.Transfer{"other": {JobID: "explicit-test", Attempts: 2}}, Active: true}
	source.Policy.TitleInclude = "教程，访谈"
	source.Policy.TitleExclude = "预告"
	source.Policy.AuthorInclude = "测试作者"
	if err := store.PutFavoriteSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	source.RestrictedUntil = 1791800000
	if err := store.PutFavoriteSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source.Active = false
	actual, err := store.GetFavoriteSource(ctx, source.FolderID)
	if err != nil || !reflect.DeepEqual(source, actual) {
		t.Fatal(actual, err)
	}
	if items, _ := store.List(ctx); len(items) != 0 {
		t.Fatal("sync created library")
	}
	if jobs, _ := store.ListDownloads(ctx); len(jobs) != 0 {
		t.Fatal("policy started downloads")
	}
	if err := store.DeleteFavoriteSource(ctx, source.FolderID); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListFavoriteSources(ctx)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}

func TestFavoriteRestrictionMigratesAndSurvivesSourceRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restriction.db")
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	source := favoritesync.Source{FolderID: "12345", Title: "旧来源", RestrictedUntil: 1791800000}
	if err := s.PutFavoriteSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFavoriteSource(ctx, source.FolderID); err != nil {
		t.Fatal(err)
	}
	if err := s.ExtendFavoriteRestriction(ctx, 1791800001); err != nil {
		t.Fatal(err)
	}
	if err := s.ExtendFavoriteRestriction(ctx, 10); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	until, err := s.FavoriteRestriction(ctx)
	if err != nil || until != 1791800001 {
		t.Fatal("shared restriction lost or shortened", until, err)
	}
}
