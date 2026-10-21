package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestFormatUpdaterError(t *testing.T) {
	if got := formatUpdaterError(mygo.ErrUpdatesDisabled); got != "此版本未启用自动更新，请安装正式发布版本。" {
		t.Fatalf("disabled update error = %q", got)
	}
	if got := formatUpdaterError(errors.New("mygo: installing the update: the update is not signed with the app's key")); got != "更新签名校验失败，已取消安装。" {
		t.Fatalf("signature error = %q", got)
	}
}

func TestUpdaterInstallFailureKeepsReleaseVisible(t *testing.T) {
	service := newUpdaterService(t.TempDir())
	service.update = &mygo.Update{Version: "1.4.0"}
	service.status.Available = true
	service.install = func(context.Context, *mygo.Update, func(int64, int64)) error {
		return errors.New("mygo: installing the update: the update is not signed with the app's key")
	}
	status, err := service.Install(context.Background())
	if err == nil || !status.Available || status.State != "error" || status.Error != "更新签名校验失败，已取消安装。" {
		t.Fatalf("failed install status = %+v, error = %v", status, err)
	}
}

func TestUpdaterCheckDue(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		last time.Time
		want bool
	}{
		{name: "first check", want: true},
		{name: "too soon", last: now.Add(-5 * time.Hour), want: false},
		{name: "at interval", last: now.Add(-6 * time.Hour), want: true},
		{name: "over interval", last: now.Add(-7 * time.Hour), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := updaterCheckDue(test.last, now, updaterInterval); got != test.want {
				t.Fatalf("updaterCheckDue() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestUpdaterSkipAndVersionComparison(t *testing.T) {
	if !updaterShouldSkip("1.2.0", "1.2.0") || updaterShouldSkip("1.2.1", "1.2.0") {
		t.Fatal("skip preference did not match exactly one version")
	}
	for _, test := range []struct {
		candidate, current string
		want               bool
	}{
		{"1.10.0", "1.9.0", true},
		{"v1.2.0", "1.2.0", false},
		{"1.2.0-beta.1", "1.2.0", false},
		{"1.2.0-beta.10", "1.2.0-beta.2", true},
		{"1.2.0+build.2", "1.2.0+build.1", false},
		{"1.2.1", "1.2.0", true},
		{"0.0.3.1", "0.0.3", true},
	} {
		if got := updaterVersionNewer(test.candidate, test.current); got != test.want {
			t.Fatalf("updaterVersionNewer(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}

func TestUpdaterPreferencesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater.json")
	if got := loadUpdaterPreferences(path); !got.AutomaticChecks || got.SkippedVersion != "" || !got.LastCheck.IsZero() {
		t.Fatalf("default preferences = %+v", got)
	}

	service := newUpdaterService(filepath.Dir(path))
	service.mu.Lock()
	service.prefs.AutomaticChecks = false
	service.prefs.SkippedVersion = "1.4.0"
	service.prefs.LastCheck = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if err := service.savePreferencesLocked(); err != nil {
		service.mu.Unlock()
		t.Fatal(err)
	}
	service.mu.Unlock()

	got := loadUpdaterPreferences(path)
	if got.AutomaticChecks || got.SkippedVersion != "1.4.0" || !got.LastCheck.Equal(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("round-trip preferences = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("saved preferences missing: %v", err)
	}
}

func TestUpdaterCheckUsesManifestEndpoint(t *testing.T) {
	type manifest struct {
		Version string `json:"version"`
		Notes   string `json:"notes"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest/download/update-darwin-arm64.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(manifest{Version: "1.4.0", Notes: "- 修复更新"})
	}))
	defer server.Close()

	service := newUpdaterService(t.TempDir())
	service.check = func(ctx context.Context) (*mygo.Update, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/releases/latest/download/update-darwin-arm64.json", nil)
		if err != nil {
			return nil, err
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		var value manifest
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			return nil, err
		}
		return &mygo.Update{Version: value.Version, Notes: value.Notes}, nil
	}
	status, err := service.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.Version != "1.4.0" || status.Notes != "- 修复更新" || status.LastCheck == "" {
		t.Fatalf("manifest status = %+v", status)
	}
}
