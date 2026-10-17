package credentials

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Browser cookies are reduced to platform-specific names before being
// normalized. In particular, tracking and unrelated account cookies are not
// copied from the WebView's full cookie jar into the credential store.
var browserCookieNames = map[string]map[string]bool{
	"linuxdo": {
		"_t": true, "_forum_session": true, "cf_clearance": true, "__cf_bm": true,
	},
	"douyin": {
		"sessionid": true, "sessionid_ss": true, "sid_tt": true, "sid_guard": true,
		"ttwid": true, "msToken": true, "s_v_web_id": true, "passport_csrf_token": true,
		"passport_csrf_token_default": true, "odin_tt": true, "tt_scid": true,
		"csrf_session_id": true, "uid_tt": true, "uid_tt_ss": true, "__ac_nonce": true,
		"__ac_signature": true,
	},
}

// PrepareBrowserLogin selects the platform-specific browser cookie policy.
// The returned saver is revision-bound and never exposes the persisted jar.
func (s *Store) PrepareBrowserLogin(platform string) func([]*http.Cookie, string) error {
	switch platform {
	case "linuxdo":
		return s.PrepareLinuxdoBrowserLogin()
	case "douyin":
		return s.PrepareDouyinBrowserLogin()
	default:
		return func([]*http.Cookie, string) error { return ErrInvalid }
	}
}

func browserJar(platform string, cookies []*http.Cookie, userAgent string) (string, error) {
	if !validUserAgent(userAgent) {
		return "", ErrInvalid
	}
	allowed, ok := browserCookieNames[platform]
	if !ok {
		return "", ErrInvalid
	}
	root := domains[platform]
	now := time.Now()
	seen := map[string]bool{}
	lines := []string{}
	for _, cookie := range cookies {
		if cookie == nil || !allowed[cookie.Name] || cookie.Value == "" || cookie.Valid() != nil || !cookie.Secure || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			continue
		}
		if cookie.Domain != "" && !browserDomainAllowed(root, cookie.Domain) || cookie.Path != "" && !strings.HasPrefix(cookie.Path, "/") {
			continue
		}
		if seen[cookie.Name] {
			continue
		}
		seen[cookie.Name] = true
		expires := int64(0)
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		}
		domain := "." + root
		subdomains := "TRUE"
		if cookie.Domain == "" {
			domain = root
			subdomains = "FALSE"
		}
		secure := "FALSE"
		if cookie.Secure {
			secure = "TRUE"
		}
		path := cookie.Path
		if path == "" {
			path = "/"
		}
		lines = append(lines, strings.Join([]string{domain, subdomains, path, secure, strconv.FormatInt(expires, 10), cookie.Name, cookie.Value}, "\t"))
	}
	if len(lines) == 0 {
		return "", ErrInvalid
	}
	jar, err := normalize(platform, "netscape", "# Netscape HTTP Cookie File\n"+strings.Join(lines, "\n")+"\n")
	if err != nil {
		return "", err
	}
	return jar, nil
}

func browserDomainAllowed(root, domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return domain == root || strings.HasSuffix(domain, "."+root)
}

func browserJarHas(jar, name string) bool {
	for _, line := range strings.Split(jar, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) == 7 && fields[5] == name {
			return true
		}
	}
	return false
}

// PrepareDouyinBrowserLogin stores only the supported Douyin session and
// challenge cookies alongside the UA that generated them. A stale WebView
// completion cannot overwrite a later import or logout.
func (s *Store) PrepareDouyinBrowserLogin() func([]*http.Cookie, string) error {
	s.mu.RLock()
	revision := s.revisions["douyin"]
	s.mu.RUnlock()
	return func(cookies []*http.Cookie, userAgent string) error {
		jar, err := browserJar("douyin", cookies, userAgent)
		if err != nil {
			return err
		}
		if !browserJarHas(jar, "sessionid") && !browserJarHas(jar, "sessionid_ss") {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["douyin"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("douyin", entry{Jar: jar, UserAgent: userAgent, UpdatedAt: time.Now().Unix()})
	}
}
