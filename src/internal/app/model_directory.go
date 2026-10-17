package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"tangentry/internal/transcription"
)

type modelDirectoryState struct {
	modelDownloads *transcription.ModelDownloads
	transcription  *transcription.Service
	modelDir       string
}

func (a *Application) ModelDirectory() string { return a.shell.ModelDirectory() }

func (a *Application) SetModelDirectory(ctx context.Context, selected string) error {
	return a.shell.SetModelDirectory(ctx, selected)
}

func (a *modelDirectoryState) ModelDirectory() string {
	if path, err := filepath.EvalSymlinks(a.modelDir); err == nil {
		return path
	}
	return a.modelDir
}

// Keep persisted paths stable via a symlink, while relocating files across disks.
func (a *modelDirectoryState) SetModelDirectory(ctx context.Context, selected string) error {
	if !filepath.IsAbs(selected) {
		return transcription.ErrInvalid
	}
	target := filepath.Join(filepath.Clean(selected), "Tangentry Models")
	return a.modelDownloads.WithIdle(func() error {
		var previous, backup string
		committed := false
		switched := false
		err := a.transcription.WithIdleModels(func(c transcription.Config) (transcription.Config, error) {
			previous = a.ModelDirectory()
			if previous == target {
				committed = true
				return c, nil
			}
			remap := func(path string) string {
				if path == "" {
					return path
				}
				relative, err := filepath.Rel(previous, path)
				if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
					return filepath.Join(a.modelDir, relative)
				}
				return path
			}
			c.Model, c.QwenAligner = remap(c.Model), remap(c.QwenAligner)
			if relative, err := filepath.Rel(previous, target); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				return c, transcription.ErrInvalid
			}
			if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				return c, errors.New("model destination already exists")
			}
			staging, err := os.MkdirTemp(selected, ".tangentry-models-*")
			if err != nil {
				return c, err
			}
			defer os.RemoveAll(staging)
			if _, err := os.Stat(previous); err == nil {
				if err := copyModelTree(ctx, previous, staging); err != nil {
					return c, err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return c, err
			}
			bundled := os.Getenv("TANGENTRY_WHISPER_MODEL")
			if bundled != "" {
				base := filepath.Join(staging, "ggml-base.bin")
				if _, err := os.Stat(base); errors.Is(err, os.ErrNotExist) {
					if err := copyModelFile(ctx, bundled, base); err != nil {
						return c, err
					}
				}
				if c.Model == bundled {
					c.Model = filepath.Join(a.modelDir, "ggml-base.bin")
				}
			}
			if err := ctx.Err(); err != nil {
				return c, err
			}
			if err := os.Rename(staging, target); err != nil {
				return c, err
			}
			link := staging + "-link"
			if err := os.Symlink(target, link); err != nil {
				os.RemoveAll(target)
				return c, err
			}
			defer os.Remove(link)
			if _, err := os.Lstat(a.modelDir); err == nil {
				backup = staging + "-previous"
				// Backup belongs beside the stable path, including across volumes.
				backup = filepath.Join(filepath.Dir(a.modelDir), filepath.Base(backup))
				if err := os.Rename(a.modelDir, backup); err != nil {
					backup = ""
					os.RemoveAll(target)
					return c, err
				}
			}
			if err := os.Rename(link, a.modelDir); err != nil {
				// Cross-volume rename cannot move the staged symlink; recreate locally.
				localLink := filepath.Join(filepath.Dir(a.modelDir), filepath.Base(link))
				if err = os.Symlink(target, localLink); err == nil {
					err = os.Rename(localLink, a.modelDir)
				}
				_ = os.Remove(localLink)
				if err != nil {
					if backup != "" {
						_ = os.Rename(backup, a.modelDir)
						backup = ""
					}
					_ = os.RemoveAll(target)
					return c, err
				}
			}
			switched = true
			return c, nil
		})
		if err != nil {
			if switched {
				_ = os.Remove(a.modelDir)
				if backup != "" {
					_ = os.Rename(backup, a.modelDir)
				}
				_ = os.RemoveAll(target)
			}
			return err
		}
		if committed {
			return nil
		}
		if backup != "" {
			info, err := os.Lstat(backup)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				_ = os.Remove(backup)
				// Only remove files copied by this operation from the previous root.
				entries, _ := os.ReadDir(target)
				for _, entry := range entries {
					_ = os.RemoveAll(filepath.Join(previous, entry.Name()))
				}
				_ = os.Remove(previous)
			} else {
				_ = os.RemoveAll(backup)
			}
		}
		return nil
	})
}
