package app

import (
	"context"
	"net/http"

	"tangentry/internal/jobs"
)

// RuntimePort is the lifecycle view exposed to a shell. It intentionally
// forwards only the runtime operations needed by internal/server.
type RuntimePort struct{ runtime *runtimeState }

func (p RuntimePort) HTTPHandler() http.Handler {
	return p.runtime.HTTPHandler()
}

func (p RuntimePort) Close() error { return p.runtime.Close() }

// ActivityPort is the optional desktop-only view of long-running jobs.
// Headless servers never need to construct or pass this port.
type ActivityPort struct{ runtime *runtimeState }

func (p ActivityPort) ObserveJobs(observe func(jobs.Event)) {
	p.runtime.observeJobs(observe)
}

// ShellStatePort is the persisted directory view exposed to a shell. Keeping it
// separate prevents callers from receiving the feature services held by the
// composition root.
type ShellStatePort struct{ state *shellState }

func (p ShellStatePort) DownloadDirectory() string {
	return p.state.DownloadDirectory()
}

func (p ShellStatePort) DownloadDirectorySelected() bool {
	return p.state.DownloadDirectorySelected()
}

func (p ShellStatePort) SetDownloadDirectory(directory string) error {
	return p.state.SetDownloadDirectory(directory)
}

func (p ShellStatePort) ModelDirectory() string { return p.state.ModelDirectory() }

func (p ShellStatePort) SetModelDirectory(ctx context.Context, selected string) error {
	return p.state.SetModelDirectory(ctx, selected)
}

// RuntimePort returns the shell-neutral lifecycle view of the application.
func (a *Application) RuntimePort() RuntimePort { return RuntimePort{runtime: a.runtime} }

// ActivityPort returns the optional desktop activity view.
func (a *Application) ActivityPort() ActivityPort { return ActivityPort{runtime: a.runtime} }

// ShellStatePort returns the optional persisted directory view of the
// application. The returned value contains no feature-service methods.
func (a *Application) ShellStatePort() ShellStatePort { return ShellStatePort{state: a.shell} }
