package downloads

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// NewService restores durable job state before starting the worker. A queued
// or running job from a previous process is marked interrupted rather than
// silently resumed.
func NewService(repo Repository, runner Runner, directory string, timeout time.Duration) (*Service, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	jobs, err := repo.ListDownloads(context.Background())
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.State == "queued" || job.State == "running" {
			job.State, job.Message, job.UpdatedAt = "interrupted", "服务重启中断了任务，可以重新下载。", time.Now().Unix()
			if err := repo.SaveDownload(context.Background(), job); err != nil {
				return nil, err
			}
		}
	}
	ctx, stop := context.WithCancel(context.Background())
	directory, err = filepath.Abs(directory)
	if err != nil {
		stop()
		return nil, err
	}
	s := &Service{repo: repo, runner: runner, directory: directory, originalDirectory: directory, settingsPath: filepath.Join(directory, "download-directories.json"), timeout: timeout, ctx: ctx, stop: stop, queue: make(chan string, 10), cancel: make(map[string]context.CancelFunc), inspectSlots: make(chan struct{}, 2)}
	if err := s.loadDirectories(); err != nil {
		stop()
		return nil, err
	}
	s.wg.Add(1)
	go s.work()
	return s, nil
}

func (s *Service) Close() { s.stop(); s.wg.Wait() }

// Observe receives each job as it starts running and once it finishes, from
// the worker goroutine and outside the service lock. It must not block.
func (s *Service) Observe(observer func(Job)) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}

func (s *Service) notify(observer func(Job), job Job) {
	if observer != nil {
		observer(job)
	}
}

// List reports a storage failure before reading the repository again.
func (s *Service) List(ctx context.Context) ([]Job, error) {
	s.mu.Lock()
	err := s.failure
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.repo.ListDownloads(ctx)
}
