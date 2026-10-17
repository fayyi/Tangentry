package favoritesync

import (
	"errors"
	"sort"
	"time"
)

func (s *Service) schedule() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *Service) tick(now time.Time) {
	if s.checkRestriction(s.ctx, now) != nil {
		return
	}
	sources, err := s.repo.ListFavoriteSources(s.ctx)
	if err != nil {
		return
	}
	for _, source := range sources {
		if source.RestrictedUntil > now.Unix() {
			return
		}
	}
	// Give overdue folders a turn even when repository order is fixed.
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].LastAttemptAt != sources[j].LastAttemptAt {
			return sources[i].LastAttemptAt < sources[j].LastAttemptAt
		}
		return sources[i].FolderID < sources[j].FolderID
	})
	for _, source := range sources {
		if source.Policy.Enabled && (source.LastAttemptAt == 0 || now.Unix()-source.LastAttemptAt >= int64(source.Policy.IntervalMinutes)*60) {
			_, err := s.Start(s.ctx, source.FolderID)
			if errors.Is(err, ErrBusy) {
				return
			}
			// Attempt one folder per tick, so sources never compete for platform requests.
			if err == nil {
				return
			}
		}
	}
}
