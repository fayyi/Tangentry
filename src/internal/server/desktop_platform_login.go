package server

import (
	"encoding/json"
	"io"
	"net/http"
)

func (b *Backend) routePlatformLogin(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/desktop/platform-login" || r.Method != http.MethodPost {
		return false
	}
	var request struct {
		Platform string `json:"platform"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF {
		desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "平台登录参数无效。"})
		return true
	}
	if !validPlatformLoginPlatform(request.Platform) {
		desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "平台登录参数无效。"})
		return true
	}
	if b.shell.PlatformLogin == nil {
		desktopJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "应用内登录暂不可用。"})
		return true
	}
	if err := b.shell.PlatformLogin(r.Context(), request.Platform); err != nil {
		// The callback owns the native window and credential handling. The HTTP
		// surface only reports that opening it failed; cookie values never cross
		// this boundary.
		desktopJSON(w, http.StatusBadGateway, map[string]string{"message": "无法打开应用内登录窗口，请稍后重试。"})
		return true
	}
	desktopJSON(w, http.StatusOK, map[string]bool{"started": true})
	return true
}

func validPlatformLoginPlatform(platform string) bool {
	switch platform {
	case "linuxdo", "douyin", "bilibili", "xiaohongshu", "youtube":
		return true
	default:
		return false
	}
}
