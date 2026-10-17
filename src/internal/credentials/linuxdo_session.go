package credentials

import (
	"net/http"
	"strings"
	"time"
)

// PrepareLinuxdoSession binds server cookie rotation to the login present at
// request start. A delayed response cannot undo a logout or a newer import.
func (s *Store) PrepareLinuxdoSession() (string, func([]*http.Cookie) error) {
	s.mu.RLock()
	previous, revision := s.entries["linuxdo"], s.revisions["linuxdo"]
	s.mu.RUnlock()
	header := headerFromJar(previous.Jar)
	return header, func(cookies []*http.Cookie) error {
		if header == "" {
			return nil
		}
		values := map[string]string{}
		order := []string{}
		for _, pair := range strings.Split(header, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if ok {
				values[name] = value
				order = append(order, name)
			}
		}
		changed := false
		for _, cookie := range cookies {
			if cookie == nil || !linuxdoCookieName(cookie.Name) || cookie.Value == "" || cookie.Valid() != nil || !cookie.Secure || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && cookie.Expires.Before(time.Now()) {
				continue
			}
			// Cloudflare clearance is minted by the browser challenge flow. Do
			// not let an ordinary API response overwrite a still-valid token.
			if cookie.Name == "cf_clearance" || cookie.Name == "__cf_bm" {
				continue
			}
			if cookie.Domain != "" && strings.TrimPrefix(cookie.Domain, ".") != "linux.do" || cookie.Path != "" && cookie.Path != "/" {
				continue
			}
			if values[cookie.Name] == cookie.Value {
				continue
			}
			if _, exists := values[cookie.Name]; !exists {
				order = append(order, cookie.Name)
			}
			values[cookie.Name] = cookie.Value
			changed = true
		}
		if !changed {
			return nil
		}
		pairs := make([]string, 0, len(order))
		for _, name := range order {
			pairs = append(pairs, name+"="+values[name])
		}
		jar, err := normalize("linuxdo", "header", strings.Join(pairs, "; "))
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["linuxdo"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("linuxdo", entry{Jar: jar, UpdatedAt: time.Now().Unix()})
	}
}

func (s *Store) LinuxdoUserKey() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.entries["linuxdo"]
	if e.ExpiresAt > 0 && time.Now().Unix() >= e.ExpiresAt {
		return "", ""
	}
	return e.APIKey, e.ClientID
}

func (s *Store) PrepareLinuxdoAuthorization() func(string, string, int64) error {
	s.mu.RLock()
	revision := s.revisions["linuxdo"]
	s.mu.RUnlock()
	return func(key, client string, expires int64) error {
		if !validToken(key) || !validToken(client) || expires < 0 {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["linuxdo"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("linuxdo", entry{APIKey: key, ClientID: client, ExpiresAt: expires, UpdatedAt: time.Now().Unix()})
	}
}

func (s *Store) PrepareLinuxdoCookieLogin() func([]*http.Cookie) error {
	s.mu.RLock()
	revision := s.revisions["linuxdo"]
	s.mu.RUnlock()
	return func(cookies []*http.Cookie) error {
		pairs := []string{}
		hasLogin := false
		for _, cookie := range cookies {
			if cookie == nil || !linuxdoCookieName(cookie.Name) || cookie.Value == "" || cookie.Valid() != nil || !cookie.Secure || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(time.Now()) {
				continue
			}
			if cookie.Domain != "" && strings.TrimPrefix(cookie.Domain, ".") != "linux.do" || cookie.Path != "" && cookie.Path != "/" {
				continue
			}
			pairs = append(pairs, cookie.Name+"="+cookie.Value)
			hasLogin = hasLogin || cookie.Name == "_t"
		}
		if !hasLogin {
			return ErrInvalid
		}
		jar, err := normalize("linuxdo", "header", strings.Join(pairs, "; "))
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["linuxdo"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("linuxdo", entry{Jar: jar, UpdatedAt: time.Now().Unix()})
	}
}

func linuxdoCookieName(name string) bool {
	switch name {
	case "_t", "_forum_session", "cf_clearance", "__cf_bm":
		return true
	default:
		return false
	}
}
