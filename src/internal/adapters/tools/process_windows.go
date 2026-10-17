package tools

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// createNoWindow stops the GUI app from flashing a console for every helper
// process (ffmpeg, whisper-cli, yt-dlp, taskkill).
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func command(ctx context.Context, executable string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, executable, args...)
	hideWindow(cmd)
	cmd.Cancel = func() error {
		killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		kill := exec.CommandContext(killCtx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideWindow(kill)
		_ = kill.Run()
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 3 * time.Second
	return cmd
}
