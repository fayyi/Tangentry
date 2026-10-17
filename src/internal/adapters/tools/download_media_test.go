package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tangentry/internal/config"
	"tangentry/internal/downloads"
)

func TestDownloadMetadataOnlyExposesChoicesAndRejectsPlaylists(t *testing.T) {
	raw := []byte(`{"title":"标题测试","uploader":"作者","thumbnail":"https://example.com/cover.jpg","formats":[
		{"format_id":"137","ext":"mp4","height":1080,"vcodec":"avc1","acodec":"none","url":"https://secret.invalid/token","http_headers":{"Cookie":"private"}},
		{"format_id":"140","ext":"m4a","vcodec":"none","acodec":"aac","filesize_approx":1234},
		{"format_id":"drm","vcodec":"avc1","acodec":"none","has_drm":true},
		{"format_id":"storyboard","vcodec":"none","acodec":"none"}],
		"subtitles":{"en":[{"name":"English"}],"live_chat":[{"name":"chat"}]},
		"automatic_captions":{"zh-Hans":[{"name":"中文"}]}}`)
	info, err := parseDownloadMetadata(raw, "https://example.com/video")
	if err != nil || len(info.Formats) != 2 || len(info.Subtitles) != 2 || !info.Formats[1].Approximate || !info.Subtitles[1].Automatic {
		t.Fatal(info, err)
	}
	mapped, _ := json.Marshal(info)
	if strings.Contains(string(mapped), "secret") || strings.Contains(string(mapped), "Cookie") {
		t.Fatal("extractor secrets leaked")
	}
	for _, raw := range []string{`{"_type":"playlist","title":"playlist"}`, `{"title":"empty","formats":[]}`, `{"title":"bad","duration":-1}`} {
		if _, err := parseDownloadMetadata([]byte(raw), "https://example.com/video"); err == nil {
			t.Fatal("invalid metadata accepted", raw)
		}
	}
}

func TestDownloadArgumentsUseTitleContainerAndLiteralChoices(t *testing.T) {
	tk := &Toolkit{config: config.Config{FFmpegPath: "/test/ffmpeg"}}
	cases := []struct {
		options downloads.Options
		want    []string
	}{
		{downloads.Options{Container: "mkv", VideoFormatID: "137", AudioFormatID: "140", SubtitleLanguages: []string{"zh-Hans"}, AutomaticSubtitles: true}, []string{"137+140/137", "--remux-video mkv", "--merge-output-format mkv", "^zh-Hans$", "--write-auto-subs"}},
		{downloads.Options{Mode: "audio", AudioFormat: "mp3"}, []string{"ba/b", "--extract-audio", "--audio-format mp3"}},
		{downloads.Options{VideoOnly: true}, []string{"bv[height<=?720]/b[height<=?720]", "--remux-video mp4"}},
	}
	for _, tc := range cases {
		args := tk.mediaArguments("https://example.com/media", "/test/output", 720, tc.options)
		text := strings.Join(args, " ")
		if !strings.Contains(text, "%(title).180B.%(ext)s") || strings.Contains(text, "--restrict-filenames") {
			t.Fatal("title naming missing", text)
		}
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q in %s", want, text)
			}
		}
	}
}

func TestRealVideoOptimizationKeepsPlayableMediaAndSmallerOriginal(t *testing.T) {
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
	tk := &Toolkit{config: config.Config{FFmpegPath: ffmpeg}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, tc := range []struct {
		name             string
		compress, silent bool
		crf              string
	}{
		{"compress", true, false, "0"},
		{"silent", false, true, "18"},
		{"compact", true, false, "45"},
		{"hdr", true, false, "18"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "Media title.mp4")
			args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=s=320x180:r=24", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "2", "-c:v", "libx264", "-crf", tc.crf, "-pix_fmt", "yuv420p", "-c:a", "aac"}
			if tc.name == "hdr" {
				args = append(args, "-color_trc", "smpte2084", "-x264-params", "transfer=smpte2084")
			}
			cmd := command(ctx, ffmpeg, append(args, file)...)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(err, string(output))
			}
			before, _ := os.Stat(file)
			result, err := tk.optimizeVideo(ctx, file, downloads.Options{Compress: tc.compress, VideoOnly: tc.silent}, downloads.Result{FileName: filepath.Base(file), FileSize: before.Size(), OriginalSize: before.Size()})
			if err != nil {
				t.Fatal(err)
			}
			output, err := command(ctx, ffprobe, "-v", "error", "-show_entries", "stream=codec_type,width,height:format=duration", "-of", "json", file).Output()
			if err != nil {
				t.Fatal(err)
			}
			var probe struct {
				Streams []struct {
					Type          string `json:"codec_type"`
					Width, Height int
				}
				Format struct{ Duration string }
			}
			if json.Unmarshal(output, &probe) != nil || len(probe.Streams) == 0 || probe.Streams[0].Width != 320 || probe.Streams[0].Height != 180 {
				t.Fatal("video dimensions lost", string(output))
			}
			if tc.silent && len(probe.Streams) != 1 || !tc.silent && len(probe.Streams) != 2 {
				t.Fatal("wrong audio stream count", string(output))
			}
			if tc.name == "compress" && (!result.Compressed || result.FileSize >= result.OriginalSize) {
				t.Fatal("compression did not reduce fixture", result)
			}
			if tc.name == "compact" && (result.Compressed || result.FileSize != result.OriginalSize) {
				t.Fatal("larger result replaced compact original", result)
			}
			if tc.name == "hdr" && (result.Compressed || result.FileSize != result.OriginalSize || result.Message == "") {
				t.Fatal("HDR was altered", result)
			}
			if output, err := command(ctx, ffmpeg, "-v", "error", "-i", file, "-f", "null", "-").CombinedOutput(); err != nil {
				t.Fatal("processed video cannot be fully decoded", err, string(output))
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(file), ".optimized.mp4")); !os.IsNotExist(err) {
				t.Fatal("temporary compression output was retained", err)
			}
			t.Logf("before=%d bytes after=%d bytes compressed=%t", result.OriginalSize, result.FileSize, result.Compressed)
		})
	}
}
