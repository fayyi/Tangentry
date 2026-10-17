package credentials

import "time"

// PrepareBilibiliLogin binds a QR result to the credentials present at creation.
func (s *Store) PrepareBilibiliLogin() func(string, string) error {
	s.mu.RLock()
	revision := s.revisions["bilibili"]
	s.mu.RUnlock()
	return func(header, refresh string) error {
		jar, err := normalize("bilibili", "header", header)
		if err != nil || refresh != "" && !validToken(refresh) {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["bilibili"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("bilibili", entry{Jar: jar, Refresh: refresh, UpdatedAt: time.Now().Unix()})
	}
}

// BilibiliPair returns the private Cookie header and refresh token used only
// by the internal renewal client. Neither value is exposed by settings APIs.
func (s *Store) BilibiliPair() (header, refresh string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.entries["bilibili"]
	return headerFromJar(e.Jar), e.Refresh
}

// SaveBilibiliIfCurrent atomically installs a rotated Cookie and refresh token.
func (s *Store) SaveBilibiliIfCurrent(oldHeader, oldRefresh, newHeader, newRefresh string) error {
	oldJar, err := normalize("bilibili", "header", oldHeader)
	if err != nil || !validToken(oldRefresh) {
		return ErrInvalid
	}
	newJar, err := normalize("bilibili", "header", newHeader)
	if err != nil || !validToken(newRefresh) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries["bilibili"]
	if e.Jar != oldJar || e.Refresh != oldRefresh {
		return ErrChanged
	}
	return s.persistCredentialLocked("bilibili", entry{Jar: newJar, Refresh: newRefresh, UpdatedAt: time.Now().Unix()})
}
