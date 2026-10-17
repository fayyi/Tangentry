// Package credentials keeps platform credentials outside the database and UI state.
package credentials

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrInvalid = errors.New("invalid platform cookies")
var ErrStorage = errors.New("cookie storage unavailable")
var ErrChanged = errors.New("credentials changed during login")
var platforms = []string{"bilibili", "youtube", "xiaoyuzhou", "douyin", "xiaohongshu", "linuxdo"}
var domains = map[string]string{"bilibili": "bilibili.com", "youtube": "youtube.com", "xiaoyuzhou": "xiaoyuzhoufm.com", "douyin": "douyin.com", "xiaohongshu": "xiaohongshu.com", "linuxdo": "linux.do"}

type State struct {
	Platform   string `json:"platform"`
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	UpdatedAt  int64  `json:"updatedAt"`
	ExpiresAt  int64  `json:"expiresAt,omitempty"`
	Username   string `json:"username,omitempty"`
}
type entry struct {
	APIKey    string `json:"apiKey,omitempty"`
	ClientID  string `json:"clientId,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	Jar       string `json:"jar"`
	// UserAgent is the browser UA that minted Jar. It is kept beside the
	// cookies so Cloudflare-bound sessions are never replayed with a different
	// client fingerprint. It is intentionally omitted from State/transport
	// responses; callers use UserAgent only when constructing platform requests.
	UserAgent string `json:"userAgent,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
	Token     string `json:"token,omitempty"`
	Refresh   string `json:"refresh,omitempty"`
	Account   string `json:"account,omitempty"`
}
type Store struct {
	mu                           sync.RWMutex
	directory                    string
	entries                      map[string]entry
	revisions                    map[string]uint64
	fallbackHeader, fallbackFile string
}

func Open(directory, header, file string) (*Store, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, ErrStorage
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, ErrStorage
	}
	s := &Store{directory: directory, entries: map[string]entry{}, revisions: map[string]uint64{}, fallbackHeader: header, fallbackFile: file}
	raw, err := os.ReadFile(filepath.Join(directory, "cookies.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil || len(raw) > 1<<20 {
		return nil, ErrStorage
	}
	if json.Unmarshal(raw, &s.entries) != nil || s.entries == nil {
		return nil, ErrStorage
	}
	for platform, e := range s.entries {
		if e.APIKey != "" && (platform != "linuxdo" || !validToken(e.APIKey) || !validToken(e.ClientID)) || e.ClientID != "" && e.APIKey == "" || e.ExpiresAt < 0 {
			return nil, ErrStorage
		}
		if _, ok := domains[platform]; !ok {
			return nil, ErrStorage
		}
		if e.Jar != "" {
			jar, err := normalize(platform, "netscape", e.Jar)
			if err != nil || jar != e.Jar {
				return nil, ErrStorage
			}
		}
		if e.UserAgent != "" && (e.Jar == "" || !validUserAgent(e.UserAgent)) {
			return nil, ErrStorage
		}
		if e.Token != "" && (platform != "xiaoyuzhou" || !validToken(e.Token)) {
			return nil, ErrStorage
		}
		if e.Refresh != "" && (!validToken(e.Refresh) || !(platform == "xiaoyuzhou" && e.Token != "" || platform == "bilibili" && e.Jar != "")) {
			return nil, ErrStorage
		}
		if e.Account != "" {
			key, err := hex.DecodeString(e.Account)
			if err != nil || len(key) != sha256.Size {
				return nil, ErrStorage
			}
		}
	}
	if os.Chmod(filepath.Join(directory, "cookies.json"), 0600) != nil {
		return nil, ErrStorage
	}
	return s, nil
}
func (s *Store) States() []State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]State, 0, len(platforms))
	for _, p := range platforms {
		v := State{Platform: p, Source: "anonymous"}
		if e, ok := s.entries[p]; ok {
			v.Configured = e.Jar != "" || e.Token != "" || e.APIKey != ""
			v.Source = "saved"
			if p == "xiaoyuzhou" && e.Token != "" {
				v.Source = "token"
			}
			if p == "linuxdo" && e.APIKey != "" {
				v.Source = "authorization"
			}
			v.UpdatedAt = e.UpdatedAt
			v.ExpiresAt = jarExpiry(e.Jar)
			if e.ExpiresAt > v.ExpiresAt {
				v.ExpiresAt = e.ExpiresAt
			}
		} else if p == "bilibili" && (s.fallbackHeader != "" || s.fallbackFile != "") {
			v.Configured = true
			v.Source = "environment"
		}
		out = append(out, v)
	}
	return out
}
func (s *Store) Save(platform, format, raw string) error {
	if platform == "xiaoyuzhou" && (format == "token" || format == "header") {
		value, err := podcastTokens(raw, format == "header")
		if err != nil {
			return err
		}
		return s.persistCredential(platform, value)
	}
	jar, err := normalize(platform, format, raw)
	if err != nil {
		return err
	}
	return s.persistCredential(platform, entry{Jar: jar, UpdatedAt: time.Now().Unix()})
}

// UserAgent returns the browser User-Agent stored with the platform cookies.
// The empty string means the credentials were manually imported or no browser
// login has been completed; callers should then use their platform default.
func (s *Store) UserAgent(platform string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.entries[platform]; ok {
		return e.UserAgent
	}
	return ""
}

// persistCredential keeps the local platform identity stable across cookie
// imports, QR logins, and token refreshes. This app has one owner; changing a
// credential is not treated as switching to another user.
func (s *Store) persistCredential(platform string, value entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistCredentialLocked(platform, value)
}

func (s *Store) persistCredentialLocked(platform string, value entry) error {
	previous := s.entries[platform]
	if previous.Account != "" {
		value.Account = previous.Account
	} else if value.Jar != "" || value.Token != "" || value.APIKey != "" {
		value.Account = localAccount(platform)
	}
	return s.persistEntryLocked(platform, value)
}

// Clear explicitly selects anonymous access, including when an environment fallback exists.
func (s *Store) Clear(platform string) error {
	if _, ok := domains[platform]; !ok {
		return ErrInvalid
	}
	return s.persist(platform, "")
}
func (s *Store) persist(platform, jar string) error {
	return s.persistEntry(platform, entry{Jar: jar, UpdatedAt: time.Now().Unix()})
}
func (s *Store) persistEntry(platform string, value entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistEntryLocked(platform, value)
}
func (s *Store) persistEntryLocked(platform string, value entry) error {
	previous := s.entries[platform]
	// Settings clients use UpdatedAt as a safe, non-secret change marker while
	// a WebView login is completing. Keep it strictly increasing even when two
	// writes happen within the same second.
	if value.UpdatedAt <= previous.UpdatedAt {
		value.UpdatedAt = previous.UpdatedAt + 1
	}
	next := make(map[string]entry, len(s.entries)+1)
	for k, v := range s.entries {
		next[k] = v
	}
	next[platform] = value
	raw, err := json.Marshal(next)
	if err != nil {
		return ErrStorage
	}
	f, err := os.CreateTemp(s.directory, ".cookies-*")
	if err != nil {
		return ErrStorage
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return ErrStorage
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return ErrStorage
	}
	if err = f.Close(); err != nil {
		return ErrStorage
	}
	if os.Rename(name, filepath.Join(s.directory, "cookies.json")) != nil {
		return ErrStorage
	}
	s.entries = next
	s.revisions[platform]++
	return nil
}

func validToken(raw string) bool {
	if len(raw) < 8 || len(raw) > 16384 {
		return false
	}
	for _, r := range raw {
		if r <= 32 || r >= 127 || r == ';' {
			return false
		}
	}
	return true
}

func validUserAgent(raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 || strings.TrimSpace(raw) == "" {
		return false
	}
	for _, r := range raw {
		// Keep controls/newlines and non-ASCII out so the value can never become
		// header injection or an ambiguous browser fingerprint.
		if r < 0x20 || r == 0x7f || r > 0x7e {
			return false
		}
	}
	return true
}

// Fingerprint identifies this local owner's platform recommendation scope.
func (s *Store) Fingerprint(platform, origin string) string {
	// Tangentry is a single-owner local app. Credential rotation and explicit
	// re-imports remain the same owner's history and recommendation scope.
	return localAccount(platform)
}

func localAccount(platform string) string {
	sum := sha256.Sum256([]byte("tangentry-local-owner\x00" + platform))
	return hex.EncodeToString(sum[:])
}
