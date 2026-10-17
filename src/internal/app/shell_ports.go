package app

import (
	"context"
	"net/http"

	"tangentry/internal/credentials"
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

// PlatformCredentialState is the shell-facing, non-secret view of a platform
// credential. Cookie values and browser User-Agent strings are intentionally
// absent so native shells can render status without receiving credentials.
type PlatformCredentialState struct {
	Platform   string
	Configured bool
	Source     string
	UpdatedAt  int64
	ExpiresAt  int64
}

// PlatformCredentialsPort is the only credential surface exposed to a native
// shell. The shell uses PrepareBrowserLogin to commit cookies collected by its
// private WebView; HTTP never receives those values.
type PlatformCredentialsPort struct{ store *credentials.Store }

func (p PlatformCredentialsPort) States() []PlatformCredentialState {
	if p.store == nil {
		return nil
	}
	states := p.store.States()
	out := make([]PlatformCredentialState, 0, len(states))
	for _, state := range states {
		out = append(out, PlatformCredentialState{
			Platform: state.Platform, Configured: state.Configured,
			Source: state.Source, UpdatedAt: state.UpdatedAt, ExpiresAt: state.ExpiresAt,
		})
	}
	return out
}

func (p PlatformCredentialsPort) Clear(platform string) error {
	if p.store == nil {
		return credentials.ErrStorage
	}
	return p.store.Clear(platform)
}

// PrepareBrowserLogin returns a revision-bound saver for an in-app WebView.
// Only the platform-specific cookie allowlist in credentials is persisted.
func (p PlatformCredentialsPort) PrepareBrowserLogin(platform string) func([]*http.Cookie, string) error {
	if p.store == nil {
		return func([]*http.Cookie, string) error { return credentials.ErrStorage }
	}
	switch platform {
	case "linuxdo":
		return p.store.PrepareLinuxdoBrowserLogin()
	case "douyin":
		return p.store.PrepareDouyinBrowserLogin()
	default:
		return func([]*http.Cookie, string) error { return credentials.ErrInvalid }
	}
}

// PlatformCredentialsPort returns the native-shell credential bridge. It does
// not expose the concrete store or any cookie values to server packages.
func (a *Application) PlatformCredentialsPort() PlatformCredentialsPort {
	if a == nil || a.runtime == nil {
		return PlatformCredentialsPort{}
	}
	return PlatformCredentialsPort{store: a.runtime.cookies}
}
