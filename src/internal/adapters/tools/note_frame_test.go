package tools

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"path/filepath"
	"testing"
	"time"

	"tangentry/internal/config"
	"tangentry/internal/notes"
)

func TestNoteFrameCapturesRequestedSceneWithinImageLimits(t *testing.T) {
	ffmpeg := executable("", "ffmpeg")
	if ffmpeg == "" {
		t.Skip("FFmpeg unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	video := filepath.Join(t.TempDir(), "two-scenes.mp4")
	cmd := command(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=1920x1200:r=10:d=1", "-f", "lavfi", "-i", "color=c=blue:s=1920x1200:r=10:d=1", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-y", video)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	toolkit := New(config.Config{FFmpegPath: ffmpeg})
	for _, at := range []float64{0.2, 1.2} {
		data, err := toolkit.CaptureNoteFrame(ctx, video, at)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if frame.Bounds().Dx() != 1080 || frame.Bounds().Dy() != 675 || len(data) > notes.MaxFrameBytes {
			t.Fatal("image limits", frame.Bounds(), len(data))
		}
		red, _, blue, _ := frame.At(540, 337).RGBA()
		if at < 1 && red < blue*3 || at > 1 && blue < red*3 {
			t.Fatal("wrong scene at", at, red, blue)
		}
	}
	if _, err := toolkit.CaptureNoteFrame(ctx, video, 10); !errors.Is(err, notes.ErrFrameUnavailable) {
		t.Fatal("out-of-range frame accepted", err)
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	if _, err := toolkit.CaptureNoteFrame(stopped, video, .2); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled capture", err)
	}
	audio := filepath.Join(t.TempDir(), "audio.wav")
	if output, err := command(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=1", "-y", audio).CombinedOutput(); err != nil {
		t.Fatalf("audio fixture: %v %s", err, output)
	}
	if _, err := toolkit.CaptureNoteFrame(ctx, audio, .2); !errors.Is(err, notes.ErrFrameUnavailable) {
		t.Fatal("audio-only file accepted", err)
	}
}
