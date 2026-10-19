package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"tangentry/internal/backup"
)

type backupHandler struct{ service BackupService }

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
	var v backup.Target
	if !decodeJSON(w, r, &v) {
		return
	}
	if err := h.service.SaveTarget(r.Context(), v); err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusNoContent, nil)
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
	if errors.Is(err, backup.ErrBadPassword) {
		return "备份密码错误或快照已损坏。"
	}
	return "备份操作未完成，请检查目标设置后重试。"
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		write(w, http.StatusBadRequest, apiError{"invalid_request", "请求格式不正确。"})
		return false
	}
	return true
}
