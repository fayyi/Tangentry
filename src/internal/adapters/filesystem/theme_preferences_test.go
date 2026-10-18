package filesystem

import (
	"context"
	"testing"

	"tangentry/internal/preferences"
)

func TestThemePreferencesSurviveReopen(t *testing.T) {
	path := t.TempDir() + "/preferences.json"
	first := NewThemePreferences(path)
	if err := first.Save(context.Background(), preferences.ThemeDark); err != nil {
		t.Fatal(err)
	}
	second := NewThemePreferences(path)
	theme, saved, err := second.Load(context.Background())
	if err != nil || !saved || theme != preferences.ThemeDark {
		t.Fatalf("loaded theme=%q saved=%v err=%v", theme, saved, err)
	}
}

func TestThemePreferencesRejectInvalidValue(t *testing.T) {
	store := NewThemePreferences(t.TempDir() + "/preferences.json")
	if err := store.Save(context.Background(), preferences.Theme("neon")); err == nil {
		t.Fatal("invalid theme was accepted")
	}
}
