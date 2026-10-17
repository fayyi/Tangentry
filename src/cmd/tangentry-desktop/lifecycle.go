package main

import (
	"runtime"

	"github.com/egoist/mygo"
)

func keepRunningWithoutWindows(goos string) bool { return goos == "darwin" }

func registerDesktopLifecycle(openWindow func()) {
	// Notifications already seen in the app need not wait in Notification Center.
	mygo.App.OnDidBecomeActive(mygo.ClearNotifications)
	mygo.App.OnNotificationClick(func(string) { openWindow() })
	mygo.App.OnWindowAllClosed(func() {
		if !keepRunningWithoutWindows(runtime.GOOS) {
			mygo.App.Quit()
		}
	})
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			openWindow()
		}
	})
	mygo.App.OnSecondInstance(func([]string, string) { openWindow() })
}
