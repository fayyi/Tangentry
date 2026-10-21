package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tangentry/internal/backup"
)

type backupHTTPFixture struct {
	testErr   error
	saveErr   error
	browseErr error
	createErr error
}

func (backupHTTPFixture) Targets(context.Context) ([]backup.Target, error)     { return nil, nil }
func (backupHTTPFixture) Tasks(context.Context) ([]backup.Task, error)         { return nil, nil }
func (backupHTTPFixture) Snapshots(context.Context) ([]backup.Snapshot, error) { return nil, nil }
func (f backupHTTPFixture) SaveTarget(context.Context, backup.Target) error    { return f.saveErr }
func (backupHTTPFixture) DeleteTarget(context.Context, string) error           { return nil }
func (backupHTTPFixture) SaveTask(context.Context, backup.Task) error          { return nil }
func (backupHTTPFixture) DeleteTask(context.Context, string) error             { return nil }
func (f backupHTTPFixture) Create(context.Context, string, string) (backup.Snapshot, error) {
	return backup.Snapshot{ID: "snapshot-1", Status: "partial_success", Targets: []backup.TargetRun{{TargetID: "target-1", Name: "Fixture", Status: "failed", Error: "WebDAV 返回 HTTP 403。"}}}, f.createErr
}
func (backupHTTPFixture) RetryFailed(context.Context, string) (backup.Snapshot, error) {
	return backup.Snapshot{ID: "snapshot-1", Status: "succeeded"}, nil
}
func (f backupHTTPFixture) TestTarget(context.Context, backup.Target) error {
	return f.testErr
}
func (backupHTTPFixture) Validate(context.Context, string, string, string) (backup.Validation, error) {
	return backup.Validation{SnapshotID: "snapshot-1"}, nil
}
func (backupHTTPFixture) Restore(context.Context, string, string, string, string) (backup.RestoreResult, error) {
	return backup.RestoreResult{PlanID: "plan-1", Status: "pending_confirmation", StagedID: "stage-1", PreRestoreSnapshotID: "pre-1"}, nil
}
func (backupHTTPFixture) ConfirmRestore(context.Context, string) (backup.RestoreResult, error) {
	return backup.RestoreResult{PlanID: "plan-1", Status: "switched"}, nil
}
func (backupHTTPFixture) CancelRestore(context.Context, string) error { return nil }
func (f backupHTTPFixture) BrowseTarget(context.Context, backup.Target, string) ([]backup.RemoteFolder, error) {
	if f.browseErr != nil {
		return nil, f.browseErr
	}
	return []backup.RemoteFolder{{Name: "Tangentry", Path: "Tangentry"}}, nil
}
func (backupHTTPFixture) CreateTargetFolder(context.Context, backup.Target, string, string) (bool, error) {
	return false, nil
}

func TestBackupRestoreRoutesRequireSameOriginAndDoNotEchoPassword(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{}}, nil)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	password := "fixture-secret-not-echoed"
	if w := request(http.MethodPost, "/api/backup/validate", `{"targetId":"target-1","snapshotId":"snapshot-1","password":"`+password+`"}`); w.Code != http.StatusOK || strings.Contains(w.Body.String(), password) {
		t.Fatalf("validate response leaked password or failed: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/backup/restore", `{"targetId":"target-1","snapshotId":"snapshot-1","password":"`+password+`"}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pending_confirmation") || strings.Contains(w.Body.String(), password) {
		t.Fatalf("restore response: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/backup/restore/plan-1/confirm", "{}"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "switched") {
		t.Fatalf("confirm response: %d %s", w.Code, w.Body.String())
	}
	badOrigin := httptest.NewRequest(http.MethodPost, "/api/backup/validate", strings.NewReader(`{}`))
	badOrigin.Host = "127.0.0.1"
	badOrigin.Header.Set("Origin", "https://evil.example")
	badOrigin.Header.Set("Content-Type", "application/json")
	badResponse := httptest.NewRecorder()
	h.ServeHTTP(badResponse, badOrigin)
	if badResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request accepted: %d", badResponse.Code)
	}
}

func TestBackupListRoutesReturnEmptyArrays(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{}}, nil)
	for _, path := range []string{"/api/backup/targets", "/api/backup/tasks", "/api/backup/snapshots"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Host = "127.0.0.1"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
				t.Fatalf("GET %s = %d %s, want 200 []", path, w.Code, w.Body.String())
			}
		})
	}
}

func TestBackupEncryptionStateUnavailableForLegacyFixture(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{}}, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/backup/encryption", nil)
	r.Host = "127.0.0.1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "backup_encryption_unavailable") {
		t.Fatalf("encryption state should fail closed without credential support: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupRunReturnsPersistedSnapshotWhenUploadFails(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{createErr: errors.New("fixture upload failure")}}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/backup/tasks/task-1/run", strings.NewReader(`{"password":"fixture-backup-password"}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), `"snapshot":{"id":"snapshot-1"`) || !strings.Contains(w.Body.String(), "HTTP 403") || strings.Contains(w.Body.String(), "fixture-backup-password") {
		t.Fatalf("backup upload failure did not return its safe persisted result: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupTargetTestDoesNotEchoOrPersistSubmittedPassword(t *testing.T) {
	fixture := backupHTTPFixture{}
	h := NewWithDependencies(Dependencies{Backup: fixture}, nil)
	password := "fixture-webdav-password"
	r := httptest.NewRequest(http.MethodPost, "/api/backup/targets/test", strings.NewReader(`{"type":"webdav","endpoint":"https://dav.example.test/root","username":"fixture-user","password":"`+password+`","updateCredential":true}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), password) {
		t.Fatalf("test endpoint failed or echoed password: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupTargetTestReportsWhenSavedCredentialsNoLongerMatch(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{testErr: backup.ErrTargetConnectionChanged}}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/backup/targets/test", strings.NewReader(`{"id":"saved-target","type":"webdav","endpoint":"https://new.example.test/dav","username":"fixture-user"}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "地址或用户名已修改，请重新输入密码") {
		t.Fatalf("changed saved credential identity was not clearly reported: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupTargetSaveReportsWhenSavedCredentialsNoLongerMatch(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{saveErr: backup.ErrTargetConnectionChanged}}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/backup/targets", strings.NewReader(`{"id":"saved-target","type":"webdav","endpoint":"https://new.example.test/dav","username":"fixture-user"}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "地址或用户名已修改，请重新输入密码") {
		t.Fatalf("changed saved credential identity was not clearly reported: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupFolderRoutesReturnRedactedFolderDataAndStatus(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{}}, nil)
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	browse := request("/api/backup/targets/browse", `{"type":"webdav","endpoint":"https://dav.example.test/root","username":"fixture-user","password":"fixture-password","folderPath":""}`)
	if browse.Code != http.StatusOK || !strings.Contains(browse.Body.String(), `"path":"Tangentry"`) || strings.Contains(browse.Body.String(), "fixture-password") {
		t.Fatalf("browse response: %d %s", browse.Code, browse.Body.String())
	}
	mkdir := request("/api/backup/targets/mkdir", `{"type":"webdav","endpoint":"https://dav.example.test/root","username":"fixture-user","password":"fixture-password","folderPath":"","folderName":"Tangentry"}`)
	if mkdir.Code != http.StatusOK || !strings.Contains(mkdir.Body.String(), `"status":"created"`) || strings.Contains(mkdir.Body.String(), "fixture-password") {
		t.Fatalf("mkdir response: %d %s", mkdir.Code, mkdir.Body.String())
	}
}

func TestBackupFolderBrowseMapsTimeoutToGatewayTimeout(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{browseErr: context.DeadlineExceeded}}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/backup/targets/browse", strings.NewReader(`{"type":"webdav","endpoint":"https://dav.example.test/","username":"fixture-user","password":"fixture-password","folderPath":""}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), "读取 WebDAV 目录超时") || strings.Contains(w.Body.String(), "fixture-password") {
		t.Fatalf("browse timeout response: %d %s", w.Code, w.Body.String())
	}
}

func TestBackupFolderErrorDoesNotMisclassifyOtherErrorsAsTimeout(t *testing.T) {
	h := NewWithDependencies(Dependencies{Backup: backupHTTPFixture{browseErr: errors.New("fixture failure")}}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/backup/targets/browse", strings.NewReader(`{"type":"webdav","endpoint":"https://dav.example.test/","username":"fixture-user","password":"fixture-password","folderPath":""}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "超时") {
		t.Fatalf("non-timeout folder error was misclassified: %d %s", w.Code, w.Body.String())
	}
}
