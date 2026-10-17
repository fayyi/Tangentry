package sqlite

import (
	"context"
	"path/filepath"
	"tangentry/internal/discovery"
	"testing"
)

func TestRecommendationPreferencesPersistWithoutDeletingContent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "preferences.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := discovery.NewPreferences(s)
	values, err := p.List(ctx)
	if err != nil || len(values) != 6 {
		t.Fatal(values, err)
	}
	for _, value := range values {
		if value.Enabled != (value.Source != "douyin") {
			t.Fatal(value)
		}
	}
	if err := p.Save(ctx, "linuxdo", false); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(ctx, "douyin", true); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(ctx, "subscriptions", false); err != discovery.ErrInvalidQuery {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p = discovery.NewPreferences(s)
	if enabled, err := p.Enabled(ctx, "linuxdo"); err != nil || enabled {
		t.Fatal(enabled, err)
	}
	if enabled, err := p.Enabled(ctx, "douyin"); err != nil || !enabled {
		t.Fatal(enabled, err)
	}
}
