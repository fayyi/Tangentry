package tools

import (
	"context"
	"errors"
	"strconv"

	"tangentry/internal/playback"
)

// Only local files from the compatibility service are passed to FFmpeg.
func (t *Toolkit) ConvertPlayback(ctx context.Context, input, output string) error {
	if t.config.FFmpegPath == "" {
		return errors.New("FFmpeg unavailable")
	}
	scale := "scale=w='if(gte(iw,ih),min(1920,iw),min(1080,iw))':h='if(gte(iw,ih),min(1080,ih),min(1920,ih))':force_original_aspect_ratio=decrease:force_divisible_by=2"
	cmd := command(ctx, t.config.FFmpegPath, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file,pipe", "-threads", "2", "-i", input, "-map", "0:v:0", "-map", "0:a:0?",
		"-vf", scale, "-c:v", "libx264", "-preset", "veryfast", "-crf", "22", "-pix_fmt", "yuv420p",
		"-threads", "2", "-filter_threads", "1", "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart",
		"-fs", strconv.Itoa(playback.MaxCompatibilityOutput), "-f", "mp4", "-y", output)
	cmd.Env = toolEnvironment()
	buffer := &boundedBuffer{}
	cmd.Stdout, cmd.Stderr = buffer, buffer
	if cmd.Run() != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("video conversion failed")
	}
	return nil
}
