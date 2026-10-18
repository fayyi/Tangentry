// Package server assembles the application's HTTP surface for both entry
// points: the desktop app's per-launch loopback session and the headless
// server's password session. Each gate checks the request's own Host and
// Origin, then hands the application a request normalized to localhost.
package server

import (
	"context"
	"net/http"
	"sync"
)

// Capabilities lists the features only a desktop shell provides. The web UI
// reads them from GET /api/capabilities and hides controls that need them.
type Capabilities struct {
	PathPicker           bool `json:"pathPicker"`
	WindowCapture        bool `json:"windowCapture"`
	Notifications        bool `json:"notifications"`
	KeepAwake            bool `json:"keepAwake"`
	Updater              bool `json:"updater"`
	Server               bool `json:"server"`
	PlatformWebViewLogin bool `json:"platformWebViewLogin"`
}

// Runtime is the shell-neutral application port. The server only needs the
// HTTP handler, lifecycle, and job observation. It deliberately does not
// expose directories, model settings, or any concrete feature service.
type Runtime interface {
	HTTPHandler() http.Handler
	Close() error
}

// ShellState exposes persisted paths that are shared by the HTTP protocol and
// an optional desktop shell. Keeping it separate from Runtime means a
// headless caller can serve the protocol without implementing native-directory
// details, and tests can provide only the state they exercise.
type ShellState interface {
	DownloadDirectory() string
	DownloadDirectorySelected() bool
	SetDownloadDirectory(string) error
	ModelDirectory() string
	SetModelDirectory(context.Context, string) error
}

// ShellOptions describes capabilities supplied by a native shell. A
// headless server uses the zero value. Keeping these options immutable after
// construction makes the backend safe to reason about while requests run.
type ShellOptions struct {
	PickPath      func(context.Context, string, string) (string, error)
	PlatformLogin func(context.Context, string) error
	Capabilities  Capabilities
}

type Backend struct {
	runtime    Runtime
	state      ShellState
	handler    http.Handler
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
	requests   sync.WaitGroup
	loopbackMu sync.Mutex
	loopback   *http.Server
	shell      ShellOptions
	dialogMu   sync.Mutex
}

// New makes both shell-neutral ports explicit at the composition boundary.
// A headless caller can pass nil state and still serve the protocol surface;
// directory-specific routes then return a clear unavailable response.
func New(runtime Runtime, state ShellState, shell ShellOptions) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrRuntimeUnavailable.Error(), http.StatusServiceUnavailable)
	}))
	if runtime != nil {
		if applicationHandler := runtime.HTTPHandler(); applicationHandler != nil {
			handler = applicationHandler
		}
	}
	return &Backend{runtime: runtime, state: state, handler: handler, ctx: ctx, cancel: cancel, shell: shell, closeDone: make(chan struct{})}
}

func (b *Backend) Capabilities() Capabilities {
	capabilities := b.shell.Capabilities
	capabilities.PathPicker = b.shell.PickPath != nil
	capabilities.PlatformWebViewLogin = b.shell.PlatformLogin != nil
	return capabilities
}

// UseDownloadDirectory keeps a directory the user already chose; otherwise it
// selects directory, which headless deployments need since they have no picker.
func (b *Backend) UseDownloadDirectory(directory string) error {
	if b.state == nil {
		return ErrStateUnavailable
	}
	if b.state.DownloadDirectorySelected() {
		return nil
	}
	return b.state.SetDownloadDirectory(directory)
}

// ServeHTTP expects a request that a gate already admitted and normalized.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		http.Error(w, "application is closing", http.StatusServiceUnavailable)
		return
	}
	b.requests.Add(1)
	b.mu.Unlock()
	defer b.requests.Done()

	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(b.ctx, cancel)
	defer stop()
	defer cancel()
	b.route(w, r.WithContext(ctx))
}

func (b *Backend) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.cancel()
		b.mu.Unlock()
		b.closeLoopback()
		b.requests.Wait()
		if b.runtime == nil {
			b.closeErr = ErrRuntimeUnavailable
		} else {
			b.closeErr = b.runtime.Close()
		}
		close(b.closeDone)
	})
	<-b.closeDone
	return b.closeErr
}

func (b *Backend) closeLoopback() {
	b.loopbackMu.Lock()
	server := b.loopback
	b.loopback = nil
	b.loopbackMu.Unlock()
	if server != nil {
		_ = server.Close()
	}
}
