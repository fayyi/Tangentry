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
	"bilibili": {
		"SESSDATA": true, "bili_jct": true, "DedeUserID": true, "DedeUserID__ckMd5": true,
		"buvid3": true, "buvid4": true, "buvid_fp": true, "b_nut": true, "sid": true,
	},
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
	"xiaohongshu": {
		"web_session": true, "a1": true, "webId": true, "gid": true, "abRequestId": true,
		"xsecappid": true, "webBuild": true, "customerClientId": true,
	},
	"youtube": {
		"SID": true, "HSID": true, "SSID": true, "APISID": true, "SAPISID": true,
		"__Secure-1PAPISID": true, "__Secure-3PAPISID": true, "LOGIN_INFO": true,
		"__Secure-1PSID": true, "__Secure-3PSID": true, "__Secure-1PSIDTS": true, "__Secure-3PSIDTS": true,
		"__Secure-1PSIDCC": true, "__Secure-3PSIDCC": true, "__Secure-1PSIDRTS": true, "__Secure-3PSIDRTS": true,
		"PREF": true, "VISITOR_INFO1_LIVE": true, "YSC": true, "GPS": true, "__Secure-YEC": true,
	},
}

// PrepareBrowserLogin selects the platform-specific browser cookie policy.
// The returned saver is revision-bound and never exposes the persisted jar.
func (s *Store) PrepareBrowserLogin(platform string) func([]*http.Cookie, string) error {
	switch platform {
	case "bilibili":
		return s.PrepareBilibiliBrowserLogin()
	case "linuxdo":
		return s.PrepareLinuxdoBrowserLogin()
	case "douyin":
		return s.PrepareDouyinBrowserLogin()
	case "xiaohongshu":
		return s.PrepareXiaohongshuBrowserLogin()
	case "youtube":
		return s.PrepareYouTubeBrowserLogin()
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
		if cookie == nil || !allowed[cookie.Name] || cookie.Value == "" || cookie.Domain == "" || cookie.Valid() != nil || !cookie.Secure || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			continue
		}
		if cookie.Domain != "" && !browserCookieDomainAllowed(platform, root, cookie.Domain) || cookie.Path != "" && !strings.HasPrefix(cookie.Path, "/") {
			continue
		}
		domainKey := strings.ToLower(strings.TrimSpace(cookie.Domain))
		key := domainKey + "\x00" + cookie.Path + "\x00" + cookie.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		expires := int64(0)
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		}
		domain := root
		subdomains := "FALSE"
		if cookie.Domain != "" {
			// Preserve the cookie's actual domain. Broadening a host cookie to
			// the platform root could send a credential to passport or CDN
			// subdomains that never received it in the WebView.
			rawDomain := strings.ToLower(strings.TrimSpace(cookie.Domain))
			if strings.HasPrefix(rawDomain, ".") {
				domain = "." + strings.TrimPrefix(rawDomain, ".")
				subdomains = "TRUE"
			} else {
				domain = rawDomain
			}
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

func browserCookieDomainAllowed(platform, root, domain string) bool {
	if browserDomainAllowed(root, domain) {
		return true
	}
	// Google account cookies are needed to establish a YouTube session, but
	// remain domain-scoped and are never sent to YouTube by Header.
	return platform == "youtube" && browserDomainAllowed("google.com", domain)
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

func (s *Store) PrepareBilibiliBrowserLogin() func([]*http.Cookie, string) error {
	s.mu.RLock()
	revision := s.revisions["bilibili"]
	s.mu.RUnlock()
	return func(cookies []*http.Cookie, userAgent string) error {
		jar, err := browserJar("bilibili", cookies, userAgent)
		if err != nil || !browserJarHas(jar, "SESSDATA") {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["bilibili"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("bilibili", entry{Jar: jar, UserAgent: userAgent, UpdatedAt: time.Now().Unix()})
	}
}

func (s *Store) PrepareXiaohongshuBrowserLogin() func([]*http.Cookie, string) error {
	s.mu.RLock()
	revision := s.revisions["xiaohongshu"]
	s.mu.RUnlock()
	return func(cookies []*http.Cookie, userAgent string) error {
		jar, err := browserJar("xiaohongshu", cookies, userAgent)
		if err != nil || !browserJarHas(jar, "web_session") && !browserJarHas(jar, "a1") {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["xiaohongshu"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("xiaohongshu", entry{Jar: jar, UserAgent: userAgent, UpdatedAt: time.Now().Unix()})
	}
}

func (s *Store) PrepareYouTubeBrowserLogin() func([]*http.Cookie, string) error {
	s.mu.RLock()
	revision := s.revisions["youtube"]
	s.mu.RUnlock()
	return func(cookies []*http.Cookie, userAgent string) error {
		jar, err := browserJar("youtube", cookies, userAgent)
		if err != nil || !browserJarHas(jar, "SID") && !browserJarHas(jar, "__Secure-1PSID") && !browserJarHas(jar, "__Secure-3PSID") && !browserJarHas(jar, "LOGIN_INFO") && !browserJarHas(jar, "SAPISID") && !browserJarHas(jar, "__Secure-1PAPISID") && !browserJarHas(jar, "__Secure-3PAPISID") {
			return ErrInvalid
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revisions["youtube"] != revision {
			return ErrChanged
		}
		return s.persistCredentialLocked("youtube", entry{Jar: jar, UserAgent: userAgent, UpdatedAt: time.Now().Unix()})
	}
}
