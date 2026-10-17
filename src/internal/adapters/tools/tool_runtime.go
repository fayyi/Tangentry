package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tangentry/internal/downloads"
)

// lookupRuntime finds a JavaScript runtime the system already provides.
var lookupRuntime = func(name string) string { return executable("", name) }

func (t *Toolkit) javaScriptRuntime(ctx context.Context) string {
	for _, name := range []string{"deno", "node"} {
		if path := lookupRuntime(name); path != "" {
			return name + ":" + path
		}
	}
	path, err := t.deno.ensure(ctx)
	if err != nil {
		return ""
	}
	return "deno:" + path
}

func executable(configured, fallback string) string {
	if configured == "" {
		configured = fallback
		if path, err := exec.LookPath(fallback); err == nil {
			configured = path
		} else {
			// GUI background services often omit package-manager paths.
			for _, root := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
				candidate := filepath.Join(root, fallback)
				if path, err := exec.LookPath(candidate); err == nil {
					configured = path
					break
				}
			}
		}
	}
	name, err := exec.LookPath(configured)
	if err != nil {
		return ""
	}
	name, err = filepath.Abs(name)
	if err != nil {
		return ""
	}
	return name
}

func probe(name, executable, flag string) downloads.ToolStatus {
	status := downloads.ToolStatus{Name: name}
	if executable == "" {
		return status
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := command(ctx, executable, flag)
	cmd.Env = toolEnvironment()
	output := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	if cmd.Run() != nil {
		return status
	}
	status.Available = true
	status.Version = strings.Split(strings.TrimSpace(output.String()), "\n")[0]
	return status
}

func toolEnvironment() []string {
	env := make([]string, 0)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TANGENTRY_") {
			env = append(env, value)
		}
	}
	return env
}
