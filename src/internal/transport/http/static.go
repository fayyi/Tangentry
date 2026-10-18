package httptransport

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"tangentry/internal/preferences"
)

func (h *Handler) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(h.assets, name); err != nil {
		if strings.Contains(name, ".") {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
		data, err := fs.ReadFile(h.assets, name)
		if err != nil {
			http.Error(w, "App unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = w.Write(h.injectTheme(r, data))
		}
		return
	}
	clone := r.Clone(r.Context())
	clone.URL.Path = "/" + name
	http.FileServer(http.FS(h.assets)).ServeHTTP(w, clone)
}

func (h *Handler) injectTheme(r *http.Request, data []byte) []byte {
	theme := preferences.ThemeSystem
	saved := false
	if h.theme != nil {
		if loaded, persisted, err := h.theme.Load(r.Context()); err == nil {
			theme, saved = loaded, persisted
		}
	}
	encodedTheme, _ := json.Marshal(string(theme))
	encodedSaved, _ := json.Marshal(strconv.FormatBool(saved))
	data = bytes.Replace(data, []byte(`"__TANGENTRY_THEME__"`), encodedTheme, 1)
	return bytes.Replace(data, []byte(`"__TANGENTRY_THEME_SAVED__"`), encodedSaved, 1)
}
