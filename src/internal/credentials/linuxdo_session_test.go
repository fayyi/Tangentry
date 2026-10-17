package credentials

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinuxdoRotationPreservesOtherCookiesAndCannotUndoLogout(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	s, err := Open(directory, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("linuxdo", "header", "_t=old; cf_clearance=keep"); err != nil {
		t.Fatal(err)
	}
	_, save := s.PrepareLinuxdoSession()
	cookies := []*http.Cookie{{Name: "_t", Value: "new", Secure: true, Path: "/"}, {Name: "_forum_session", Value: "forum", Secure: true, Domain: ".linux.do", Path: "/"}, {Name: "_t", Value: "evil", Secure: true, Domain: "evil.example"}, {Name: "cf_clearance", Value: "ignored", Secure: true}}
	if err := save(cookies); err != nil {
		t.Fatal(err)
	}
	header := s.Header("linuxdo", "https://linux.do/latest.json")
	if !strings.Contains(header, "_t=new") || !strings.Contains(header, "cf_clearance=keep") || !strings.Contains(header, "_forum_session=forum") || strings.Contains(header, "evil") {
		t.Fatal("session merge")
	}
	_, delayed := s.PrepareLinuxdoSession()
	if err := s.Clear("linuxdo"); err != nil {
		t.Fatal(err)
	}
	if err := delayed([]*http.Cookie{{Name: "_t", Value: "late", Secure: true}}); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	if s.Header("linuxdo", "https://linux.do/latest.json") != "" {
		t.Fatal("logout undone")
	}
}

func TestLinuxdoAuthorizationPersistsPrivatelyAndRejectsStaleLogin(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	s, err := Open(directory, "", "")
	if err != nil {
		t.Fatal(err)
	}
	save := s.PrepareLinuxdoAuthorization()
	if err := save(strings.Repeat("a", 32), strings.Repeat("b", 32), 0); err != nil {
		t.Fatal(err)
	}
	s, err = Open(directory, "", "")
	if err != nil {
		t.Fatal(err)
	}
	key, client := s.LinuxdoUserKey()
	if len(key) != 32 || len(client) != 32 {
		t.Fatal("authorization lost")
	}
	if s.Header("linuxdo", "https://linux.do/latest.json") != "" {
		t.Fatal("key exposed as cookie")
	}
	stale := s.PrepareLinuxdoAuthorization()
	if err := s.Save("linuxdo", "header", "_t=manually-imported"); err != nil {
		t.Fatal(err)
	}
	if err := stale(strings.Repeat("c", 32), strings.Repeat("d", 32), 0); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	if key, _ := s.LinuxdoUserKey(); key != "" {
		t.Fatal("manual import retained old authorization")
	}
}

func TestLinuxdoBrowserLoginCannotRestoreClearedCredentials(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	save := s.PrepareLinuxdoCookieLogin()
	if err := s.Clear("linuxdo"); err != nil {
		t.Fatal(err)
	}
	cookies := []*http.Cookie{{Name: "_t", Value: "synthetic", Secure: true}}
	if err := save(cookies); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	if err := s.PrepareLinuxdoCookieLogin()(cookies); err != nil {
		t.Fatal(err)
	}
	if s.Header("linuxdo", "https://linux.do/") != "_t=synthetic" {
		t.Fatal("new browser login not persisted")
	}
}

func TestLinuxdoBrowserLoginRejectsExpiredOrScopedCookies(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "credentials"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range []*http.Cookie{
		{Name: "_t", Value: "expired", Secure: true, Expires: time.Now().Add(-time.Hour)},
		{Name: "_t", Value: "scoped", Secure: true, Path: "/session"},
		{Name: "_t", Value: "foreign", Secure: true, Domain: "other.example"},
	} {
		if err := s.PrepareLinuxdoCookieLogin()([]*http.Cookie{cookie}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid login accepted: %v", err)
		}
	}
	if s.Header("linuxdo", "https://linux.do/") != "" {
		t.Fatal("invalid login persisted")
	}
}
