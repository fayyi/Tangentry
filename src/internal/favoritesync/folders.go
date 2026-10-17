package favoritesync

import (
	"context"
	"errors"
	"time"

	"tangentry/internal/discovery"
)

func (s *Service) Folders(ctx context.Context) ([]Folder, error) {
	release, err := s.begin("folder-list")
	if err != nil {
		return nil, err
	}
	defer release()
	run, cancel := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	if err := s.checkRestriction(ctx, time.Now()); err != nil {
		return nil, err
	}
	folders, err := s.remote.Folders(run)
	var platform *discovery.PlatformError
	if errors.As(err, &platform) && platform.Restricted && run.Err() == nil {
		s.restrictAccount()
	}
	return folders, err
}

func (s *Service) restrictAccount() int64 {
	until := time.Now().Add(15 * time.Minute).Unix()
	s.mu.Lock()
	s.restrictedUntil = max(s.restrictedUntil, until)
	s.mu.Unlock()
	_ = s.repo.ExtendFavoriteRestriction(context.Background(), until)
	return until
}

// A restricted account affects every folder, independently of source removal.
func (s *Service) checkRestriction(ctx context.Context, now time.Time) error {
	until, err := s.repo.FavoriteRestriction(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	until = max(until, s.restrictedUntil)
	s.mu.Unlock()
	if until > now.Unix() {
		return &discovery.PlatformError{Restricted: true, Message: "B站收藏同步暂时受限，已暂停新请求，请稍后重试。已有收藏和下载记录仍保留。"}
	}
	return nil
}
