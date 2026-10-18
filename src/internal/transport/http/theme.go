package httptransport

import (
	"encoding/json"
	"io"
	"net/http"

	"tangentry/internal/preferences"
)

type themeInput struct {
	Theme string `json:"theme"`
}

func (h *Handler) themeGet(w http.ResponseWriter, r *http.Request) {
	theme := preferences.ThemeSystem
	saved := false
	if h.theme != nil {
		loaded, persisted, err := h.theme.Load(r.Context())
		if err == nil {
			theme, saved = loaded, persisted
		}
	}
	write(w, http.StatusOK, map[string]any{"theme": theme, "saved": saved})
}

func (h *Handler) themePut(w http.ResponseWriter, r *http.Request) {
	if h.theme == nil {
		write(w, http.StatusServiceUnavailable, apiError{"theme_unavailable", "主题设置暂时不可用。"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input themeInput
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		write(w, http.StatusBadRequest, apiError{"invalid_theme", "请选择浅色、深色或跟随系统。"})
		return
	}
	theme := preferences.ParseTheme(input.Theme)
	if theme != preferences.Theme(input.Theme) {
		write(w, http.StatusBadRequest, apiError{"invalid_theme", "请选择浅色、深色或跟随系统。"})
		return
	}
	if err := h.theme.Save(r.Context(), theme); err != nil {
		write(w, http.StatusServiceUnavailable, apiError{"theme_unavailable", "主题设置未能保存，请检查数据目录权限。"})
		return
	}
	write(w, http.StatusOK, map[string]any{"theme": theme, "saved": true})
}
