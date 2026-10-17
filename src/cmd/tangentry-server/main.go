// Command tangentry-server runs Tangentry without a desktop shell, for Docker
// and other always-on hosts. Browsers sign in with the server password.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tangentry/internal/app"
	"tangentry/internal/server"
	"tangentry/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Tangentry server failed", "error", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func run() error {
	addr := flag.String("addr", env("TANGENTRY_ADDR", ":7380"), "listen address (TANGENTRY_ADDR)")
	dataDir := flag.String("data", env("TANGENTRY_DATA", "/data"), "data directory (TANGENTRY_DATA)")
	health := flag.Bool("health", false, "check that the server at -addr answers, then exit")
	flag.Parse()
	if *health {
		return checkHealth(*addr)
	}
	data, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		return err
	}
	password, generated, err := server.LoadPassword(data, os.Getenv("TANGENTRY_PASSWORD"))
	if err != nil {
		return err
	}
	if generated {
		slog.Info("generated a sign-in password; set TANGENTRY_PASSWORD to choose your own", "password", password, "stored", filepath.Join(data, "admin-password"))
	}
	application, err := app.New(data, web.Assets())
	if err != nil {
		return err
	}
	backend := server.New(application.RuntimePort(), application.ShellStatePort(), server.ShellOptions{})
	defer func() {
		if err := backend.Close(); err != nil {
			slog.Error("could not close application services", "error", err)
		}
	}()
	downloads := env("TANGENTRY_DOWNLOAD_DIR", filepath.Join(data, "downloads"))
	if err := os.MkdirAll(downloads, 0700); err != nil {
		return err
	}
	if err := backend.UseDownloadDirectory(downloads); err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr: *addr, Handler: server.NewPasswordGate(backend, password),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			httpServer.Close()
		}
	}()
	slog.Info("Tangentry server listening", "addr", *addr, "data", data)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-drained
	return nil
}

// checkHealth backs the container HEALTHCHECK, which has no curl to call.
func checkHealth(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || net.ParseIP(host) != nil && net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("health check returned %s", response.Status)
	}
	return nil
}
