package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestUnavailableAISettingsDoNotBlockExistingLibrary(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"TANGENTRY_DOWNLOAD_DIR": filepath.Join(root, "downloads"), "TANGENTRY_YTDLP_PATH": filepath.Join(root, "absent-tool"), "TANGENTRY_FFMPEG_PATH": filepath.Join(root, "absent-tool"), "TANGENTRY_BILIBILI_COOKIE_FILE": "", "TANGENTRY_BILIBILI_COOKIE": ""} {
		t.Setenv(name, value)
	}
	if err := os.MkdirAll(filepath.Join(root, "ai"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ai", "settings.json"), []byte("malformed private configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	app, err := New(root, fstest.MapFS{})
	if err != nil {
		t.Fatal("optional AI prevented startup", err)
	}
	defer app.Close()
	for path, status := range map[string]int{"/api/health": 200, "/api/library": 200, "/api/ai/settings": 503} {
		r := httptest.NewRequest("GET", "http://127.0.0.1"+path, nil)
		w := httptest.NewRecorder()
		app.HTTPHandler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
