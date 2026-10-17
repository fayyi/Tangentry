package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/egoist/mygo"

	"tangentry/internal/app"
	"tangentry/internal/server"
	"tangentry/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Tangentry desktop failed", "error", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	// Keep idle backend heap retention bounded; model inference runs in a child process.
	debug.SetMemoryLimit(128 << 20)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.BindAs("DesktopCapture", desktopCapture{})
	logFile, err := openDesktopLog()
	if err != nil {
		return err
	}
	defer func() {
		if runErr != nil {
			slog.Error("Tangentry desktop failed", "error", runErr)
		}
		_ = logFile.Close()
	}()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	dataDir, err := configureDesktopRuntime()
	if err != nil {
		return err
	}
	application, err := app.New(dataDir, web.Assets())
	if err != nil {
		return err
	}
	runtime := application.RuntimePort()
	activity := application.ActivityPort()
	backend := server.New(runtime, application.ShellStatePort(), server.ShellOptions{
		PickPath:     pickPath,
		Capabilities: server.Capabilities{WindowCapture: true, Notifications: true, KeepAwake: true},
	})
	defer func() {
		if err := backend.Close(); err != nil {
			slog.Error("could not close application services", "error", err)
		}
	}()
	address, _, err := backend.StartLoopback()
	if err != nil {
		return err
	}
	openWindow := newDesktopWindows(address).Open
	activity.ObserveJobs(startJobActivity(openWindow).Observe)
	registerDesktopLifecycle(openWindow)
	mygo.App.WhenReady(openWindow)
	if err := mygo.App.Run(); err != nil {
		return fmt.Errorf("desktop event loop: %w", err)
	}
	return nil
}
