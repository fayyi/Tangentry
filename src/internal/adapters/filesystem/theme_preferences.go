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
	Values      map[string]preferenceValue `json:"values"`
	LegacyTheme string                     `json:"theme,omitempty"`
}
type preferenceValue struct {
	Value    string `json:"value"`
	Revision int64  `json:"revision"`
}

func decodePreferenceRecord(data []byte) preferenceRecord {
	var record preferenceRecord
	if json.Unmarshal(data, &record) == nil {
		if record.Values == nil {
			record.Values = map[string]preferenceValue{}
		}
		return record
	}
	var legacy struct {
		Values map[string]string `json:"values"`
	}
	if json.Unmarshal(data, &legacy) == nil {
		record.Values = make(map[string]preferenceValue, len(legacy.Values))
		for key, value := range legacy.Values {
			record.Values[key] = preferenceValue{Value: value}
		}
		return record
	}
	return preferenceRecord{Values: map[string]preferenceValue{}}
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
		return preferenceRecord{Values: map[string]preferenceValue{}}, nil
	}
	if err != nil {
		return preferenceRecord{}, err
	}
	record := decodePreferenceRecord(data)
	if record.LegacyTheme != "" {
		record.Values[preferences.ThemeKey] = preferenceValue{Value: record.LegacyTheme}
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
	entry, ok := record.Values[key]
	return entry.Value, ok, nil
}

func (s *Preferences) Set(ctx context.Context, key, value string, revisions ...int64) error {
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
		record.Values = map[string]preferenceValue{}
	} else if err != nil {
		return err
	} else {
		record = decodePreferenceRecord(data)
	}
	if record.Values == nil {
		record.Values = map[string]preferenceValue{}
	}
	revision := int64(0)
	if len(revisions) > 0 {
		revision = revisions[0]
	}
	if current, ok := record.Values[key]; ok && revision > 0 && current.Revision >= revision {
		return nil
	}
	record.Values[key] = preferenceValue{Value: value, Revision: revision}
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
