package tools

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tangentry/internal/transcription"
)

//go:embed download_model.py
var modelDownloadScript []byte

type ModelInstaller struct {
	Root         string
	BundledModel string
	ggmlMu       sync.Mutex
	ggmlSource   string // test override of the official model host
}

func (m *ModelInstaller) AvailableFor(option transcription.ModelOption, python string) bool {
	return option.Backend == "whisper.cpp" || m.Available(python)
}

func (m *ModelInstaller) Available(python string) bool {
	return pythonModuleInstalled(python, "huggingface_hub")
}
func modelDirectory(option transcription.ModelOption) string {
	if option.Backend == "whisper.cpp" {
		return "ggml-" + strings.TrimPrefix(option.ID, "whisper-") + ".bin"
	}
	if option.Backend == "qwen3-asr" {
		return option.ID
	}
	return "faster-" + option.ID
}
func installedModel(path string, option transcription.ModelOption) bool {
	if option.Backend == "whisper.cpp" {
		info, err := os.Stat(path)
		spec, ok := ggmlModels[option.ID]
		return err == nil && ok && info.Mode().IsRegular() && info.Size() == spec.size
	}
	if option.Backend == "qwen3-asr" {
		return qwenModel(path, option.ID == "qwen3-aligner-0.6b")
	}
	return fasterModel(path)
}
func (m *ModelInstaller) Installed(option transcription.ModelOption) (string, bool) {
	path := filepath.Join(m.Root, modelDirectory(option))
	if installedModel(path, option) {
		return path, true
	}
	if option.ID == "whisper-base" && installedModel(m.BundledModel, option) {
		return m.BundledModel, true
	}
	return path, installedModel(path, option)
}
func (m *ModelInstaller) Install(ctx context.Context, option transcription.ModelOption, python string, stage func(string)) error {
	if option.Backend == "whisper.cpp" {
		return m.installGGML(ctx, option, stage)
	}
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return err
	}
	root, err := os.MkdirTemp(m.Root, ".model-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	helper := filepath.Join(root, "download.py")
	if err = os.WriteFile(helper, modelDownloadScript, 0600); err != nil {
		return err
	}
	destination := filepath.Join(root, "model")
	cmd := command(ctx, executable(python, "python3"), helper, option.Repository, destination, option.Backend)
	cmd.Env = append(toolEnvironment(), "HF_HUB_DISABLE_IMPLICIT_TOKEN=1", "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_PROGRESS_BARS=1", "DO_NOT_TRACK=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = &boundedBuffer{}
	if err = cmd.Run(); err != nil {
		return err
	}
	stage("verifying")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !installedModel(destination, option) || !regularFile(filepath.Join(destination, "tangentry-source.json")) {
		return transcription.ErrUnavailable
	}
	// Publish a complete directory atomically; a partial download cannot appear
	// in installed-model choices or replace a working model.
	return m.publish(ctx, destination, option)
}

func (m *ModelInstaller) publish(ctx context.Context, source string, option transcription.ModelOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.Join(m.Root, modelDirectory(option))
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return os.Rename(source, target)
	} else if err != nil {
		return err
	}
	// A working model may have been added while the download was running.
	if installedModel(target, option) {
		return nil
	}
	backup, err := os.MkdirTemp(m.Root, ".model-backup-")
	if err != nil {
		return err
	}
	previous := filepath.Join(backup, "previous")
	if err = os.Rename(target, previous); err != nil {
		_ = os.Remove(backup)
		return err
	}
	if err = ctx.Err(); err == nil {
		err = os.Rename(source, target)
	}
	if err != nil {
		if restoreErr := os.Rename(previous, target); restoreErr != nil {
			// Leave the backup intact if restoration is not possible.
			return errors.Join(err, restoreErr)
		}
		_ = os.Remove(backup)
		return err
	}
	// Keep incomplete user files outside model discovery, rather than delete them.
	return nil
}
