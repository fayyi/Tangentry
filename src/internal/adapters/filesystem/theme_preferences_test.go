package filesystem

import (
	"context"
	"os"
	"strings"
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

func TestPreferencesStoreValidatesKeysAndValueSize(t *testing.T) {
	store := NewPreferences(t.TempDir() + "/preferences.json")
	if err := store.Set(context.Background(), "other", "value"); err == nil {
		t.Fatal("unsupported preference key was accepted")
	}
	if err := store.Set(context.Background(), preferences.ThemeKey, strings.Repeat("x", preferences.MaxValueSize+1)); err == nil {
		t.Fatal("oversized preference value was accepted")
	}
}

func TestPreferencesStoreKeepsExistingValues(t *testing.T) {
	store := NewPreferences(t.TempDir() + "/preferences.json")
	if err := store.Set(context.Background(), preferences.ThemeKey, string(preferences.ThemeDark)); err != nil {
		t.Fatal(err)
	}
	value, saved, err := store.Get(context.Background(), preferences.ThemeKey)
	if err != nil || !saved || value != string(preferences.ThemeDark) {
		t.Fatalf("stored preference = %q saved=%v err=%v", value, saved, err)
	}
}

func TestPreferencesStoreReadsLegacyThemeRecord(t *testing.T) {
	path := t.TempDir() + "/preferences.json"
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewPreferences(path)
	value, saved, err := store.Get(context.Background(), preferences.ThemeKey)
	if err != nil || !saved || value != string(preferences.ThemeDark) {
		t.Fatalf("legacy preference = %q saved=%v err=%v", value, saved, err)
	}
}

func TestPreferencesStoreReplacesLegacyThemeWhenWriting(t *testing.T) {
	path := t.TempDir() + "/preferences.json"
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewPreferences(path)
	if err := store.Set(context.Background(), preferences.ThemeKey, string(preferences.ThemeLight)); err != nil {
		t.Fatal(err)
	}
	value, saved, err := store.Get(context.Background(), preferences.ThemeKey)
	if err != nil || !saved || value != string(preferences.ThemeLight) {
		t.Fatalf("updated legacy preference = %q saved=%v err=%v", value, saved, err)
	}
}
