package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCookiesPersistenceScopesAndSnapshots(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "legacy=test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("bilibili", "header", "SESSDATA=private-test; bili_jct=csrf-test"); err != nil {
		t.Fatal(err)
	}
	if got := s.Header("bilibili", "https://api.bilibili.com/x"); got != "SESSDATA=private-test; bili_jct=csrf-test" {
		t.Fatal(got)
	}
	for _, u := range []string{"https://youtube.com/", "https://bilibili.com.evil.example/", "http://api.bilibili.com/"} {
		if got := s.Header("bilibili", u); got != "" {
			t.Fatal("cookie scope", u)
		}
	}
	if err := s.Save("youtube", "netscape", ".youtube.com\tTRUE\t/\tTRUE\t0\tSID\tyoutube-test\n"); err != nil {
		t.Fatal(err)
	}
	a, clean, err := s.Snapshot("bilibili")
	if err != nil {
		t.Fatal(err)
	}
	defer clean()
	if info, _ := os.Stat(a); info.Mode().Perm() != 0600 {
		t.Fatal("permissions", info.Mode())
	}
	os.WriteFile(a, []byte("tool changed snapshot"), 0600)
	if !strings.Contains(s.Header("bilibili", "https://api.bilibili.com/"), "private-test") {
		t.Fatal("tool mutated saved jar")
	}
	re, err := Open(root, "legacy=test", "")
	if err != nil {
		t.Fatal(err)
	}
	if !re.States()[0].Configured || !re.States()[1].Configured {
		t.Fatal("lost state")
	}
	if err := re.Clear("bilibili"); err != nil {
		t.Fatal(err)
	}
	if re.Header("bilibili", "https://api.bilibili.com/") != "" {
		t.Fatal("environment credential resurrected")
	}
	if re.Header("youtube", "https://www.youtube.com/") != "SID=youtube-test" {
		t.Fatal("cross-platform clearing")
	}
	if info, _ := os.Stat(filepath.Join(root, "cookies.json")); info.Mode().Perm() != 0600 {
		t.Fatal("saved permissions")
	}
	clean()
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("snapshot not removed")
	}
}
func TestCookieValidationAndMatching(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "")
	invalid := []struct{ p, f, v string }{
		{"unknown", "header", "sid=value"}, {"youtube", "bad", "sid=value"}, {"youtube", "header", "sid=x\nInjected=value"},
		{"youtube", "netscape", ".bilibili.com\tTRUE\t/\tTRUE\t0\tSID\twrong"},
		{"youtube", "netscape", ".youtube.com.evil.test\tTRUE\t/\tTRUE\t0\tSID\twrong"},
		{"youtube", "netscape", ".youtube.com\tTRUE\t/\tTRUE\t-1\tSID\twrong"},
		{"youtube", "header", "bad name=value"}, {"youtube", "header", ""}, {"youtube", "header", strings.Repeat("x", 300<<10)},
	}
	for _, v := range invalid {
		if err := s.Save(v.p, v.f, v.v); err != ErrInvalid {
			t.Errorf("invalid accepted %s %s", v.p, v.f)
		}
	}
	jar := "#HttpOnly_www.youtube.com\tFALSE\t/watch\tTRUE\t0\tSID\tvalid\n.youtube.com\tTRUE\t/\tTRUE\t1\told\texpired\n"
	if err := s.Save("youtube", "netscape", jar); err != nil {
		t.Fatal(err)
	}
	if s.Header("youtube", "https://www.youtube.com/watch?v=123") != "SID=valid" {
		t.Fatal("matching")
	}
	for _, u := range []string{"https://m.youtube.com/watch", "https://www.youtube.com/watched", "http://www.youtube.com/watch"} {
		if s.Header("youtube", u) != "" {
			t.Fatal("path/domain/TLS leak", u)
		}
	}
}

func TestCookieHeaderRejectsNonHTTPAndCredentialBearingDestinations(t *testing.T) {
	s, err := Open(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A non-secure cookie must still be scoped to an HTTP request destination.
	if err := s.Save("youtube", "netscape", ".youtube.com\tTRUE\t/\tFALSE\t0\tSID\tprivate-fixture"); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{
		"ftp://www.youtube.com/", "file://www.youtube.com/", "//www.youtube.com/",
		"https://user:password" + "@www.youtube.com/", "http://user" + "@www.youtube.com/",
	} {
		if s.Header("youtube", destination) != "" {
			t.Fatal("unsafe destination received cookies")
		}
	}
	for _, destination := range []string{"http://www.youtube.com/", "https://www.youtube.com/"} {
		if s.Header("youtube", destination) != "SID=private-fixture" {
			t.Fatal("valid HTTP cookie scope changed")
		}
	}
}
