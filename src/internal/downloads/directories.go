package downloads

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type directorySettings struct {
	Current string            `json:"current"`
	Jobs    map[string]string `json:"jobs"`
}

func (s *Service) loadDirectories() error {
	s.locations = directorySettings{Jobs: map[string]string{}}
	data, err := os.ReadFile(s.settingsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.locations); err != nil {
		return fmt.Errorf("invalid download directory settings: %w", err)
	}
	if s.locations.Jobs == nil {
		s.locations.Jobs = map[string]string{}
	}
	if s.locations.Current != "" {
		if !filepath.IsAbs(s.locations.Current) {
			return ErrInvalid
		}
		s.directory = s.locations.Current
	}
	for _, dir := range s.locations.Jobs {
		if !filepath.IsAbs(dir) {
			return ErrInvalid
		}
	}
	return nil
}

// Called with mu held. Keep settings beside application data, never in a
// user-selected destination, so changing folders does not orphan old downloads.
func (s *Service) saveDirectories() error {
	data, err := json.Marshal(s.locations)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.settingsPath), ".download-directories-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, s.settingsPath)
}

func (s *Service) Directory() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.directory
}

func (s *Service) DirectorySelected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locations.Current != ""
}

func (s *Service) SetDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return ErrInvalid
	}
	directory = filepath.Clean(directory)
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrInvalid
	}
	probe, err := os.CreateTemp(directory, ".tangentry-write-check-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return ErrBusy
	}
	previous := s.locations.Current
	s.locations.Current = directory
	if err := s.saveDirectories(); err != nil {
		s.locations.Current = previous
		return err
	}
	s.directory = directory
	return nil
}

func (s *Service) jobDirectory(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if directory := s.locations.Jobs[id]; directory != "" {
		return directory
	}
	return s.originalDirectory
}
