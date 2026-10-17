package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tangentry/internal/transcription"
)

func speechDownloaderFixture(t *testing.T, body string) (string, string) {
	t.Helper()
	python := executable("", "python3")
	if python == "" {
		t.Skip("Python required for explicit process fixture")
	}
	root := t.TempDir()
	record := filepath.Join(root, "args.json")
	script := "#!" + python + "\nimport json, pathlib, sys, time\nargs=sys.argv[1:]\npath=pathlib.Path(args[args.index('--output')+1])\nrecord=pathlib.Path(" + strconv.Quote(record) + ")\nrecord.write_text(json.dumps(args))\n" + body
	binary := filepath.Join(root, "download-fixture.py")
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary, record
}

func TestSpeechDownloadUsesYTDLPWithBoundedRetriesAndNoPlatformCookies(t *testing.T) {
	binary, record := speechDownloaderFixture(t, "path.write_bytes(b'complete audio fixture')\n")
	path := filepath.Join(t.TempDir(), "source.media")
	engine := Transcriber{YTDLP: binary}
	audio := transcription.Audio{Platform: "bilibili", URL: "https://media.bilivideo.com/audio.m4s?fixture=1", Referer: "https://www.bilibili.com/"}
	if err := engine.fetchSpeechAudio(context.Background(), audio, path); err != nil {
		t.Fatal(err)
	}
	bytes, _ := os.ReadFile(record)
	var args []string
	json.Unmarshal(bytes, &args)
	for option, want := range map[string]string{"--retries": "2", "--fragment-retries": "2", "--http-chunk-size": "2M", "--max-filesize": "512M", "--output": path, "--referer": audio.Referer} {
		found := false
		for i, arg := range args {
			if arg == option && i+1 < len(args) && args[i+1] == want {
				found = true
			}
		}
		if !found {
			t.Fatal("missing bounded downloader option", option, args)
		}
	}
	for _, arg := range args {
		if arg == "--cookies" || arg == "--cookies-from-browser" {
			t.Fatal("API login jar sent to CDN")
		}
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != audio.URL {
		t.Fatal("source was not a separated argument")
	}
	if value, _ := os.ReadFile(path); string(value) != "complete audio fixture" {
		t.Fatal("unexpected staged file")
	}
}

func TestBackupDoesNotAppendToPrimaryPartialFile(t *testing.T) {
	binary, _ := speechDownloaderFixture(t, "if 'primary.bilivideo.com' in args[-1]:\n    pathlib.Path(str(path)+'.part').write_bytes(b'primary partial')\n    raise SystemExit(1)\nassert not pathlib.Path(str(path)+'.part').exists()\npath.write_bytes(b'backup complete')\n")
	engine := Transcriber{YTDLP: binary}
	path := filepath.Join(t.TempDir(), "source.media")
	err := engine.fetchSpeechAudio(context.Background(), transcription.Audio{Platform: "bilibili", URL: "https://primary.bilivideo.com/a", Backups: []string{"https://evil.invalid/a", "https://backup.bilivideo.com/a"}}, path)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := os.ReadFile(path); string(value) != "backup complete" {
		t.Fatal("mixed primary and backup bytes")
	}
}

func TestSpeechDownloadRejectsUntrustedPlatformSourceBeforeLaunchingTool(t *testing.T) {
	binary, record := speechDownloaderFixture(t, "path.write_bytes(b'should not run')\n")
	engine := Transcriber{YTDLP: binary}
	for _, audio := range []transcription.Audio{
		{Platform: "bilibili", URL: "http://media.bilivideo.com/a"},
		{Platform: "bilibili", URL: "https://127.0.0.1/a"},
		{Platform: "youtube", URL: "https://googlevideo.com.evil.invalid/a"},
		{Platform: "youtube", URL: "https://private@media.googlevideo.com/a"},
		{Platform: "rss", URL: "https://media.googlevideo.com/a"},
	} {
		if err := engine.fetchSpeechAudio(context.Background(), audio, filepath.Join(t.TempDir(), "source.media")); !errors.Is(err, transcription.ErrInvalid) {
			t.Fatal(audio, err)
		}
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe source launched downloader")
	}
}

func TestCanceledDownloadStopsProcessAndCleansTranscriptionTemporaryDirectory(t *testing.T) {
	binary, record := speechDownloaderFixture(t, "pathlib.Path(str(path)+'.part').write_bytes(b'partial')\ntime.sleep(120)\n")
	model := filepath.Join(t.TempDir(), "model.bin")
	os.WriteFile(model, []byte("explicit model fixture"), 0600)
	engine := Transcriber{YTDLP: binary, FFmpeg: binary}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := engine.Transcribe(ctx, transcription.Config{Binary: binary, Model: model, Language: "zh"}, transcription.Audio{Platform: "bilibili", URL: "https://media.bilivideo.com/a"}, 60, func(string) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("canceled downloader not stopped", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal("process did not start", err)
	}
	var args []string
	json.Unmarshal(data, &args)
	for i, arg := range args {
		if arg == "--output" {
			if _, err := os.Stat(filepath.Dir(args[i+1])); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary directory not cleaned")
			}
			return
		}
	}
	t.Fatal("output path missing")
}

func TestPublicAudioRetryRestartsRatherThanMixingPartialBytes(t *testing.T) {
	calls := 0
	engine := Transcriber{HTTP: &http.Client{Transport: speechTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credential leaked")
		}
		text := "truncated-first"
		length := int64(100)
		if calls == 2 {
			text = "second complete"
			length = int64(len(text))
		}
		return &http.Response{StatusCode: 200, ContentLength: length, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(text))}, nil
	})}}
	path := filepath.Join(t.TempDir(), "source.media")
	if err := engine.fetchSpeechAudio(context.Background(), transcription.Audio{URL: "https://public-podcast.example/audio.mp3"}, path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "second complete" || calls != 2 {
		t.Fatal("partial body reused", calls, string(data))
	}
}

func TestSubprocessFailureNeverExposesRawDiagnostics(t *testing.T) {
	for _, test := range []struct{ stage, text, code string }{
		{"converting", "https://media.example/signed?secret=fixture Invalid data found when processing input", "audio_conversion"},
		{"recognizing", "RuntimeError: out of memory https://private.invalid?token=fixture", "model_memory"},
		{"recognizing", "ModuleNotFoundError: No module named 'numpy'", "python_dependencies"},
	} {
		err := toolFailure(test.stage, test.text)
		var reason *transcription.Failure
		if !errors.As(err, &reason) || reason.Code != test.code || strings.Contains(err.Error(), "https://") || strings.Contains(err.Error(), "fixture") {
			t.Fatal(err)
		}
	}
}
