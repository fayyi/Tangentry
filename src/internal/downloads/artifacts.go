package downloads

import (
	"context"
	"errors"
	"io/fs"
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
	if job.State != "completed" {
		return nil, ErrNotFound
	}
	fileName, err = NormalizeArtifactPath(fileName)
	if err != nil {
		return nil, ErrNotFound
	}
	directory, err := s.jobOutputDirectory(job)
	if err != nil {
		return nil, ErrNotFound
	}
	name, err := safeArtifactFilePath(directory, fileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrInvalid) {
			return nil, ErrNotFound
		}
		return nil, err
	}
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
	return s.completedPath(ctx, rawURL, nil, nil, nil)
}

func (s *Service) CompletedPathFor(ctx context.Context, rawURL string, height int, options Options, subdirectory string) (string, error) {
	if err := options.Normalize(); err != nil {
		return "", err
	}
	subdirectory, err := NormalizeSubdirectory(subdirectory)
	if err != nil {
		return "", err
	}
	return s.completedPath(ctx, rawURL, &options, &height, &subdirectory)
}

func (s *Service) completedPath(ctx context.Context, rawURL string, options *Options, height *int, subdirectory *string) (string, error) {
	canonical, err := NormalizeURL(rawURL)
	if err != nil {
		return "", ErrInvalid
	}
	jobs, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	for _, job := range jobs {
		if !sameURL(job.URL, canonical) || job.State != "completed" || job.Mode == "audio" || options != nil && !sameOptions(job.Options, *options) || height != nil && job.MaxHeight != *height || subdirectory != nil && job.Subdirectory != *subdirectory {
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
	names := make([]string, 0)
	primary, err := NormalizeArtifactPath(primary)
	if err != nil {
		return nil, ErrNotFound
	}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		name, err := NormalizeArtifactPath(filepath.ToSlash(relative))
		if err != nil {
			return nil
		}
		if name != primary && !slices.Contains([]string{".mp4", ".mkv", ".webm", ".mov", ".flv", ".ts", ".m4v", ".avi", ".srt", ".vtt", ".ass", ".lrc", ".ttml", ".srv1", ".srv2", ".srv3", ".json", ".json3", ".xml", ".jpg", ".jpeg", ".png", ".webp", ".nfo"}, strings.ToLower(filepath.Ext(name))) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Size() > 0 {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !slices.Contains(names, primary) {
		return nil, ErrNotFound
	}
	return names, nil
}

func validArtifactPath(name string) bool {
	_, err := NormalizeArtifactPath(name)
	return err == nil
}

func safeArtifactFilePath(directory, name string) (string, error) {
	name, err := NormalizeArtifactPath(name)
	if err != nil {
		return "", err
	}
	current := directory
	parts := strings.Split(filepath.FromSlash(name), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || index < len(parts)-1 && !info.IsDir() {
			return "", ErrInvalid
		}
	}
	return current, nil
}
