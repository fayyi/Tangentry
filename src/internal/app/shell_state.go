package app

import (
	"context"

	"tangentry/internal/downloads"
)

// shellState is the only application-owned state exposed to native shells.
// It deliberately contains no HTTP handler or feature registry.
type shellState struct {
	downloads *downloads.Service
	models    *modelDirectoryState
}

func (s *shellState) DownloadDirectory() string { return s.downloads.Directory() }

func (s *shellState) DownloadDirectorySelected() bool { return s.downloads.DirectorySelected() }

func (s *shellState) SetDownloadDirectory(directory string) error {
	return s.downloads.SetDirectory(directory)
}

func (s *shellState) ModelDirectory() string { return s.models.ModelDirectory() }

func (s *shellState) SetModelDirectory(ctx context.Context, selected string) error {
	return s.models.SetModelDirectory(ctx, selected)
}
