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

// Preferences keeps interface preferences outside the origin-scoped browser
// storage. The file lives in the stable application data directory.
type Preferences struct {
	path string
	mu   sync.Mutex
}

type preferenceRecord struct {
	Values      map[string]string `json:"values"`
	LegacyTheme string            `json:"theme,omitempty"`
}

func NewPreferences(path string) *Preferences {
	return &Preferences{path: path}
}

func (s *Preferences) read(ctx context.Context) (preferenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return preferenceRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return preferenceRecord{Values: map[string]string{}}, nil
	}
	if err != nil {
		return preferenceRecord{}, err
	}
	var record preferenceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return preferenceRecord{Values: map[string]string{}}, nil
	}
	if record.Values == nil {
		record.Values = map[string]string{}
	}
	if record.LegacyTheme != "" {
		record.Values[preferences.ThemeKey] = record.LegacyTheme
	}
	return record, nil
}

func (s *Preferences) Get(ctx context.Context, key string) (string, bool, error) {
	if err := preferences.ValidateKey(key); err != nil {
		return "", false, err
	}
	record, err := s.read(ctx)
	if err != nil {
		return "", false, err
	}
	value, ok := record.Values[key]
	return value, ok, nil
}

func (s *Preferences) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := preferences.ValidateKey(key); err != nil {
		return err
	}
	if err := preferences.ValidateValue(value); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	var record preferenceRecord
	if errors.Is(err, os.ErrNotExist) {
		record.Values = map[string]string{}
	} else if err != nil {
		return err
	} else if json.Unmarshal(data, &record) != nil {
		record.Values = map[string]string{}
	}
	if record.Values == nil {
		record.Values = map[string]string{}
	}
	record.Values[key] = value
	if key == preferences.ThemeKey {
		record.LegacyTheme = ""
	}
	data, err = json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".preferences-*")
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

// ThemePreferences is retained as a focused constructor for existing callers.
type ThemePreferences = Preferences

func NewThemePreferences(path string) *Preferences { return NewPreferences(path) }

func (s *Preferences) Load(ctx context.Context) (preferences.Theme, bool, error) {
	value, saved, err := s.Get(ctx, preferences.ThemeKey)
	if err != nil {
		return preferences.ThemeSystem, false, err
	}
	theme := preferences.ParseTheme(value)
	return theme, saved && theme == preferences.Theme(value), nil
}

func (s *Preferences) Save(ctx context.Context, theme preferences.Theme) error {
	if !theme.Valid() {
		return errors.New("invalid theme preference")
	}
	return s.Set(ctx, preferences.ThemeKey, string(theme))
}
