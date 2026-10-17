package credentials

import (
	"errors"
	"net/http"
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
		{Name: "sessionid", Value: "session-fixture", Domain: ".douyin.com", Path: "/", Secure: true, HttpOnly: true},
		{Name: "ttwid", Value: "ttwid-fixture", Domain: ".douyin.com", Path: "/", Secure: true},
		{Name: "msToken", Value: "token-fixture", Domain: ".douyin.com", Path: "/", Secure: true},
		{Name: "unrelated", Value: "drop-me", Domain: ".douyin.com", Path: "/", Secure: true},
	}
	if err := s.PrepareDouyinBrowserLogin()(cookies, ua); err != nil {
		t.Fatal(err)
	}
	if got := s.UserAgent("douyin"); got != ua {
		t.Fatalf("UA mismatch: %q", got)
	}
	header := s.Header("douyin", "https://www.douyin.com/")
	if !strings.Contains(header, "sessionid=session-fixture") || !strings.Contains(header, "ttwid=ttwid-fixture") || strings.Contains(header, "unrelated") {
		t.Fatalf("cookie header mismatch: %q", header)
	}
	reopened, err := Open(dir, "", "")
	if err != nil || reopened.UserAgent("douyin") != ua {
		t.Fatalf("UA persistence failed: %v", err)
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
