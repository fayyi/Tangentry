package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBilibiliQRSessionPrivatePersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareBilibiliLogin()("SESSDATA=synthetic-session; bili_jct=synthetic-csrf", "synthetic-refresh"); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "", "")
	if err != nil || s.entries["bilibili"].Refresh != "synthetic-refresh" {
		t.Fatal("refresh not persisted", err)
	}
	state, _ := json.Marshal(s.States())
	header := s.Header("bilibili", "https://api.bilibili.com/")
	path, cleanup, err := s.Snapshot("bilibili")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	jar, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{string(state), header, string(jar)} {
		if strings.Contains(raw, "synthetic-refresh") {
			t.Fatal("refresh leaked")
		}
	}
	info, err := os.Stat(filepath.Join(dir, "cookies.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe permissions", err)
	}
	if err := s.Save("bilibili", "header", "SESSDATA=manual-session"); err != nil {
		t.Fatal(err)
	}
	if s.entries["bilibili"].Refresh != "" {
		t.Fatal("manual import retained stale refresh")
	}
}

func TestBilibiliQRRejectsEveryNewerCredentialWrite(t *testing.T) {
	for _, change := range []string{"manual", "clear", "other-login", "identical"} {
		t.Run(change, func(t *testing.T) {
			s, err := Open(t.TempDir(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Save("bilibili", "header", "SESSDATA=original-session"); err != nil {
				t.Fatal(err)
			}
			late := s.PrepareBilibiliLogin()
			switch change {
			case "clear":
				err = s.Clear("bilibili")
			case "other-login":
				err = s.PrepareBilibiliLogin()("SESSDATA=new-session; bili_jct=new-csrf", "new-refresh")
			case "identical":
				err = s.Save("bilibili", "header", "SESSDATA=original-session")
			default:
				err = s.Save("bilibili", "header", "SESSDATA=manual-session")
			}
			if err != nil {
				t.Fatal(err)
			}
			before := s.entries["bilibili"]
			if err := late("SESSDATA=late-session; bili_jct=late-csrf", "late-refresh"); !errors.Is(err, ErrChanged) {
				t.Fatal("stale login accepted", err)
			}
			if s.entries["bilibili"] != before {
				t.Fatal("stale login changed credentials")
			}
		})
	}
}

func TestBilibiliQRInvalidRefreshPreservesCredentials(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "")
	if err := s.Save("bilibili", "header", "SESSDATA=original-session"); err != nil {
		t.Fatal(err)
	}
	before := s.entries["bilibili"]
	for _, refresh := range []string{"short", "invalid\r\nheader", "token=one; other=two"} {
		if err := s.PrepareBilibiliLogin()("SESSDATA=new-session", refresh); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if s.entries["bilibili"] != before {
			t.Fatal("invalid login replaced credentials")
		}
	}
}
