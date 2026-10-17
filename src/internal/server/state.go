package server

import "errors"

// ErrStateUnavailable means a shell did not wire persisted directory state.
// The HTTP backend remains usable for protocol-only requests in that case.
var ErrStateUnavailable = errors.New("server state unavailable")

// ErrRuntimeUnavailable means the backend was constructed without an
// application runtime. The constructor keeps its historical pointer return
// type for shell callers, so operations report this error instead of
// dereferencing a nil runtime.
var ErrRuntimeUnavailable = errors.New("server runtime unavailable")

// ErrBackendClosed reports an attempt to start a shell-owned listener after
// the application lifecycle has ended.
var ErrBackendClosed = errors.New("server backend closed")

// ErrLoopbackBusy reports that a desktop backend already owns a listener.
var ErrLoopbackBusy = errors.New("server loopback already running")
