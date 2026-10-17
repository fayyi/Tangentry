package credentials

import (
	"bytes"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDouyinBrowserLoginPersistsSessionAndUA(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/136.0.0.0 Safari/537.36"
	cookies := []*http.Cookie{
		{Name: "sessionid", Value: "session-fixture", Domain: "passport.douyin.com", Path: "/", Secure: true, HttpOnly: true},
		{Name: "ttwid", Value: "ttwid-fixture", Domain: ".douyin.com", Path: "/", Secure: true},
		{Name: "msToken", Value: "token-fixture", Domain: ".douyin.com", Path: "/", Secure: true},
		{Name: "__ac_nonce", Value: "narrow-fixture", Domain: "passport.douyin.com", Path: "/", Secure: true},
		{Name: "unrelated", Value: "drop-me", Domain: ".douyin.com", Path: "/", Secure: true},
	}
	if err := s.PrepareDouyinBrowserLogin()(cookies, ua); err != nil {
		t.Fatal(err)
	}
	if got := s.UserAgent("douyin"); got != ua {
		t.Fatalf("UA mismatch: %q", got)
	}
	header := s.Header("douyin", "https://www.douyin.com/")
	if strings.Contains(header, "sessionid=session-fixture") || !strings.Contains(header, "ttwid=ttwid-fixture") || strings.Contains(header, "unrelated") {
		t.Fatalf("cookie header mismatch: %q", header)
	}
	passportHeader := s.Header("douyin", "https://passport.douyin.com/")
	if strings.Contains(header, "__ac_nonce") || !strings.Contains(passportHeader, "__ac_nonce=narrow-fixture") || !strings.Contains(passportHeader, "sessionid=session-fixture") {
		t.Fatalf("host cookie domain was broadened: %q / %q", header, passportHeader)
	}
	reopened, err := Open(dir, "", "")
	if err != nil || reopened.UserAgent("douyin") != ua {
		t.Fatalf("UA persistence failed: %v", err)
	}
}

func TestBrowserLoginPersistsSupportedPlatformCookiesAndFiltersDomains(t *testing.T) {
	tests := []struct {
		platform string
		cookies  []*http.Cookie
		want     string
		login    func(*Store) func([]*http.Cookie, string) error
	}{
		{"bilibili", []*http.Cookie{{Name: "SESSDATA", Value: "sess", Domain: ".bilibili.com", Path: "/", Secure: true}, {Name: "buvid3", Value: "buvid", Domain: ".bilibili.com", Path: "/", Secure: true}, {Name: "other", Value: "drop", Domain: ".bilibili.com", Path: "/", Secure: true}}, "SESSDATA=sess", func(s *Store) func([]*http.Cookie, string) error { return s.PrepareBilibiliBrowserLogin() }},
		{"xiaohongshu", []*http.Cookie{{Name: "web_session", Value: "session", Domain: ".xiaohongshu.com", Path: "/", Secure: true}, {Name: "a1", Value: "a-one", Domain: ".xiaohongshu.com", Path: "/", Secure: true}}, "web_session=session", func(s *Store) func([]*http.Cookie, string) error { return s.PrepareXiaohongshuBrowserLogin() }},
		{"youtube", []*http.Cookie{{Name: "SID", Value: "sid", Domain: ".google.com", Path: "/", Secure: true}, {Name: "SAPISID", Value: "api", Domain: ".youtube.com", Path: "/", Secure: true}}, "SAPISID=api", func(s *Store) func([]*http.Cookie, string) error { return s.PrepareYouTubeBrowserLogin() }},
	}
	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			s, err := Open(t.TempDir(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			ua := "Mozilla/5.0 (fixture)"
			if err := test.login(s)(test.cookies, ua); err != nil {
				t.Fatal(err)
			}
			if s.UserAgent(test.platform) != ua || !strings.Contains(s.Header(test.platform, "https://www."+map[string]string{"bilibili": "bilibili.com", "xiaohongshu": "xiaohongshu.com", "youtube": "youtube.com"}[test.platform]+"/"), test.want) {
				t.Fatalf("saved browser credential mismatch: ua=%q header=%q", s.UserAgent(test.platform), s.Header(test.platform, "https://www."+map[string]string{"bilibili": "bilibili.com", "xiaohongshu": "xiaohongshu.com", "youtube": "youtube.com"}[test.platform]+"/"))
			}
			if strings.Contains(s.Header(test.platform, "https://www."+map[string]string{"bilibili": "bilibili.com", "xiaohongshu": "xiaohongshu.com", "youtube": "youtube.com"}[test.platform]+"/"), "other=drop") {
				t.Fatal("unrelated browser cookie persisted")
			}
		})
	}
}

func TestYouTubeBrowserLoginRejectsSessionlessCookies(t *testing.T) {
	s, err := Open(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareYouTubeBrowserLogin()([]*http.Cookie{{Name: "PREF", Value: "tz=UTC", Domain: ".youtube.com", Path: "/", Secure: true}}, "Mozilla/5.0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sessionless browser login accepted: %v", err)
	}
}

func TestYouTubeGoogleCookiesStayScopedToYouTubeRequests(t *testing.T) {
	s, err := Open(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareYouTubeBrowserLogin()([]*http.Cookie{
		{Name: "__Secure-1PSID", Value: "private-session", Domain: ".google.com", Path: "/", Secure: true},
		{Name: "SAPISID", Value: "youtube-api", Domain: ".youtube.com", Path: "/", Secure: true},
	}, "Mozilla/5.0"); err != nil {
		t.Fatal(err)
	}
	if got := s.Header("youtube", "https://www.youtube.com/youtubei/v1/browse"); !strings.Contains(got, "SAPISID=youtube-api") || strings.Contains(got, "private-session") {
		t.Fatalf("Google cookie leaked into YouTube request header: %q", got)
	}
	if got := s.Header("youtube", "https://accounts.google.com/"); got != "" {
		t.Fatalf("Google-domain request received YouTube credentials: %q", got)
	}
}

func TestEncryptedStoreDoesNotPersistBrowserSecretsAsPlaintext(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "credentials")
	s, err := OpenWithKey(dir, "", "", key)
	if err != nil {
		t.Fatal(err)
	}
	ua := "Mozilla/5.0 (fixture)"
	if err := s.PrepareLinuxdoBrowserLogin()([]*http.Cookie{{Name: "_t", Value: "encrypted-cookie", Domain: ".linux.do", Path: "/", Secure: true}}, ua); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cookies.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "encrypted-cookie") || strings.Contains(string(raw), ua) {
		t.Fatalf("credential plaintext persisted: %s", raw)
	}
	reopened, err := OpenWithKey(dir, "", "", key)
	if err != nil || reopened.UserAgent("linuxdo") != ua || !strings.Contains(reopened.Header("linuxdo", "https://linux.do/"), "_t=encrypted-cookie") {
		t.Fatalf("encrypted credentials did not round-trip: %v", err)
	}
	headless, err := Open(dir, "", "")
	if err != nil || headless.States()[5].Configured {
		t.Fatalf("headless open should hide opaque desktop credentials: %v", err)
	}
}

func TestEncryptedStoreWithWrongLocalKeyDegradesToUnavailable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	key := bytes.Repeat([]byte{0x11}, 32)
	s, err := OpenWithKey(dir, "", "", key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("douyin", "netscape", "# Netscape HTTP Cookie File\n.douyin.com\tTRUE\t/\tTRUE\t0\tsessionid\tfixture-session\n"); err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Repeat([]byte{0x22}, 32)
	degraded, err := OpenWithKey(dir, "", "", wrong)
	if err != nil {
		t.Fatalf("wrong local key should not block startup: %v", err)
	}
	if degraded.States()[3].Configured {
		t.Fatal("credentials encrypted with an unavailable key remained configured")
	}
}

func TestEncryptedStoreMigratesKeychainPayloadToLocalFormat(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "credentials")
	key := bytes.Repeat([]byte{0x33}, 32)
	secret, err := encryptPrivate(key, entry{Jar: "# Netscape HTTP Cookie File\n.douyin.com\tTRUE\t/\tTRUE\t0\tsessionid\tmigrated-session\n", UserAgent: "Mozilla/5.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"douyin":{"secret":"` + secret + `","updatedAt":1}}`)
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenWithKey(dir, "", "", key)
	if err != nil {
		t.Fatal(err)
	}
	if !s.States()[3].Configured || !strings.Contains(s.Header("douyin", "https://www.douyin.com/"), "sessionid=migrated-session") {
		t.Fatal("migrated credentials are unavailable")
	}
	persisted, err := os.ReadFile(filepath.Join(dir, "cookies.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), `"secret"`) || strings.Contains(string(persisted), "migrated-session") {
		t.Fatal("keychain payload was not preserved as encrypted local data")
	}
}

func TestDouyinBrowserLoginRequiresSessionAndRejectsStaleCompletion(t *testing.T) {
	s, err := Open(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	ua := "Mozilla/5.0"
	if err := s.PrepareDouyinBrowserLogin()([]*http.Cookie{{Name: "ttwid", Value: "only", Domain: ".douyin.com", Path: "/", Secure: true}}, ua); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sessionless browser login accepted: %v", err)
	}
	late := s.PrepareDouyinBrowserLogin()
	if err := s.Clear("douyin"); err != nil {
		t.Fatal(err)
	}
	if err := late([]*http.Cookie{{Name: "sessionid", Value: "late", Domain: ".douyin.com", Path: "/", Secure: true}}, ua); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale completion accepted: %v", err)
	}
}
