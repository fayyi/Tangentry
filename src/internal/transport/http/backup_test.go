package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tangentry/internal/backup"
)

type backupHTTPFixture struct{ tested backup.Target }

func (backupHTTPFixture) Targets(context.Context) ([]backup.Target, error)     { return nil, nil }
func (backupHTTPFixture) Tasks(context.Context) ([]backup.Task, error)         { return nil, nil }
func (backupHTTPFixture) Snapshots(context.Context) ([]backup.Snapshot, error) { return nil, nil }
func (backupHTTPFixture) SaveTarget(context.Context, backup.Target) error      { return nil }
func (backupHTTPFixture) DeleteTarget(context.Context, string) error           { return nil }
func (backupHTTPFixture) SaveTask(context.Context, backup.Task) error          { return nil }
func (backupHTTPFixture) DeleteTask(context.Context, string) error             { return nil }
func (backupHTTPFixture) Create(context.Context, string, string) (backup.Snapshot, error) {
	return backup.Snapshot{ID: "snapshot-1", Status: "succeeded"}, nil
}
func (backupHTTPFixture) RetryFailed(context.Context, string) (backup.Snapshot, error) {
	return backup.Snapshot{ID: "snapshot-1", Status: "succeeded"}, nil
}
func (f backupHTTPFixture) TestTarget(_ context.Context, target backup.Target) error {
	f.tested = target
	return nil
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
