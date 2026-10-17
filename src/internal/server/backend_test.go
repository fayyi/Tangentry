package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"tangentry/internal/app"
)

type countingRuntime struct {
	handler http.Handler
	closes  atomic.Int32
}

func (r *countingRuntime) HTTPHandler() http.Handler { return r.handler }
func (r *countingRuntime) Close() error {
	r.closes.Add(1)
	return nil
}

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	t.Setenv("TANGENTRY_YTDLP_PATH", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("TANGENTRY_FFMPEG_PATH", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("TANGENTRY_BILIBILI_COOKIE_FILE", "")
	application, err := app.New(t.TempDir(), fstest.MapFS{"index.html": {Data: []byte("app")}})
	if err != nil {
		t.Fatal(err)
	}
	return New(application.RuntimePort(), application.ShellStatePort(), ShellOptions{})
}

func TestCapabilitiesReportOnlyWiredDesktopFeatures(t *testing.T) {
	headless := newTestBackend(t)
	defer headless.Close()
	read := func(backend *Backend) Capabilities {
		response := httptest.NewRecorder()
		backend.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/api/capabilities", nil))
		var capabilities Capabilities
		if response.Code != 200 || json.NewDecoder(response.Body).Decode(&capabilities) != nil {
			t.Fatalf("capabilities unavailable: %d", response.Code)
		}
		return capabilities
	}
	if read(headless) != (Capabilities{}) {
		t.Fatal("headless backend advertised desktop features")
	}
	application, err := app.New(t.TempDir(), fstest.MapFS{"index.html": {Data: []byte("app")}})
	if err != nil {
		t.Fatal(err)
	}
	desktop := New(application.RuntimePort(), application.ShellStatePort(), ShellOptions{
		PickPath:     func(context.Context, string, string) (string, error) { return "", nil },
		Capabilities: Capabilities{WindowCapture: true, KeepAwake: true},
	})
	defer desktop.Close()
	if got := read(desktop); got != (Capabilities{PathPicker: true, WindowCapture: true, KeepAwake: true}) {
		t.Fatalf("capabilities = %+v", got)
	}
}

func TestHeadlessPathPickerIsUnavailable(t *testing.T) {
	backend := newTestBackend(t)
	defer backend.Close()
	response := httptest.NewRecorder()
	backend.ServeHTTP(response, httptest.NewRequest("POST", "http://localhost/api/desktop/download-directory", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	directory := t.TempDir()
	if err := backend.UseDownloadDirectory(directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.UseDownloadDirectory(t.TempDir()); err != nil || backend.state.DownloadDirectory() != directory {
		t.Fatal("chosen download directory was replaced", err)
	}
}

func TestBackendReportsUnavailableStateWithoutShellDirectoryPort(t *testing.T) {
	backend := newTestBackend(t)
	defer backend.Close()
	backend.state = nil
	response := httptest.NewRecorder()
	backend.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/api/desktop/download-directory", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if err := backend.UseDownloadDirectory(t.TempDir()); err != ErrStateUnavailable {
		t.Fatalf("UseDownloadDirectory error = %v, want %v", err, ErrStateUnavailable)
	}
}

func TestBackendCloseCancelsRequestsAndRejectsNewWork(t *testing.T) {
	backend := newTestBackend(t)
	started := make(chan struct{})
	requestDone := make(chan struct{})
	backend.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	go func() {
		defer close(requestDone)
		backend.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://localhost/api/health", nil))
	}()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- backend.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel and drain the request")
	}
	<-requestDone
	response := httptest.NewRecorder()
	backend.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/api/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed backend accepted work: %d", response.Code)
	}
}

func TestBackendCloseIsIdempotent(t *testing.T) {
	runtime := &countingRuntime{handler: http.NotFoundHandler()}
	backend := New(runtime, nil, ShellOptions{})
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if got := runtime.closes.Load(); got != 1 {
		t.Fatalf("runtime close calls = %d, want 1", got)
	}
}

func TestBackendWithoutRuntimeFailsExplicitly(t *testing.T) {
	backend := New(nil, nil, ShellOptions{})
	response := httptest.NewRecorder()
	backend.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/api/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	if _, _, err := backend.StartLoopback(); err != ErrRuntimeUnavailable {
		t.Fatalf("StartLoopback error = %v, want %v", err, ErrRuntimeUnavailable)
	}
	if err := backend.Close(); err != ErrRuntimeUnavailable {
		t.Fatalf("Close error = %v, want %v", err, ErrRuntimeUnavailable)
	}
	if err := backend.Close(); err != ErrRuntimeUnavailable {
		t.Fatalf("second Close error = %v, want %v", err, ErrRuntimeUnavailable)
	}
}
