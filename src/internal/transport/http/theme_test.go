package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"tangentry/internal/adapters/filesystem"
	"tangentry/internal/adapters/sqlite"
	"tangentry/internal/content"
	"tangentry/internal/discovery"
	"tangentry/internal/preferences"
)

func newThemeTestHandler(t *testing.T, store *filesystem.ThemePreferences) http.Handler {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(content.NewService(db), discovery.NewService(testProvider{}), fstest.MapFS{
		"index.html": {Data: []byte(`<script>const theme = "__TANGENTRY_THEME__"; const saved = "__TANGENTRY_THEME_SAVED__" === 'true'</script>`)},
	}, Options{Theme: store})
}

func themeRequest(handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, body)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestThemeAPIReadsWritesAndValidates(t *testing.T) {
	store := filesystem.NewThemePreferences(filepath.Join(t.TempDir(), "preferences.json"))
	handler := newThemeTestHandler(t, store)

	response := themeRequest(handler, http.MethodGet, "/api/settings/theme", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("default GET status = %d", response.Code)
	}
	var initial struct {
		Theme string `json:"theme"`
		Saved bool   `json:"saved"`
	}
	if err := json.NewDecoder(response.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.Theme != string(preferences.ThemeSystem) || initial.Saved {
		t.Fatalf("default theme = %+v", initial)
	}

	response = themeRequest(handler, http.MethodPut, "/api/settings/theme", bytes.NewBufferString(`{"theme":"dark"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", response.Code, response.Body.String())
	}
	response = themeRequest(handler, http.MethodGet, "/api/settings/theme", nil)
	var saved struct {
		Theme string `json:"theme"`
		Saved bool   `json:"saved"`
	}
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Theme != string(preferences.ThemeDark) || !saved.Saved {
		t.Fatalf("saved theme = %+v", saved)
	}

	for _, payload := range []string{`{"theme":"neon"}`, `{"theme":"dark"} trailing`} {
		response = themeRequest(handler, http.MethodPut, "/api/settings/theme", bytes.NewBufferString(payload))
		if response.Code != http.StatusBadRequest {
			t.Errorf("payload %q status = %d", payload, response.Code)
		}
	}
}

func TestThemeAPIValueSurvivesHandlerReconstruction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	first := filesystem.NewThemePreferences(path)
	if err := first.Save(context.Background(), preferences.ThemeLight); err != nil {
		t.Fatal(err)
	}
	second := filesystem.NewThemePreferences(path)
	handler := newThemeTestHandler(t, second)
	response := themeRequest(handler, http.MethodGet, "/api/settings/theme", nil)
	var value struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || value.Theme != string(preferences.ThemeLight) {
		t.Fatalf("reopened theme status=%d value=%q", response.Code, value.Theme)
	}
}

func TestIndexHTMLInjectsPersistedThemeBeforeAppScripts(t *testing.T) {
	store := filesystem.NewThemePreferences(filepath.Join(t.TempDir(), "preferences.json"))
	if err := store.Save(context.Background(), preferences.ThemeDark); err != nil {
		t.Fatal(err)
	}
	handler := newThemeTestHandler(t, store)
	response := themeRequest(handler, http.MethodGet, "/", nil)
	body, _ := io.ReadAll(response.Body)
	html := string(body)
	if response.Code != http.StatusOK {
		t.Fatalf("index status = %d", response.Code)
	}
	if !strings.Contains(html, `const theme = "dark"`) || !strings.Contains(html, `const saved = "true" === 'true'`) {
		t.Fatalf("theme was not injected: %s", html)
	}
	if strings.Contains(html, "__TANGENTRY_THEME") {
		t.Fatalf("placeholder leaked into HTML: %s", html)
	}
}
