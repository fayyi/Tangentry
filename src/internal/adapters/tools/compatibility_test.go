package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"tangentry/internal/config"
)

func TestCompatibilityFFmpegProducesH264WithAudioAndOriginalTimeline(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	ffprobe, err := exec.LookPath("ffprobe")
	if ffmpeg == "" || err != nil {
		t.Skip("real FFmpeg and ffprobe required for codec integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	input, output := filepath.Join(t.TempDir(), "hevc.mp4"), filepath.Join(t.TempDir(), "h264.mp4")
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=128x96:rate=12", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "1", "-c:v", "libx265", "-x265-params", "pools=1:frame-threads=1", "-c:a", "aac", "-y", input)
	if cmd.Run() != nil {
		t.Fatal("could not create explicit HEVC codec fixture")
	}
	tk := New(config.Config{FFmpegPath: ffmpeg})
	if err := tk.ConvertPlayback(ctx, input, output); err != nil {
		t.Fatal(err)
	}
	data, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,width,height:format=duration", "-of", "json", output).Output()
	if err != nil {
		t.Fatal("converted file cannot be inspected")
	}
	var report struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		Format struct{ Duration string }
	}
	if json.Unmarshal(data, &report) != nil || len(report.Streams) != 2 ||
		report.Streams[0].Codec != "h264" || report.Streams[0].Width != 128 || report.Streams[0].Height != 96 ||
		report.Streams[1].Codec != "aac" || report.Format.Duration != "1.000000" {
		t.Fatalf("wrong converted representation: %s", data)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatal("empty converted video")
	}
}
