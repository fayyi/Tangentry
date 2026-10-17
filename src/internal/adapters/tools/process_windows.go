package tools

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func command(ctx context.Context, executable string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Cancel = func() error {
		killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = exec.CommandContext(killCtx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 3 * time.Second
	return cmd
}
