package server

import (
	"encoding/json"
	"net/http"
)

func (b *Backend) chooseDirectory(r *http.Request) (string, error) {
	if b.state == nil {
		return "", ErrStateUnavailable
	}
	path, err := b.shell.PickPath(r.Context(), "download-directory", b.state.DownloadDirectory())
	if err == nil && path != "" {
		err = b.state.SetDownloadDirectory(path)
	}
	return path, err
}

func (b *Backend) routePathSelection(w http.ResponseWriter, r *http.Request) bool {
	picking := r.Method == http.MethodPost && (r.URL.Path == "/api/desktop/download-directory" || r.URL.Path == "/api/desktop/select-path" || (r.URL.Path == "/api/downloads" && b.state != nil && !b.state.DownloadDirectorySelected()))
	if !picking {
		return false
	}
	b.dialogMu.Lock()
	defer b.dialogMu.Unlock()
	if b.shell.PickPath == nil {
		desktopJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "文件选择暂不可用。"})
		return true
	}
	var path string
	var err error
	if r.URL.Path == "/api/desktop/select-path" {
		var input struct {
			Kind    string `json:"kind"`
			Current string `json:"current"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
			desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "路径选择参数无效。"})
			return true
		}
		switch input.Kind {
		case "whisper-binary", "whisper-model", "python", "model-directory":
		default:
			desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "路径类型无效。"})
			return true
		}
		path, err = b.shell.PickPath(r.Context(), input.Kind, input.Current)
	} else {
		// A second download request may have waited while the first chose a folder.
		if r.URL.Path == "/api/downloads" && b.state != nil && b.state.DownloadDirectorySelected() {
			b.applicationHandler().ServeHTTP(w, r)
			return true
		}
		path, err = b.chooseDirectory(r)
	}
	if err != nil {
		desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "无法选择或保存路径，请确认位置可访问。"})
		return true
	}
	if r.Context().Err() != nil {
		return true
	}
	if r.URL.Path != "/api/downloads" {
		desktopJSON(w, http.StatusOK, map[string]any{"directory": path, "path": path, "cancelled": path == ""})
		return true
	}
	if path == "" {
		desktopJSON(w, http.StatusBadRequest, map[string]string{"message": "请选择下载目录后开始下载。"})
		return true
	}
	return false
}
