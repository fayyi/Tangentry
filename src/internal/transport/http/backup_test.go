package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tangentry/internal/backup"
)

type backupHTTPFixture struct{}

func (backupHTTPFixture) Targets(context.Context) ([]backup.Target, error) { return nil, nil }
func (backupHTTPFixture) Tasks(context.Context) ([]backup.Task, error)     { return nil, nil }
func (backupHTTPFixture) Snapshots(context.Context) ([]backup.Snapshot, error) {
	return []backup.Snapshot{{ID: "snapshot-1"}}, nil
}
func (backupHTTPFixture) SaveTarget(context.Context, backup.Target) error { return nil }
func (backupHTTPFixture) DeleteTarget(context.Context, string) error      { return nil }
func (backupHTTPFixture) SaveTask(context.Context, backup.Task) error     { return nil }
func (backupHTTPFixture) DeleteTask(context.Context, string) error        { return nil }
func (backupHTTPFixture) Create(context.Context, string, string) (backup.Snapshot, error) {
	return backup.Snapshot{ID: "snapshot-1", Status: "succeeded"}, nil
}
func (backupHTTPFixture) Validate(context.Context, string, string, string) (backup.Validation, error) {
	return backup.Validation{SnapshotID: "snapshot-1"}, nil
}
func (backupHTTPFixture) Restore(context.Context, string, string, string, string) (backup.RestoreResult, error) {
	return backup.RestoreResult{PlanID: "plan-1", Status: "pending_confirmation", PreRestoreSnapshot: "/tmp/protected"}, nil
}
func (backupHTTPFixture) ConfirmRestore(context.Context, string) (backup.RestoreResult, error) {
	return backup.RestoreResult{PlanID: "plan-1", Status: "switched"}, nil
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
	if w := request(http.MethodPost, "/api/backup/restore", `{"targetId":"target-1","snapshotId":"snapshot-1","password":"`+password+`","destination":"/tmp/restore"}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pending_confirmation") || strings.Contains(w.Body.String(), password) {
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
