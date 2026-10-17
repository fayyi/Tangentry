package main

import (
	"crypto/rand"
	"errors"
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
	credentialKey, err := desktopCredentialKey()
	if err != nil {
		return err
	}
	application, err := app.New(dataDir, web.Assets(), app.Options{CredentialKey: credentialKey})
	if err != nil {
		return err
	}
	platformLogin := newPlatformLoginBridge(application.PlatformCredentialsPort())
	mygo.BindAs("PlatformLogin", platformLoginService{bridge: platformLogin})
	runtime := application.RuntimePort()
	activity := application.ActivityPort()
	backend := server.New(runtime, application.ShellStatePort(), server.ShellOptions{
		PickPath:      pickPath,
		PlatformLogin: platformLogin.Open,
		Capabilities:  server.Capabilities{WindowCapture: true, Notifications: true, KeepAwake: true},
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

func desktopCredentialKey() ([]byte, error) {
	const keyName = "tangentry-credential-key"
	key, err := mygo.Secrets.Get(keyName)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("桌面凭据密钥长度无效")
		}
		return key, nil
	}
	if !errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, fmt.Errorf("无法访问系统凭据存储：%w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("无法生成桌面凭据密钥：%w", err)
	}
	if err := mygo.Secrets.Set(keyName, key); err != nil {
		return nil, fmt.Errorf("无法保存系统凭据密钥：%w", err)
	}
	return key, nil
}
