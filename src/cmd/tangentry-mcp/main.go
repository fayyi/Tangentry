// Command tangentry-mcp is the stdio MCP bridge for a user-enabled local
// Tangentry Agent Gateway. The bearer token is read from a 0600 connection
// file and is never accepted as a command-line argument.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"tangentry/internal/agent/gateway"
	"tangentry/internal/agent/mcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tangentry-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultFile := os.Getenv("TANGENTRY_AGENT_CONNECTION_FILE")
	if defaultFile == "" {
		if dataDir := os.Getenv("TANGENTRY_DATA"); dataDir != "" {
			defaultFile = filepath.Join(dataDir, "agent", "connection.json")
		}
	}
	connectionFile := flag.String("connection-file", defaultFile, "0600 Tangentry Agent connection file (or TANGENTRY_AGENT_CONNECTION_FILE)")
	flag.Parse()
	if *connectionFile == "" {
		return errors.New("connection file is required")
	}
	connection, err := gateway.ReadConnection(*connectionFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return mcp.New(connection).Serve(ctx, os.Stdin, os.Stdout)
}
