package main

import (
	"log/slog"
	"net/url"

	"github.com/egoist/mygo"
)

type desktopWindows struct {
	address string
	origin  string
	onClose func()
}

func newDesktopWindows(address string, onClose ...func()) *desktopWindows {
	parsed, err := url.Parse(address)
	if err != nil {
		slog.Error("could not parse loopback address", "address", address, "error", err)
		return &desktopWindows{address: address, onClose: firstClose(onClose)}
	}
	return &desktopWindows{address: address, origin: parsed.Scheme + "://" + parsed.Host, onClose: firstClose(onClose)}
}

func firstClose(callbacks []func()) func() {
	if len(callbacks) == 0 {
		return nil
	}
	return callbacks[0]
}

func (w *desktopWindows) Open() {
	for _, window := range mygo.Windows() {
		window.Restore()
		window.Show()
		window.Focus()
		return
	}
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: "Tangentry", URL: w.address,
		Width: 1280, Height: 850, MinWidth: 900, MinHeight: 600,
		StateKey: "main", BackgroundColor: "#171717",
		Page: mygo.PageOptions{TrustedOrigins: []string{w.origin}, PreloadScript: `document.addEventListener('DOMContentLoaded', () => document.documentElement.classList.add('tangentry-desktop'), {once:true});`},
	})
	window.Page().OnPageTitleUpdated(func(event *mygo.TitleEvent) {
		event.PreventDefault()
	})
	window.Page().OnWillNavigate(func(event *mygo.NavigateEvent) {
		decision := decideNavigation(event.URL, w.origin, event.UserInitiated)
		if decision.allow {
			return
		}
		event.PreventDefault()
		if decision.openExternally {
			go mygo.Shell.OpenExternal(event.URL)
		}
	})
	window.Page().OnDidFailLoad(func(err *mygo.LoadError) {
		slog.Error("page failed to load", "error", err)
	})
	window.Page().OnRenderProcessGone(func(reason string) {
		slog.Warn("webview stopped; reloading", "reason", reason)
		window.Page().Reload()
	})
	if w.onClose != nil {
		window.OnClosed(w.onClose)
	}
}
