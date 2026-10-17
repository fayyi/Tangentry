package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/config"
)

func TestErrorsNeverExposeSensitiveToolOutput(t *testing.T) {
	for _, text := range []string{"ERROR 412 Cookie: session=private-value", "login required https://user:private-value@example.com", "unknown failure private-value"} {
		message := classify(text).Error()
		if strings.Contains(message, "private-value") || strings.Contains(message, "example.com") {
			t.Fatal("sensitive tool output was exposed")
		}
	}
}

func TestMissingToolsRemainUnavailable(t *testing.T) {
	tk := New(config.Config{YTDLPPath: filepath.Join(t.TempDir(), "missing-yt-dlp"), FFmpegPath: filepath.Join(t.TempDir(), "missing-ffmpeg")})
	if tk.Ready() || tk.Status().Tools[0].Available {
		t.Fatal("missing binaries reported ready")
	}
}

func TestMetadataMappingDoesNotNeedFFmpegOrExposeExtractorPayload(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	path := filepath.Join(t.TempDir(), "metadata-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo test-fixture; exit 0; fi\nprintf '%s\\n' '{\"id\":\"BV17x411w7KC_p1\",\"title\":\"Metadata fixture\",\"uploader\":\"Test author\",\"uploader_id\":\"12345\",\"duration\":62.5,\"upload_date\":\"20241001\",\"formats\":[{\"private\":\"should-never-be-forwarded\"}]}'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	tk := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(t.TempDir(), "missing")})
	if tk.Ready() {
		t.Fatal("download unexpectedly ready")
	}
	item, err := tk.Video(context.Background(), "BV17x411w7KC")
	if err != nil || item.ID != "bilibili:BV17x411w7KC" || item.AuthorID != "12345" || item.DurationSeconds != 62 || item.PublishedAt == 0 {
		t.Fatalf("bad metadata mapping: %+v %v", item, err)
	}
	if _, err := tk.Video(context.Background(), "http://127.0.0.1/private"); err == nil {
		t.Fatal("untrusted metadata URL accepted")
	}
}

func TestPlaybackMappingAcceptsOnlyOneTrustedHTTPSRepresentation(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX process fixture")
	}
	path := filepath.Join(t.TempDir(), "playback-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo test-fixture; exit 0; fi\nprintf '%s\\n' '{\"id\":\"BV17x411w7KC\",\"url\":\"https://x.bilivideo.com/video.mp4\",\"protocol\":\"https\",\"format\":\"mp4\",\"height\":720,\"duration\":17}'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	tk := New(config.Config{YTDLPPath: path, FFmpegPath: filepath.Join(t.TempDir(), "missing")})
	source, err := tk.Playback(context.Background(), "BV17x411w7KC")
	if err != nil || source.URL != "https://x.bilivideo.com/video.mp4" || source.Quality != 720 || source.Duration != 17 {
		t.Fatalf("bad playback mapping: %+v %v", source, err)
	}
}

// Opt-in integration: real FFmpeg -> local HLS -> real yt-dlp -> real ffprobe.
// It does not request Bilibili, use account cookies, or leave fixture files in the app.
func TestRealMediaToolchain(t *testing.T) {
	if os.Getenv("TANGENTRY_TOOL_SMOKE") != "1" {
		t.Skip("set TANGENTRY_TOOL_SMOKE=1 for installed-tool integration")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.MkdirAll(source, 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := command(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=160x90:r=5", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-f", "hls", "-hls_time", "1", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(source, "segment-%03d.ts"), filepath.Join(source, "video.m3u8"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(source, "master.m3u8"), []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000,RESOLUTION=160x90\nvideo.m3u8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(source)))
	defer server.Close()
	tk := New(config.Config{})
	metadata, err := tk.Inspect(ctx, server.URL+"/master.m3u8")
	if err != nil || len(metadata.Formats) == 0 {
		t.Fatal("real HLS inspection failed", metadata, err)
	}
	output := filepath.Join(root, "output")
	name, err := tk.Download(ctx, server.URL+"/master.m3u8", output, 1080)
	if err != nil {
		t.Fatal(err)
	}
	media, err := command(ctx, ffprobe, "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", filepath.Join(output, name)).CombinedOutput()
	if err != nil || !strings.Contains(string(media), "video") || !strings.Contains(string(media), "audio") {
		t.Fatalf("invalid downloaded media: %v %s", err, media)
	}
}
