package downloads

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tangentry/internal/discovery"
)

// Start validates and admits one request while holding the service state lock.
// Repository and directory writes intentionally retain their existing ordering
// so duplicate and failure behavior stay unchanged.
func (s *Service) Start(ctx context.Context, request Request) (Job, error) {
	canonical, err := NormalizeURL(request.URL)
	if err != nil || len(request.Title) > 2000 {
		return Job{}, ErrInvalid
	}
	if request.MaxHeight < 0 || request.MaxHeight > 16384 || request.Options.normalize() != nil {
		return Job{}, ErrInvalid
	}
	if !s.runner.Ready() {
		return Job{}, ErrTools
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Job{}, ErrBusy
	}
	if s.failure != nil {
		return Job{}, s.failure
	}
	jobs, err := s.repo.ListDownloads(ctx)
	if err != nil {
		return Job{}, err
	}
	for _, job := range jobs {
		if sameURL(job.URL, canonical) && job.MaxHeight == request.MaxHeight && sameOptions(job.Options, request.Options) && (job.State == "queued" || job.State == "running") {
			return job, nil
		}
	}
	if len(s.queue) == cap(s.queue) {
		return Job{}, ErrBusy
	}
	if strings.TrimSpace(request.Title) == "" {
		_, id, _, _ := discovery.VideoURL(canonical)
		request.Title = id
		if request.Title == "" {
			request.Title = canonical
		}
	}
	now := time.Now().Unix()
	job := Job{Options: request.Options, ID: rand.Text(), URL: canonical, Title: strings.TrimSpace(request.Title), MaxHeight: request.MaxHeight, State: "queued", CreatedAt: now, UpdatedAt: now}
	s.locations.Jobs[job.ID] = s.directory
	if err := s.saveDirectories(); err != nil {
		delete(s.locations.Jobs, job.ID)
		return Job{}, err
	}
	if err := s.repo.SaveDownload(ctx, job); err != nil {
		return Job{}, err
	}
	s.queue <- job.ID
	return job, nil
}

// Cancel persists the user-requested terminal state before cancelling the
// runner context, so a concurrent worker cannot overwrite it as completed.
func (s *Service) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.repo.GetDownload(ctx, id)
	if err != nil {
		return err
	}
	if job.State != "queued" && job.State != "running" {
		return nil
	}
	job.State, job.Message, job.UpdatedAt = "cancelled", "已取消下载。", time.Now().Unix()
	if err := s.repo.SaveDownload(ctx, job); err != nil {
		return err
	}
	if cancel := s.cancel[id]; cancel != nil {
		cancel()
	}
	return nil
}

// Delete removes terminal history. Active jobs must be cancelled first so the
// worker cannot recreate or overwrite a record while it is being removed.
func (s *Service) Delete(ctx context.Context, id string, removeFiles bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.repo.GetDownload(ctx, id)
	if err != nil {
		return err
	}
	if job.State == "queued" || job.State == "running" {
		return ErrActive
	}
	directory := s.jobDirectoryUnlocked(id)
	if err := s.repo.DeleteDownload(ctx, id); err != nil {
		return err
	}
	delete(s.locations.Jobs, id)
	if err := s.saveDirectories(); err != nil {
		return err
	}
	if removeFiles {
		if err := os.RemoveAll(filepath.Join(directory, id)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Clear(ctx context.Context, removeFiles bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.repo.ListDownloads(ctx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(jobs))
	terminal := make([]Job, 0, len(jobs))
	for _, job := range jobs {
		if job.State == "queued" || job.State == "running" {
			continue
		}
		ids = append(ids, job.ID)
		terminal = append(terminal, job)
	}
	if err := s.repo.DeleteDownloads(ctx, ids); err != nil {
		return err
	}
	if removeFiles {
		for _, job := range terminal {
			directory := s.locations.Jobs[job.ID]
			if directory == "" {
				directory = s.originalDirectory
			}
			if err := os.RemoveAll(filepath.Join(directory, job.ID)); err != nil {
				return err
			}
		}
	}
	for _, id := range ids {
		delete(s.locations.Jobs, id)
	}
	return s.saveDirectories()
}
