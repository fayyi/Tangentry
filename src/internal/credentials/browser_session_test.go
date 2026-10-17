package credentials

import (
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
