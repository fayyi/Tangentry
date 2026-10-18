package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"tangentry/internal/preferences"
)

// ThemePreferences keeps interface preferences outside the origin-scoped
// browser storage. The file lives in the stable application data directory.
type ThemePreferences struct {
	path string
	mu   sync.Mutex
}

type themeRecord struct {
	Theme string `json:"theme"`
}

func NewThemePreferences(path string) *ThemePreferences {
	return &ThemePreferences{path: path}
}

func (s *ThemePreferences) Load(ctx context.Context) (preferences.Theme, bool, error) {
	if err := ctx.Err(); err != nil {
		return preferences.ThemeSystem, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return preferences.ThemeSystem, false, nil
	}
	if err != nil {
		return preferences.ThemeSystem, false, err
	}
	var record themeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return preferences.ThemeSystem, false, nil
	}
	theme := preferences.ParseTheme(record.Theme)
	if theme != preferences.Theme(record.Theme) {
		return preferences.ThemeSystem, false, nil
	}
	return theme, true, nil
}

func (s *ThemePreferences) Save(ctx context.Context, theme preferences.Theme) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !theme.Valid() {
		return errors.New("invalid theme preference")
	}
	data, err := json.Marshal(themeRecord{Theme: string(theme)})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".theme-preferences-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, s.path)
}
