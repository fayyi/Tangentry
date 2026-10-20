package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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

func TestNetscapeDomainFlagCombinationsNormalizeAndMatch(t *testing.T) {
	tests := []struct {
		name           string
		domain         string
		includeDomains string
		wantDomain     string
		wantFlag       string
		wantSubdomain  bool
	}{
		{"dot and true", ".douyin.com", "TRUE", ".douyin.com", "TRUE", true},
		{"no dot and false", "douyin.com", "FALSE", "douyin.com", "FALSE", false},
		{"no dot and true", "douyin.com", "TRUE", ".douyin.com", "TRUE", true},
		{"dot and false", ".douyin.com", "FALSE", "douyin.com", "FALSE", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "# Netscape HTTP Cookie File\n" + test.domain + "\t" + test.includeDomains + "\t/\tTRUE\t0\tsessionid\tfixture-value\n"
			jar, err := normalize("douyin", "netscape", raw)
			if err != nil {
				t.Fatal(err)
			}
			assertMozillaJar(t, jar)
			line := strings.Split(strings.TrimSpace(strings.SplitN(jar, "\n", 2)[1]), "\t")
			if line[0] != test.wantDomain || line[1] != test.wantFlag {
				t.Fatalf("normalized domain/flag = %q/%q, want %q/%q", line[0], line[1], test.wantDomain, test.wantFlag)
			}

			s, err := Open(t.TempDir(), "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Save("douyin", "netscape", raw); err != nil {
				t.Fatal(err)
			}
			if got := s.Header("douyin", "https://douyin.com/"); got != "sessionid=fixture-value" {
				t.Fatalf("root host matching = %q, want cookie", got)
			}
			if got := s.Header("douyin", "https://www.douyin.com/"); (got != "") != test.wantSubdomain {
				t.Fatalf("subdomain matching = %q, want match=%v", got, test.wantSubdomain)
			}
		})
	}
}

func TestHeaderIgnoresEmptySegmentsButRequiresCookie(t *testing.T) {
	for _, raw := range []string{"a=b;", ";a=b;;", " a=b ; ;"} {
		jar, err := normalize("youtube", "header", raw)
		if err != nil {
			t.Errorf("normalize(%q): %v", raw, err)
			continue
		}
		assertMozillaJar(t, jar)
		fields := strings.Split(strings.SplitN(jar, "\n", 2)[1], "\t")
		if fields[0] != ".youtube.com" || fields[1] != "TRUE" {
			t.Errorf("header cookie scope = %q/%q, want .youtube.com/TRUE", fields[0], fields[1])
		}
	}
	for _, raw := range []string{"", ";", "; ;"} {
		if _, err := normalize("youtube", "header", raw); err != ErrInvalid {
			t.Errorf("normalize(%q) error = %v, want ErrInvalid", raw, err)
		}
	}
	for _, raw := range []string{"a=b; invalid pair", "a=b;bad name=c"} {
		if _, err := normalize("youtube", "header", raw); err != ErrInvalid {
			t.Errorf("malformed cookie %q error = %v, want ErrInvalid", raw, err)
		}
	}
}

func TestOpenRepairsAndPersistsLegacyJarBeforeSnapshot(t *testing.T) {
	dir := t.TempDir()
	legacy := entry{Jar: "# Netscape HTTP Cookie File\n" +
		"douyin.com\tTRUE\t/\tTRUE\t0\tsessionid\tfixture-domain\n" +
		".passport.douyin.com\tFALSE\t/\tTRUE\t0\tsid_guard\tfixture-host\n"}
	raw, err := json.Marshal(map[string]entry{"douyin": legacy})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cookies.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Netscape HTTP Cookie File\n" +
		".douyin.com\tTRUE\t/\tTRUE\t0\tsessionid\tfixture-domain\n" +
		"passport.douyin.com\tFALSE\t/\tTRUE\t0\tsid_guard\tfixture-host\n"
	if s.entries["douyin"].Jar != want {
		t.Fatalf("loaded jar not normalized: %q", s.entries["douyin"].Jar)
	}
	persisted, err := os.ReadFile(filepath.Join(dir, "cookies.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]entry
	if err := json.Unmarshal(persisted, &entries); err != nil {
		t.Fatal(err)
	}
	if entries["douyin"].Jar != want {
		t.Fatalf("normalized jar not persisted: %q", entries["douyin"].Jar)
	}

	s.entries["douyin"] = legacy
	path, cleanup, err := s.Snapshot("douyin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	snapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot) != want {
		t.Fatalf("snapshot jar mismatch: %q", snapshot)
	}
	assertMozillaJar(t, string(snapshot))
	if got := s.Header("douyin", "https://passport.douyin.com/"); !strings.Contains(got, "sid_guard=fixture-host") {
		t.Fatalf("host-only cookie missing on original host: %q", got)
	}
	if got := s.Header("douyin", "https://child.passport.douyin.com/"); strings.Contains(got, "sid_guard=fixture-host") {
		t.Fatalf("host-only cookie broadened to subdomain: %q", got)
	}
}

func assertMozillaJar(t *testing.T, jar string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(jar, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "# Netscape HTTP Cookie File" {
		t.Fatalf("invalid Mozilla cookie jar header: %q", jar)
	}
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 7 || fields[0] == "" || (fields[1] != "TRUE" && fields[1] != "FALSE") ||
			!strings.HasPrefix(fields[2], "/") || (fields[3] != "TRUE" && fields[3] != "FALSE") ||
			strings.HasPrefix(fields[0], ".") != (fields[1] == "TRUE") || !validName(fields[5]) || !validValue(fields[6]) {
			t.Fatalf("invalid Mozilla cookie row: %q", line)
		}
		if expiry, err := strconv.ParseInt(fields[4], 10, 64); err != nil || expiry < 0 {
			t.Fatalf("invalid Mozilla cookie expiry: %q", line)
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
