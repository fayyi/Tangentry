package httptransport

import "net/http"

func (h *Handler) registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backup/targets", h.backup.targets)
	mux.HandleFunc("POST /api/backup/targets", h.backup.saveTarget)
	mux.HandleFunc("POST /api/backup/targets/test", h.backup.testTarget)
	mux.HandleFunc("POST /api/backup/targets/browse", h.backup.browseTarget)
	mux.HandleFunc("POST /api/backup/targets/mkdir", h.backup.createFolder)
	mux.HandleFunc("DELETE /api/backup/targets/{id}", h.backup.deleteTarget)
	mux.HandleFunc("GET /api/backup/tasks", h.backup.tasks)
	mux.HandleFunc("POST /api/backup/tasks", h.backup.saveTask)
	mux.HandleFunc("DELETE /api/backup/tasks/{id}", h.backup.deleteTask)
	mux.HandleFunc("GET /api/backup/snapshots", h.backup.snapshots)
	mux.HandleFunc("POST /api/backup/tasks/{id}/run", h.backup.run)
	mux.HandleFunc("POST /api/backup/snapshots/{id}/retry-failed", h.backup.retryFailed)
	mux.HandleFunc("POST /api/backup/validate", h.backup.validate)
	mux.HandleFunc("POST /api/backup/restore", h.backup.restore)
	mux.HandleFunc("POST /api/backup/restore/{id}/confirm", h.backup.confirmRestore)
	mux.HandleFunc("DELETE /api/backup/restore/{id}", h.backup.cancelRestore)
}
