package app

import (
	"context"
	"errors"
	"net/http"

	"tangentry/internal/credentials"
	"tangentry/internal/jobs"
)

// RuntimePort is the lifecycle view exposed to a shell. It intentionally
// forwards only the runtime operations needed by internal/server. It retains
// the application, rather than one runtime instance, so a confirmed restore
// that rebuilds services is visible to an already-running server backend.
type RuntimePort struct{ application *Application }

func (p RuntimePort) HTTPHandler() http.Handler {
	if p.application == nil {
		return nil
	}
	return p.application.HTTPHandler()
}

func (p RuntimePort) Close() error {
	if p.application == nil {
		return nil
	}
	return p.application.Close()
}

// ActivityPort is the optional desktop-only view of long-running jobs.
// Headless servers never need to construct or pass this port.
type ActivityPort struct{ runtime *runtimeState }

func (p ActivityPort) ObserveJobs(observe func(jobs.Event)) {
	p.runtime.observeJobs(observe)
}

// ShellStatePort is the persisted directory view exposed to a shell. Keeping it
// separate prevents callers from receiving the feature services held by the
// composition root.
type ShellStatePort struct{ application *Application }

func (p ShellStatePort) DownloadDirectory() string {
	if p.application == nil || p.application.shell == nil {
		return ""
	}
	return p.application.shell.DownloadDirectory()
}

func (p ShellStatePort) DownloadDirectorySelected() bool {
	return p.application != nil && p.application.shell != nil && p.application.shell.DownloadDirectorySelected()
}

func (p ShellStatePort) SetDownloadDirectory(directory string) error {
	if p.application == nil || p.application.shell == nil {
		return errors.New("application shell unavailable")
	}
	return p.application.shell.SetDownloadDirectory(directory)
}

func (p ShellStatePort) ModelDirectory() string {
	if p.application == nil || p.application.shell == nil {
		return ""
	}
	return p.application.shell.ModelDirectory()
}

func (p ShellStatePort) SetModelDirectory(ctx context.Context, selected string) error {
	if p.application == nil || p.application.shell == nil {
		return errors.New("application shell unavailable")
	}
	return p.application.shell.SetModelDirectory(ctx, selected)
}

// RuntimePort returns the shell-neutral lifecycle view of the application.
func (a *Application) RuntimePort() RuntimePort { return RuntimePort{application: a} }

// ActivityPort returns the optional desktop activity view.
func (a *Application) ActivityPort() ActivityPort { return ActivityPort{runtime: a.runtime} }

// ShellStatePort returns the optional persisted directory view of the
// application. The returned value contains no feature-service methods.
func (a *Application) ShellStatePort() ShellStatePort { return ShellStatePort{application: a} }

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
	case "bilibili":
		return p.store.PrepareBilibiliBrowserLogin()
	case "linuxdo":
		return p.store.PrepareLinuxdoBrowserLogin()
	case "douyin":
		return p.store.PrepareDouyinBrowserLogin()
	case "xiaohongshu":
		return p.store.PrepareXiaohongshuBrowserLogin()
	case "youtube":
		return p.store.PrepareYouTubeBrowserLogin()
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
