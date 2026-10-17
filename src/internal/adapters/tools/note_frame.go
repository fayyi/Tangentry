package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"tangentry/internal/notes"
)

func (t *Toolkit) CaptureNoteFrame(ctx context.Context, input string, seconds float64) ([]byte, error) {
	if t.config.FFmpegPath == "" {
		return nil, notes.ErrFrameUnavailable
	}
	directory, err := os.MkdirTemp("", "tangentry-note-frame-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	output := filepath.Join(directory, "frame.jpg")
	cmd := command(ctx, t.config.FFmpegPath, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpegts,flv",
		"-ss", strconv.FormatFloat(seconds, 'f', 3, 64), "-i", input,
		"-map", "0:v:0", "-frames:v", "1", "-an",
		"-vf", "scale=w='min(1080,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease",
		"-threads", "1", "-filter_threads", "1", "-q:v", "4", "-fs", strconv.Itoa(notes.MaxFrameBytes), "-y", output)
	cmd.Env = toolEnvironment()
	buffer := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = buffer, buffer
	if cmd.Run() != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, notes.ErrFrameUnavailable
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 || info.Size() > notes.MaxFrameBytes {
		return nil, notes.ErrFrameUnavailable
	}
	return os.ReadFile(output)
}
