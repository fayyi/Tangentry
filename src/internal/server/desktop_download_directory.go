package server

import "net/http"

func (b *Backend) routeDownloadDirectory(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/desktop/download-directory" || r.Method != http.MethodGet {
		return false
	}
	if b.state == nil {
		desktopJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "桌面目录状态暂不可用。"})
		return true
	}
	path := ""
	if b.state.DownloadDirectorySelected() {
		path = b.state.DownloadDirectory()
	}
	desktopJSON(w, http.StatusOK, map[string]any{"directory": path})
	return true
}
