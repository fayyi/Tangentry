package playback

import (
	"context"
	"time"
)

// A successful backup remains first for later seeks. Keep its full signed URL
// private and retain the other addresses as fallbacks.
func (s *Service) rememberSource(token string, revision uint64, raw string, size int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[token]
	if !ok || value.revision != revision {
		return false
	}
	if size > 0 && value.mediaSize > 0 && size != value.mediaSize {
		return false
	}
	if size > 0 {
		value.mediaSize = size
	}
	if value.source.URL == raw {
		s.sessions[token] = value
		return true
	}
	all := append([]string{value.source.URL}, value.source.Backups...)
	found := false
	backups := make([]string, 0, 3)
	for _, candidate := range all {
		if candidate == raw {
			found = true
		} else {
			backups = append(backups, candidate)
		}
	}
	if !found {
		return false
	}
	value.source.URL, value.source.Backups = raw, backups
	value.sourceExpires = sourceDeadline(raw)
	s.sessions[token] = value
	return true
}

// Serialize refreshes per session, recheck revision after waiting, and never
// resurrect a released session. A failed refresh cools down for 30 seconds.
func (s *Service) refreshSession(ctx context.Context, token string, observed entry) (entry, error) {
	select {
	case observed.refresh <- struct{}{}:
		defer func() { <-observed.refresh }()
	case <-ctx.Done():
		return entry{}, ctx.Err()
	}
	s.mu.Lock()
	current, ok := s.sessions[token]
	s.mu.Unlock()
	if !ok || !current.expires.After(time.Now()) {
		return entry{}, ErrExpired
	}
	if current.revision != observed.revision {
		return current, nil
	}
	if time.Now().Before(current.refreshAfter) {
		return entry{}, ErrUnavailable
	}
	select {
	case s.opening <- struct{}{}:
		defer func() { <-s.opening }()
	default:
		return entry{}, ErrBusy
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	source, err := s.resolve(resolveCtx, current.videoID)
	if err == nil {
		source, err = normalizeSource(source)
	}
	if err == nil && (source.Quality != current.source.Quality || source.Duration != current.source.Duration) {
		err = ErrUnavailable
	}
	deadline := sourceDeadline(source.URL)
	if err == nil && !deadline.IsZero() && !deadline.After(time.Now().Add(5*time.Second)) {
		err = ErrUnavailable
	}
	if err == nil {
		err = resolveCtx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[token]
	if !ok || !value.expires.After(time.Now()) {
		return entry{}, ErrExpired
	}
	if err != nil {
		value.refreshAfter = time.Now().Add(30 * time.Second)
		s.sessions[token] = value
		return entry{}, err
	}
	value.source, value.sourceExpires, value.revision = source, deadline, value.revision+1
	value.refreshAfter = time.Time{}
	s.sessions[token] = value
	return value, nil
}
