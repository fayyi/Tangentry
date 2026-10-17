// Package favoritesync synchronizes explicitly selected Bilibili folders.
// Platform requests and persistent downloads remain separate adapters.
package favoritesync

import (
	"context"
	"sync"
	"time"
)

type Service struct {
	repo            Repository
	remote          Remote
	downloader      Downloader
	ctx             context.Context
	stop            context.CancelFunc
	mu              sync.Mutex
	activeID        string
	cancel          context.CancelFunc
	closed          bool
	wg              sync.WaitGroup
	pageDelay       time.Duration
	restrictedUntil int64
}

func New(repo Repository, remote Remote, downloader Downloader) *Service {
	ctx, stop := context.WithCancel(context.Background())
	s := &Service{repo: repo, remote: remote, downloader: downloader, ctx: ctx, stop: stop, pageDelay: 800 * time.Millisecond}
	s.wg.Add(1)
	go s.schedule()
	return s
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.stop()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) List(ctx context.Context) ([]Source, error) {
	sources, err := s.repo.ListFavoriteSources(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range sources {
		sources[i].Active = s.activeID == sources[i].FolderID
	}
	return sources, err
}

// Remote listing, mutations and sync runs share a gate. Local reads remain available.
func (s *Service) begin(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.activeID != "" {
		return nil, ErrBusy
	}
	s.activeID = id
	s.wg.Add(1)
	return func() {
		s.mu.Lock()
		s.activeID = ""
		s.cancel = nil
		s.mu.Unlock()
		s.wg.Done()
	}, nil
}

func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeID != id || s.cancel == nil {
		return ErrNotFound
	}
	s.cancel()
	return nil
}
