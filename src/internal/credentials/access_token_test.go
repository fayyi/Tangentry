package credentials

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPodcastFingerprintSurvivesRefreshReopenAndManualReplacement(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, "", "")
	if err := s.SaveTokens("initial-access", "initial-refresh"); err != nil {
		t.Fatal(err)
	}
	scope := s.Fingerprint("xiaoyuzhou", "")
	if err := s.SaveTokensIfCurrent("initial-access", "initial-refresh", "rotated-access", "rotated-refresh"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, "", "")
	if err != nil || s.Fingerprint("xiaoyuzhou", "") != scope {
		t.Fatal("refresh or restart lost pending inventory scope", err)
	}
	if err := s.Save("xiaoyuzhou", "header", "x-jike-access-token=manual-access; x-jike-refresh-token=manual-refresh"); err != nil {
		t.Fatal(err)
	}
	replaced := s.Fingerprint("xiaoyuzhou", "")
	if replaced != scope {
		t.Fatal("manual replacement unexpectedly changed the single-user scope")
	}
	if err := s.SaveTokensIfCurrent("rotated-access", "rotated-refresh", "late-access", "late-refresh"); !errors.Is(err, ErrChanged) {
		t.Fatal("stale refresh accepted", err)
	}
	if s.Fingerprint("xiaoyuzhou", "") != replaced || s.AccessToken("xiaoyuzhou") != "manual-access" {
		t.Fatal("stale refresh changed account or tokens")
	}
	if err := s.Clear("xiaoyuzhou"); err != nil {
		t.Fatal(err)
	}
	if s.Fingerprint("xiaoyuzhou", "") != replaced {
		t.Fatal("clearing credentials changed the single-user scope")
	}
}

func TestAppTokenStoredPrivatelyAndNeverUsedAsCookie(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("xiaoyuzhou", "token", "synthetic-app-token"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.AccessToken("xiaoyuzhou") != "synthetic-app-token" || reopened.Header("xiaoyuzhou", "https://api.xiaoyuzhoufm.com/") != "" {
		t.Fatal("token storage or Cookie isolation failed")
	}
	raw, _ := json.Marshal(reopened.States())
	if strings.Contains(string(raw), "synthetic-app-token") {
		t.Fatal("secret in settings")
	}
	for _, token := range []string{"", "bad\r\nheader", "name=value; other=value"} {
		if s.Save("xiaoyuzhou", "token", token) == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if s.Save("youtube", "token", "synthetic-app-token") == nil {
		t.Fatal("token accepted for Cookie platform")
	}
	if err := reopened.Clear("xiaoyuzhou"); err != nil || reopened.AccessToken("xiaoyuzhou") != "" {
		t.Fatal("token not cleared", err)
	}
}

func TestPodcastCookieExtractsBothTokensAndDiscardsOtherCookies(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, "", "")
	for _, format := range []string{"token", "header"} {
		if err := s.Save("xiaoyuzhou", format, "Cookie: _tracking=ignored; empty=; x-jike-access-token=synthetic-access; x-jike-refresh-token=synthetic-refresh"); err != nil {
			t.Fatal(err)
		}
		reopened, err := Open(dir, "", "")
		if err != nil || reopened.AccessToken("xiaoyuzhou") != "synthetic-access" || reopened.entries["xiaoyuzhou"].Refresh != "synthetic-refresh" || reopened.Header("xiaoyuzhou", "https://api.xiaoyuzhoufm.com/") != "" {
			t.Fatal("token extraction/isolation failed", err)
		}
		state, _ := json.Marshal(reopened.States())
		if strings.Contains(string(state), "synthetic") {
			t.Fatal("secret exposed")
		}
	}
	for _, value := range []string{"x-jike-refresh-token=synthetic-refresh", "x-jike-access-token=synthetic-access; x-jike-access-token=another-access", "x-jike-access-token=synthetic-access; x-jike-refresh-token=bad", "x-jike-access-token=synthetic-access\r\nInjected: value"} {
		if s.Save("xiaoyuzhou", "token", value) == nil || s.AccessToken("xiaoyuzhou") != "synthetic-access" {
			t.Fatal("invalid input replaced credentials")
		}
	}
}

func TestFingerprintTreatsCredentialRotationAsSameLocalOwner(t *testing.T) {
	s, _ := Open(t.TempDir(), "", "")
	anonymous := s.Fingerprint("xiaoyuzhou", "https://api.xiaoyuzhoufm.com/v1/discovery-feed/list")
	if len(anonymous) != 64 {
		t.Fatal("invalid anonymous fingerprint")
	}
	if err := s.SaveTokens("access-one", "refresh-one"); err != nil {
		t.Fatal(err)
	}
	one := s.Fingerprint("xiaoyuzhou", "https://api.xiaoyuzhoufm.com/v1/discovery-feed/list")
	if one != anonymous || len(one) != 64 {
		t.Fatal("credential import changed the single-owner scope")
	}
	if strings.Contains(one, "access-one") || strings.Contains(one, "refresh-one") {
		t.Fatal("fingerprint exposed a token")
	}
	if err := s.SaveTokens("access-two", "refresh-two"); err != nil {
		t.Fatal(err)
	}
	if two := s.Fingerprint("xiaoyuzhou", "https://api.xiaoyuzhoufm.com/v1/discovery-feed/list"); two != one {
		t.Fatal("credential rotation changed the single-owner scope")
	}
}
