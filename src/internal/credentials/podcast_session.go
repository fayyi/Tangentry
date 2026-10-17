package credentials

import (
	"strings"
	"time"
)

// SaveTokens stores the two private Xiaoyuzhou app credentials atomically.
func (s *Store) SaveTokens(access, refresh string) error {
	if !validToken(access) || !validToken(refresh) {
		return ErrInvalid
	}
	return s.persistCredential("xiaoyuzhou", entry{Token: access, Refresh: refresh, UpdatedAt: time.Now().Unix()})
}

// PrepareTokenLogin prevents a pending QR login from replacing newer credentials.
func (s *Store) PrepareTokenLogin() func(string, string) error {
	s.mu.RLock()
	previous := s.entries["xiaoyuzhou"]
	s.mu.RUnlock()
	return func(access, refresh string) error {
		if !validToken(access) || !validToken(refresh) {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.entries["xiaoyuzhou"] != previous {
			return ErrChanged
		}
		return s.persistCredentialLocked("xiaoyuzhou", entry{Token: access, Refresh: refresh, UpdatedAt: time.Now().Unix()})
	}
}

func podcastTokens(raw string, requireHeader bool) (entry, error) {
	raw = strings.TrimSpace(raw)
	value := entry{UpdatedAt: time.Now().Unix()}
	if len(raw) > 256<<10 || strings.ContainsAny(raw, "\r\n\t") {
		return value, ErrInvalid
	}
	if !requireHeader && !strings.Contains(raw, ";") && !strings.HasPrefix(raw, "Cookie:") && !strings.HasPrefix(raw, "x-jike-") {
		if !validToken(raw) {
			return value, ErrInvalid
		}
		value.Token = raw
		return value, nil
	}
	seen := map[string]bool{}
	for _, pair := range strings.Split(strings.TrimSpace(strings.TrimPrefix(raw, "Cookie:")), ";") {
		name, token, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok || !validName(name) || !validValue(token) {
			return value, ErrInvalid
		}
		if name != "x-jike-access-token" && name != "x-jike-refresh-token" {
			continue
		}
		if seen[name] || !validToken(token) {
			return value, ErrInvalid
		}
		seen[name] = true
		if name == "x-jike-access-token" {
			value.Token = token
		} else {
			value.Refresh = token
		}
	}
	if value.Token == "" {
		return value, ErrInvalid
	}
	return value, nil
}

// AccessToken is never serialized through the settings API or sent as a Cookie.
func (s *Store) AccessToken(platform string) string {
	if platform != "xiaoyuzhou" {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[platform].Token
}

// TokenPair returns private app credentials for internal authenticated clients.
// It is intentionally not exposed through the settings HTTP API.
func (s *Store) TokenPair(platform string) (access, refresh string) {
	if platform != "xiaoyuzhou" {
		return "", ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.entries[platform]
	return e.Token, e.Refresh
}

// SaveTokensIfCurrent updates credentials only when they still match the
// snapshot used for refresh, preventing an older refresh response from
// replacing credentials entered by the user in the meantime.
func (s *Store) SaveTokensIfCurrent(oldAccess, oldRefresh, access, refresh string) error {
	if !validToken(access) || !validToken(refresh) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries["xiaoyuzhou"]
	if e.Token != oldAccess || e.Refresh != oldRefresh {
		return ErrChanged
	}
	return s.persistCredentialLocked("xiaoyuzhou", entry{Token: access, Refresh: refresh, Account: localAccount("xiaoyuzhou"), UpdatedAt: time.Now().Unix()})
}
