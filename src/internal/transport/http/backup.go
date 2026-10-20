package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"tangentry/internal/backup"
)

type backupHandler struct{ service BackupService }

type backupTargetRequest struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Path             string `json:"path,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	Username         string `json:"username,omitempty"`
	RemotePath       string `json:"remotePath,omitempty"`
	FolderPath       string `json:"folderPath,omitempty"`
	FolderName       string `json:"folderName,omitempty"`
	AllowInsecureTLS bool   `json:"allowInsecureTLS,omitempty"`
	Password         string `json:"password,omitempty"`
	UpdateCredential bool   `json:"updateCredential,omitempty"`
	Enabled          bool   `json:"enabled"`
}

func (v backupTargetRequest) target() backup.Target {
	return backup.Target{ID: v.ID, Name: v.Name, Type: v.Type, Path: v.Path, Endpoint: v.Endpoint, Username: v.Username, RemotePath: v.RemotePath, AllowInsecureTLS: v.AllowInsecureTLS, Password: v.Password, UpdateCredential: v.UpdateCredential, Enabled: v.Enabled}
}

func (h *backupHandler) targets(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.Targets(r.Context())
	h.respond(w, v, err)
}
func (h *backupHandler) tasks(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.Tasks(r.Context())
	h.respond(w, v, err)
}
func (h *backupHandler) snapshots(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.Snapshots(r.Context())
	h.respond(w, v, err)
}
func (h *backupHandler) saveTarget(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var operationErr error
	targetType := "unknown"
	defer func() { logBackupHTTP("save_target", targetType, time.Since(started), operationErr) }()
	var request backupTargetRequest
	if !decodeJSON(w, r, &request) {
		operationErr = errors.New("invalid request")
		return
	}
	targetType = safeBackupTargetType(request.Type)
	v := request.target()
	if err := h.service.SaveTarget(r.Context(), v); err != nil {
		operationErr = err
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
}

func (h *backupHandler) browseTarget(w http.ResponseWriter, r *http.Request) {
	var request backupTargetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	folders, err := h.service.BrowseTarget(r.Context(), request.target(), request.FolderPath)
	if err != nil {
		h.folderError(w, err)
		return
	}
	write(w, http.StatusOK, folders)
}

func (h *backupHandler) createFolder(w http.ResponseWriter, r *http.Request) {
	var request backupTargetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	exists, err := h.service.CreateTargetFolder(r.Context(), request.target(), request.FolderPath, request.FolderName)
	if err != nil {
		h.folderError(w, err)
		return
	}
	status := "created"
	if exists {
		status = "exists"
	}
	write(w, http.StatusOK, map[string]string{"status": status})
}

func (h *backupHandler) testTarget(w http.ResponseWriter, r *http.Request) {
	var request backupTargetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.service.TestTarget(r.Context(), request.target()); err != nil {
		if errors.Is(err, context.Canceled) {
			write(w, http.StatusRequestTimeout, apiError{"backup_test_cancelled", "连接测试已取消。"})
			return
		}
		write(w, http.StatusBadRequest, apiError{"backup_test_failed", safeBackupTestError(err)})
		return
	}
	write(w, http.StatusOK, map[string]string{"status": "succeeded", "message": "连接成功。"})
}
func (h *backupHandler) deleteTarget(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteTarget(r.Context(), r.PathValue("id")); err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
}
func (h *backupHandler) saveTask(w http.ResponseWriter, r *http.Request) {
	var v backup.Task
	if !decodeJSON(w, r, &v) {
		return
	}
	if err := h.service.SaveTask(r.Context(), v); err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
}
func (h *backupHandler) deleteTask(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteTask(r.Context(), r.PathValue("id")); err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
}
func (h *backupHandler) run(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &v) {
		return
	}
	snap, err := h.service.Create(r.Context(), r.PathValue("id"), v.Password)
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusCreated, snap)
}
func (h *backupHandler) retryFailed(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.service.RetryFailed(r.Context(), r.PathValue("id"))
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, snapshot)
}
func (h *backupHandler) restore(w http.ResponseWriter, r *http.Request) {
	var v struct {
		TargetID   string `json:"targetId"`
		SnapshotID string `json:"snapshotId"`
		Password   string `json:"password"`
	}
	if !decodeJSON(w, r, &v) {
		return
	}
	result, err := h.service.Restore(r.Context(), v.TargetID, v.SnapshotID, v.Password, "")
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
func (h *backupHandler) validate(w http.ResponseWriter, r *http.Request) {
	var v struct {
		TargetID   string `json:"targetId"`
		SnapshotID string `json:"snapshotId"`
		Password   string `json:"password"`
	}
	if !decodeJSON(w, r, &v) {
		return
	}
	result, err := h.service.Validate(r.Context(), v.TargetID, v.SnapshotID, v.Password)
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
func (h *backupHandler) confirmRestore(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ConfirmRestore(r.Context(), r.PathValue("id"))
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
func (h *backupHandler) cancelRestore(w http.ResponseWriter, r *http.Request) {
	if err := h.service.CancelRestore(r.Context(), r.PathValue("id")); err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
}
func (h *backupHandler) respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, v)
}
func (h *backupHandler) error(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, context.Canceled) {
		code = http.StatusRequestTimeout
	}
	write(w, code, apiError{"backup_error", safeBackupError(err)})
}
func safeBackupError(err error) string {
	if errors.Is(err, backup.ErrTargetConnectionChanged) {
		return "地址或用户名已修改，请重新输入密码。"
	}
	if errors.Is(err, backup.ErrBadPassword) {
		return "备份密码错误或快照已损坏。"
	}
	return "备份操作未完成，请检查目标设置后重试。"
}
func safeBackupTestError(err error) string {
	if errors.Is(err, backup.ErrTargetConnectionChanged) {
		return "地址或用户名已修改，请重新输入密码。"
	}
	var summary interface{ ErrorSummary() string }
	if errors.As(err, &summary) {
		return summary.ErrorSummary()
	}
	return "连接失败，请检查地址、账号、密码和网络。"
}

func (h *backupHandler) folderError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "backup_folder_error"
	message := "远端目录操作失败，请检查设置后重试。"
	switch {
	case errors.Is(err, backup.ErrTargetConnectionChanged):
		status, code, message = http.StatusBadRequest, "backup_folder_credentials_changed", "地址或用户名已修改，请重新输入密码。"
	case errors.Is(err, backup.ErrFolderUnauthorized):
		status, code, message = http.StatusUnauthorized, "backup_folder_unauthorized", "WebDAV 认证失败，请检查账号和密码。"
	case errors.Is(err, backup.ErrFolderPermissionDenied):
		status, code, message = http.StatusForbidden, "backup_folder_forbidden", "WebDAV 服务器拒绝了目录操作。"
	case errors.Is(err, backup.ErrFolderParentNotFound):
		status, code, message = http.StatusNotFound, "backup_folder_parent_missing", "远端父目录不存在。"
	case errors.Is(err, backup.ErrFolderResponseTooLarge):
		status, code, message = http.StatusBadGateway, "backup_folder_response_too_large", "WebDAV 响应过大，已拒绝处理。"
	case errors.Is(err, backup.ErrTooManyFolders):
		status, code, message = http.StatusBadGateway, "backup_folder_too_many", "远端子文件夹数量超过上限。"
	case errors.Is(err, backup.ErrInvalidFolderName):
		code, message = "backup_folder_invalid_name", "文件夹名称不合法。"
	case errors.Is(err, backup.ErrInvalidFolderPath):
		code, message = "backup_folder_invalid_path", "远端目录路径不合法。"
	}
	write(w, status, apiError{code, message})
}

func logBackupHTTP(operation, targetType string, elapsed time.Duration, err error) {
	errorClass := "none"
	if err != nil {
		errorClass = "operation"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			errorClass = "cancelled"
		}
	}
	slog.Info("backup target http timing", "operation", operation, "duration_ms", elapsed.Milliseconds(), "target_type", targetType, "error_class", errorClass)
}

func safeBackupTargetType(value string) string {
	if value == "local" || value == "webdav" {
		return value
	}
	return "unknown"
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		write(w, http.StatusBadRequest, apiError{"invalid_request", "请求格式不正确。"})
		return false
	}
	return true
}
