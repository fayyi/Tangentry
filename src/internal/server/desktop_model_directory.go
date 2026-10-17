package server

import (
	"errors"
	"net/http"

	"tangentry/internal/transcription"
)

func (b *Backend) routeModelDirectory(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/desktop/model-directory" {
		return false
	}
	if b.state == nil {
		desktopJSON(w, http.StatusServiceUnavailable, map[string]string{"message": "桌面目录状态暂不可用。"})
		return true
	}
	if r.Method == http.MethodGet {
		desktopJSON(w, http.StatusOK, map[string]string{"directory": b.state.ModelDirectory()})
		return true
	}
	if r.Method != http.MethodPost || b.shell.PickPath == nil {
		return false
	}
	b.dialogMu.Lock()
	defer b.dialogMu.Unlock()
	path, err := b.shell.PickPath(r.Context(), "model-directory", b.state.ModelDirectory())
	if err == nil && path != "" {
		err = b.state.SetModelDirectory(r.Context(), path)
	}
	if err != nil {
		message := "模型迁移未完成，原模型已保留。请选择一个可写且没有 Tangentry Models 文件夹的位置。"
		if errors.Is(err, transcription.ErrBusy) {
			message = "请等待模型下载或转录结束后再迁移模型。"
		}
		desktopJSON(w, http.StatusBadRequest, map[string]string{"message": message})
		return true
	}
	desktopJSON(w, http.StatusOK, map[string]any{"directory": b.state.ModelDirectory(), "cancelled": path == ""})
	return true
}
