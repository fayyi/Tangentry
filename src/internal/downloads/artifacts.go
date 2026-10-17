package downloads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tangentry/internal/discovery"
)

func (s *Service) File(ctx context.Context, id string) (*os.File, error) {
	return s.Artifact(ctx, id, "")
}

func (s *Service) Artifact(ctx context.Context, id, fileName string) (*os.File, error) {
	job, err := s.repo.GetDownload(ctx, id)
	if err != nil {
		return nil, err
	}
	if fileName == "" {
		fileName = job.FileName
	} else if fileName != job.FileName {
		found := false
		for _, name := range job.Files {
			found = found || name == fileName
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	if job.State != "completed" || !validFileName(fileName) {
		return nil, ErrNotFound
	}
	name := filepath.Join(s.jobDirectory(job.ID), job.ID, fileName)
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, ErrNotFound
	}
	file, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return file, err
}

// NoteFrameFile checks that a completed download belongs to the note's source.
func (s *Service) NoteFrameFile(ctx context.Context, contentID, id string) (*os.File, error) {
	job, err := s.repo.GetDownload(ctx, id)
	if err != nil {
		return nil, err
	}
	platform, videoID, _, err := discovery.VideoURL(job.URL)
	if err != nil || platform+":"+videoID != contentID {
		return nil, ErrNotFound
	}
	return s.File(ctx, id)
}

// CompletedPath reuses the same file boundary as playback. It does not create
// downloads, wait for unfinished jobs, or resolve platform URLs over the network.
func (s *Service) CompletedPath(ctx context.Context, rawURL string) (string, error) {
	canonical, err := NormalizeURL(rawURL)
	if err != nil {
		return "", ErrInvalid
	}
	jobs, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	for _, job := range jobs {
		if !sameURL(job.URL, canonical) || job.State != "completed" || job.Mode == "audio" {
			continue
		}
		file, err := s.File(ctx, job.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		info, err := file.Stat()
		path := file.Name()
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		if info.Size() > 0 {
			return path, nil
		}
	}
	return "", ErrNotFound
}

func completedArtifacts(directory, primary string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if !validFileName(entry.Name()) || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		if entry.Name() != primary && !slices.Contains([]string{".srt", ".vtt", ".ass", ".lrc", ".ttml", ".srv1", ".srv2", ".srv3", ".json3"}, strings.ToLower(filepath.Ext(entry.Name()))) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() && info.Size() > 0 {
			names = append(names, entry.Name())
		}
	}
	if !slices.Contains(names, primary) {
		return nil, ErrNotFound
	}
	return names, nil
}

func validFileName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}
