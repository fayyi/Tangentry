package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

// ProbePlayback reports only the codec/container fields used by the
// compatibility service. ffprobe is optional; a missing probe simply causes a
// conservative conversion.
func (t *Toolkit) ProbePlayback(ctx context.Context, input string) (playback.PlaybackFormat, error) {
	ffprobe := executable(filepath.Join(filepath.Dir(t.config.FFmpegPath), "ffprobe"), "")
	if ffprobe == "" {
		var err error
		ffprobe, err = exec.LookPath("ffprobe")
		if err != nil {
			return playback.PlaybackFormat{}, err
		}
	}
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=format_name:stream=codec_type,codec_name", "-of", "json", input)
	cmd.Env = toolEnvironment()
	data, err := cmd.Output()
	if err != nil {
		return playback.PlaybackFormat{}, err
	}
	var report struct {
		Streams []struct {
			Type  string `json:"codec_type"`
			Codec string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			Name string `json:"format_name"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return playback.PlaybackFormat{}, err
	}
	result := playback.PlaybackFormat{Container: report.Format.Name}
	for _, stream := range report.Streams {
		switch strings.ToLower(stream.Type) {
		case "video":
			result.VideoCodec = stream.Codec
		case "audio":
			result.AudioCodec = stream.Codec
		}
	}
	return result, nil
}
