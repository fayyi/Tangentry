package downloads

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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

func (s *Service) jobDirectoryUnlocked(id string) string {
	if directory := s.locations.Jobs[id]; directory != "" {
		return directory
	}
	return s.originalDirectory
}

// NormalizeSubdirectory accepts a portable, relative directory path only.
func NormalizeSubdirectory(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if !utf8.ValidString(raw) || len(raw) > 512 || strings.ContainsAny(raw, `\\:`) || filepath.IsAbs(raw) {
		return "", ErrInvalid
	}
	parts := strings.Split(raw, "/")
	if len(parts) > 4 {
		return "", ErrInvalid
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || utf8.RuneCountInString(part) > 80 || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") {
			return "", ErrInvalid
		}
		for _, char := range part {
			if unicode.IsControl(char) || strings.ContainsRune(`<>"|?*`, char) {
				return "", ErrInvalid
			}
		}
		upper := strings.ToUpper(strings.TrimRight(part, ". "))
		if slices.Contains([]string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}, upper) {
			return "", ErrInvalid
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean != raw || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", ErrInvalid
	}
	return clean, nil
}

// NormalizeArtifactPath accepts a portable relative file path from the media
// runner. Unlike output folder names, nested paths are needed for Bilibili
// multi-page downloads (for example, Season 1/Video - S01E01.mp4).
func NormalizeArtifactPath(raw string) (string, error) {
	if raw == "" || !utf8.ValidString(raw) || len(raw) > 1024 || strings.ContainsAny(raw, `\\:`) || filepath.IsAbs(raw) {
		return "", ErrInvalid
	}
	parts := strings.Split(raw, "/")
	if len(parts) > 4 {
		return "", ErrInvalid
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 240 || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") {
			return "", ErrInvalid
		}
		for _, char := range part {
			if unicode.IsControl(char) || strings.ContainsRune(`<>"|?*`, char) {
				return "", ErrInvalid
			}
		}
		base := strings.ToUpper(strings.TrimSuffix(part, filepath.Ext(part)))
		if slices.Contains([]string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}, base) {
			return "", ErrInvalid
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
	if clean != raw || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", ErrInvalid
	}
	return clean, nil
}

func normalizeOutputDirectory(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.Contains(raw, "/") {
		return "", ErrInvalid
	}
	if _, err := NormalizeSubdirectory(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func jobOutputName(job Job) string {
	if job.OutputDirectory != "" {
		return job.OutputDirectory
	}
	return job.ID
}

func (s *Service) jobOutputDirectory(job Job) (string, error) {
	root := s.jobDirectory(job.ID)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0700); err != nil {
			return "", err
		}
	}
	return safeJobOutputDirectory(root, job)
}

func safeJobOutputDirectory(root string, job Job) (string, error) {
	if !filepath.IsAbs(root) || job.ID == "" || job.ID == "." || job.ID == ".." || filepath.Base(job.ID) != job.ID || strings.ContainsAny(job.ID, `/\\`) {
		return "", ErrInvalid
	}
	subdirectory, err := NormalizeSubdirectory(job.Subdirectory)
	if err != nil {
		return "", err
	}
	output, err := normalizeOutputDirectory(jobOutputName(job))
	if err != nil || !validArtifactPath(output) || strings.ContainsAny(output, `/\\`) {
		return "", ErrInvalid
	}
	directory := filepath.Join(root, filepath.FromSlash(subdirectory), output)
	relative, err := filepath.Rel(root, directory)
	if err != nil || !filepath.IsLocal(relative) {
		return "", ErrInvalid
	}
	current := root
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", ErrInvalid
		}
	}
	return directory, nil
}

func ensureJobOutputDirectory(root, subdirectory, output string) error {
	if !filepath.IsAbs(root) {
		return ErrInvalid
	}
	subdirectory, err := NormalizeSubdirectory(subdirectory)
	if err != nil {
		return err
	}
	output, err = normalizeOutputDirectory(output)
	if err != nil || !validArtifactPath(output) || strings.ContainsAny(output, `/\\`) {
		return ErrInvalid
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	directory := filepath.Join(root, filepath.FromSlash(subdirectory), output)
	relative, err := filepath.Rel(root, directory)
	if err != nil || !filepath.IsLocal(relative) {
		return ErrInvalid
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalid
		}
	}
	return nil
}

// Called with mu held. Active records and existing files both reserve a
// readable name so parallel jobs and changed policies never overwrite media.
func (s *Service) availableOutputDirectory(base, subdirectory string, jobs []Job) string {
	reserved := make(map[string]bool)
	for _, job := range jobs {
		if job.Subdirectory == subdirectory && s.jobDirectoryUnlocked(job.ID) == s.directory {
			reserved[jobOutputName(job)] = true
		}
	}
	parent := filepath.Join(s.directory, filepath.FromSlash(subdirectory))
	for index := 1; ; index++ {
		candidate := base
		if index > 1 {
			suffix := fmt.Sprintf(" (%d)", index)
			prefix := []rune(base)
			if len(prefix)+utf8.RuneCountInString(suffix) > 80 {
				prefix = prefix[:80-utf8.RuneCountInString(suffix)]
			}
			candidate = string(prefix) + suffix
		}
		if utf8.RuneCountInString(candidate) > 80 || reserved[candidate] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(parent, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		} else if err != nil {
			// The worker will surface an actionable storage error.
			return candidate
		}
	}
}
