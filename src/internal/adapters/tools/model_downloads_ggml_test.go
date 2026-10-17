package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tangentry/internal/transcription"
	"testing"
)

func TestGGMLDownloadRejectsCorruptionAndCancellationBeforePublishing(t *testing.T) {
	body := "model fixture"
	hash := sha256.Sum256([]byte(body))
	spec := ggmlSpec{int64(len(body)), hex.EncodeToString(hash[:])}
	option := transcription.ModelOption{ID: "whisper-tiny", Backend: "whisper.cpp"}
	for _, scenario := range []string{"valid", "corrupt", "truncated", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			installer := &ModelInstaller{Root: root}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data := body
			if scenario == "corrupt" {
				data = strings.Repeat("x", len(body))
			}
			if scenario == "truncated" {
				data = body[:3]
			}
			if scenario == "canceled" {
				cancel()
			}
			err := installer.receiveGGML(ctx, strings.NewReader(data), option, spec, func(string) {})
			contents, readErr := os.ReadFile(filepath.Join(root, "ggml-tiny.bin"))
			if scenario == "valid" {
				if err != nil || readErr != nil || string(contents) != body {
					t.Fatal(err, readErr)
				}
			} else if err == nil || !os.IsNotExist(readErr) {
				t.Fatal("unverified model published", err, readErr)
			}
			entries, _ := os.ReadDir(root)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".") {
					t.Fatal("staging leaked")
				}
			}
		})
	}
	if !(&ModelInstaller{}).AvailableFor(option, "/missing/python") {
		t.Fatal("GGML requires Python")
	}
}

func TestTranscriptionDownloadsMissingDefaultModelOnFirstUseAndRecovers(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("POSIX fixture")
	}
	body := strings.Repeat("whisper model fixture ", 40000)
	hash := sha256.Sum256([]byte(body))
	previous := ggmlModels["whisper-tiny"]
	ggmlModels["whisper-tiny"] = ggmlSpec{int64(len(body)), hex.EncodeToString(hash[:])}
	defer func() { ggmlModels["whisper-tiny"] = previous }()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/ggml-tiny.bin" || requests == 1 {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	root := t.TempDir()
	installer := &ModelInstaller{Root: root, ggmlSource: server.URL + "/"}
	engine := &Transcriber{FFmpeg: "/bin/sh", Models: installer}
	cfg := transcription.Config{Backend: "whisper.cpp", Binary: "/bin/sh", Model: filepath.Join(root, "ggml-tiny.bin"), Language: "zh"}
	status := engine.Status(cfg)
	if !status.Ready || status.ModelDownload == "" || len(status.Missing) != 0 {
		t.Fatal("a downloadable default model blocked transcription", status)
	}
	outside := cfg
	outside.Model = filepath.Join(t.TempDir(), "ggml-tiny.bin")
	if status := engine.Status(outside); status.Ready || status.ModelDownload != "" {
		t.Fatal("a model outside the managed directory would be downloaded", status)
	}
	audio := transcription.Audio{LocalPath: filepath.Join(root, "missing.media")}
	var stages []string
	_, err := engine.Transcribe(context.Background(), cfg, audio, 0, func(stage string) { stages = append(stages, stage) })
	var failure *transcription.Failure
	if !errors.As(err, &failure) || failure.Code != "model_download" || len(stages) != 1 || stages[0] != "model" {
		t.Fatal("failed model download was not reported as such", err, stages)
	}
	if _, err := os.Stat(cfg.Model); !os.IsNotExist(err) {
		t.Fatal("failed download left a model behind")
	}
	var received, total int64
	ctx := transcription.WithProgress(context.Background(), func(r, t int64) { received, total = r, t })
	_, err = engine.Transcribe(ctx, cfg, audio, 0, func(string) {})
	if !errors.As(err, &failure) || failure.Code != "local_media" {
		t.Fatal("retry did not continue past the downloaded model", err)
	}
	if contents, err := os.ReadFile(cfg.Model); err != nil || string(contents) != body {
		t.Fatal("retried download not published", err)
	}
	if received != int64(len(body)) || total != int64(len(body)) {
		t.Fatal("download progress not reported", received, total)
	}
	if status := engine.Status(cfg); !status.Ready || status.ModelDownload != "" {
		t.Fatal("installed model still reported as pending", status)
	}
}
